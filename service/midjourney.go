package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

func CovertMjpActionToModelName(mjAction string) string {
	modelName := "mj_" + strings.ToLower(mjAction)
	if mjAction == constant.MjActionSwapFace {
		modelName = "swap_face"
	}
	return modelName
}

// PreConsumeMidjourneyBilling 在调用上游前执行 Key 的平台来源授权。
// 显式策略由统一会话按顺序预扣，不能用钱包查询替代真实预留。
// 免费动作仍检查平台权限，空策略与仅挂靠策略不能借零价绕过。
// 未配置策略的旧 Key 保留历史钱包预检及后扣行为。
func PreConsumeMidjourneyBilling(c *gin.Context, info *relaycommon.RelayInfo, quota int, shouldBill bool) error {
	if _, exists := c.Get("oak.resource_policy"); exists { // 有策略与旧Key缺省是两种不同授权合同。
		if !shouldBill || quota == 0 { // 纯平台授权不制造一笔最小订阅扣款。
			if apiErr := AuthorizeFreePlatformResource(c, info); apiErr != nil {
				return apiErr
			}
			return nil
		}
		if apiErr := PreConsumeBilling(c, quota, info); apiErr != nil {
			return apiErr
		}
		return nil
	}
	// 原始非付费动作没有钱包金额门槛，不改变旧客户端合同。
	if !shouldBill { // 取消付费动作仍由上层请求权限保护。
		return nil
	}
	userQuota, err := model.GetUserQuota(info.UserId, false)
	if err != nil {
		return err
	}
	if userQuota < quota { // 旧合同保持预检，不能将不足解释为网络故障。
		return ErrInsufficientWalletQuota
	}
	return nil
}

// RefundMidjourneyPreConsume 仅释放尚未交付持久任务的真实预留。
// 接受并保存后的等额结算会关闭会话，异步失败转由任务原子退款。
// 失败结果向上传递，不能把清理失败伪装成已退款成功。
func RefundMidjourneyPreConsume(info *relaycommon.RelayInfo) error {
	if info == nil || info.Billing == nil { // 免费与旧后扣请求没有预留需要释放。
		return nil
	}
	session, ok := info.Billing.(*BillingSession)
	if !ok {
		return errors.New("invalid Midjourney billing session")
	}
	return session.RefundSync() // 会话自身确保结算后不退款、重复释放不重复加额。
}

// PrepareMidjourneyTaskBilling 将预扣事实冻结到即将持久化的任务。
// 输入金额是本次固定价格，任务不能在后台按新的倍率重新估价。
// 新会话已经完成资金预留，旧调用则仍由后扣路径负责资金写入。
// 先清空标记，确保被拒绝的提交不会进入异步退款队列。
// 只有真实扣过的 Token 才参与退款，不能根据调用身份推断扣款。
// 来源与原订阅周期一起保存，用户随后改 Key 顺序不改变原任务。
// 这里不写数据库，由调用方将完整任务与计费标记一次插入。
// 任务插入失败仍由请求会话释放，不能交给无主键的后台记录。
// 返回值只表达是否需要结算，不代表任务已接受或已经落库。
func PrepareMidjourneyTaskBilling(relayInfo *relaycommon.RelayInfo, task *model.Midjourney, quota int, shouldBill bool) (bool, error) {
	if task == nil { // 无承载记录不能建立后台退款授权。
		return false, errors.New("Midjourney task is nil")
	}
	task.Quota = 0
	task.TokenId = 0
	task.BillingChannelId = 0
	task.BillingSource = "" // 清除复用对象上可能残留的旧资金归属。
	task.SubscriptionId = 0
	task.SubscriptionResetTime = 0
	if !shouldBill { // 上游明确拒绝时只释放原会话，不留任务扣款。
		return false, nil
	}
	if relayInfo == nil {
		return false, errors.New("relay info is nil")
	}
	if quota < 0 { // 负金额不能转换为向用户增加预算。
		return false, errors.New("quota cannot be negative")
	}
	if relayInfo.Billing != nil {
		// 仅接受已经按完整价格预扣的会话，任务插入后不再二次扣款。
		// Token 实际预留由同一会话决定，不能仅依据请求 Token ID 猜测。
		session, ok := relayInfo.Billing.(*BillingSession)
		if !ok || session.GetPreConsumedQuota() != quota {
			return false, errors.New("Midjourney reservation mismatch")
		}
		task.BillingSource = relayInfo.BillingSource
		task.SubscriptionId = relayInfo.SubscriptionId
		// 周期从真实预扣的锁内结果取得，不能在这里查询可能已重置的订阅。
		if funding, ok := session.funding.(*SubscriptionFunding); ok {
			task.SubscriptionResetTime = funding.SubscriptionResetTime
		}
		if session.tokenConsumed > 0 && !relayInfo.IsPlayground {
			task.TokenId = relayInfo.TokenId
		}
	} else if relayInfo.BillingSource == BillingSourceSubscription && quota > 0 {
		return false, errors.New("legacy Midjourney billing does not support subscriptions")
	}

	task.Quota = quota // 持久退款金额只来自本次已校验的计费价格。
	task.BillingChannelId = task.ChannelId
	if relayInfo.ChannelMeta != nil && relayInfo.ChannelId > 0 {
		task.BillingChannelId = relayInfo.ChannelId
	}
	return true, nil
}

// SettleMidjourneyTaskBilling 在任务可靠落库后完成计费生命周期。
// 新来源固定价格已经预扣，等额结算只让会话退出可退款状态。
// 此后失败退款以数据库任务为准，不依赖进程内会话继续存在。
// 未持久化任务拒绝结算，避免资金消费缺失可查询的业务依据。
// 历史任务继续使用原后扣合同，不能因升级再次预扣旧请求。
// 历史钱包扣费失败时清除标记，防止后台退回从未扣过的金额。
// Token 扣费失败与钱包扣费失败分别保存，退款不得凭空增加预算。
// 返回 billed 表达资金是否已应用，错误不能被调用方伪装为成功。
// 调用方只在成功结算后记录消费统计与正常提交响应。
func SettleMidjourneyTaskBilling(relayInfo *relaycommon.RelayInfo, task *model.Midjourney, prepared bool) (bool, error) {
	if !prepared { // 非计费响应不得进入任何后扣路径。
		return false, nil
	}
	if relayInfo == nil {
		return false, errors.New("relay info is nil")
	}
	if task == nil || task.Id == 0 { // 主键证明调用方已经完成持久化步骤。
		return false, errors.New("Midjourney task must be persisted before billing")
	}
	if task.BillingSource != "" { // 新资金合同不能再进入历史钱包路径。
		// 任务行已经带着真实预扣来源落库；固定价格等额结算只关闭会话。
		// 不重跑旧钱包后扣，否则同一请求会重复扣费。
		if relayInfo.Billing == nil {
			return false, errors.New("Midjourney billing session missing")
		}
		if err := relayInfo.Billing.Settle(task.Quota); err != nil {
			return false, err
		}
		return true, nil
	}

	result, billingErr := postConsumeQuotaWithResult(relayInfo, task.Quota, 0, true)
	if !result.FundingApplied { // 没有真实资金消费时后台也没有退款权限。
		task.Quota = 0
		task.TokenId = 0
		task.BillingChannelId = 0
		if updateErr := task.UpdateBillingState(); updateErr != nil {
			return false, errors.Join(billingErr, fmt.Errorf("clear Midjourney billing state: %w", updateErr))
		}
		return false, billingErr
	}

	task.TokenId = 0
	if result.TokenApplied {
		task.TokenId = relayInfo.TokenId
	}
	if updateErr := task.UpdateBillingState(); updateErr != nil {
		return true, errors.Join(billingErr, fmt.Errorf("update Midjourney billing state: %w", updateErr))
	}
	return true, billingErr
}

// RefundMidjourneyQuota 按任务保存的资金来源释放失败任务额度。
// 新任务资金和 Token 与退款标记原子提交，旧任务保留历史解释。
// 只对本次真正退款的金额更新统计，陈旧轮询副本不能重复计数。
// 订阅编号与周期均来自原任务，不能使用用户现在的计费偏好。
// 数据库失败会保留可恢复标记，由轮询任务继续处理。
// 缓存失败会告警，但已经提交的数据库资金不能重新再退一次。
// 统计与业务日志不是资金授权，任何日志失败都不能重新执行加额。
// 退款不会减少请求次数，失败请求仍是实际发生过的请求。
// 日志只保存任务标识与错误分类，不附带渠道凭据。
// 旧任务没有冻结来源时不能猜测它以前来自哪个订阅。
// 返回 false 表示退款尚未完成，调用方必须保留后续恢复机会。
// 新路径的重放成功返回 true，但不会新增第二条退款流水。
func RefundMidjourneyQuota(ctx context.Context, task *model.Midjourney, reason string) bool {
	quota := task.Quota
	if quota == 0 { // 已清标记无需调用资金接口。
		return true
	}

	if task.BillingSource != "" {
		// 数据库行而非轮询快照决定金额，重放和进程重启都只退一次。
		// 任一资金步骤失败会保留任务金额，供后续轮询恢复。
		refunded, err := model.RefundMidjourneyFunding(task)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("Midjourney funding refund failed, task %s: %s", task.MjId, err))
			if refunded == 0 {
				return false
			}
		}
		if refunded == 0 { // 另一轮已退清，不能再次调整统计。
			return true
		}
		quota = refunded // 以数据库锁定行的真实金额为准。
	} else if err := model.IncreaseUserQuota(task.UserId, quota, false); err != nil {
		logger.LogWarn(ctx, common.LogText("failed to refund Midjourney user quota, task %s: %s", task.MjId, err.Error()))
		return false
	}

	// 新来源的 Token 已在同一事务退款，只有历史任务沿用旧拆分路径。
	if task.BillingSource == "" && task.TokenId > 0 {
		tokenKey := resolveTokenKey(ctx, task.TokenId, task.MjId)
		if tokenKey != "" {
			if err := model.IncreaseTokenQuota(task.TokenId, tokenKey, quota); err != nil {
				logger.LogWarn(ctx, common.LogText("failed to refund Midjourney token quota, task %s: %s", task.MjId, err.Error()))
			}
		}
	}

	billingChannelId := task.GetBillingChannelId() // 退款统计归原收费渠道，而不是后续查图渠道。
	model.UpdateUserUsedQuota(task.UserId, -quota)
	model.UpdateChannelUsedQuota(billingChannelId, -quota)
	other := model.NewLogOther()
	other.SetPublic("task_id", task.MjId)
	other.SetPublic("reason", reason) // 保留业务失败原因以便定位本次返还。
	if task.BillingSource != "" {
		other.SetPublic("resource_source", task.BillingSource)
	}
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   model.LogTypeRefund,
		ChannelId: billingChannelId,
		ModelName: CovertMjpActionToModelName(task.Action),
		Quota:     quota,
		TokenId:   task.TokenId,
		Other:     other,
	})

	task.Quota = 0 // 历史路径也清除标记；新路径已经在资金事务中完成。
	if err := task.UpdateBillingState(); err != nil {
		logger.LogError(ctx, common.LogText("Midjourney refund succeeded but clearing the quota failed, task %s: %s", task.MjId, err.Error()))
	}
	return true
}

func GetMjRequestModel(relayMode int, midjRequest *dto.MidjourneyRequest) (string, *dto.MidjourneyResponse, bool) {
	action := ""
	if relayMode == relayconstant.RelayModeMidjourneyAction {
		// plus request
		err := CoverPlusActionToNormalAction(midjRequest)
		if err != nil {
			return "", err, false
		}
		action = midjRequest.Action
	} else {
		switch relayMode {
		case relayconstant.RelayModeMidjourneyImagine:
			action = constant.MjActionImagine
		case relayconstant.RelayModeMidjourneyVideo:
			action = constant.MjActionVideo
		case relayconstant.RelayModeMidjourneyEdits:
			action = constant.MjActionEdits
		case relayconstant.RelayModeMidjourneyDescribe:
			action = constant.MjActionDescribe
		case relayconstant.RelayModeMidjourneyBlend:
			action = constant.MjActionBlend
		case relayconstant.RelayModeMidjourneyShorten:
			action = constant.MjActionShorten
		case relayconstant.RelayModeMidjourneyChange:
			action = midjRequest.Action
		case relayconstant.RelayModeMidjourneyModal:
			action = constant.MjActionModal
		case relayconstant.RelayModeSwapFace:
			action = constant.MjActionSwapFace
		case relayconstant.RelayModeMidjourneyUpload:
			action = constant.MjActionUpload
		case relayconstant.RelayModeMidjourneySimpleChange:
			params := ConvertSimpleChangeParams(midjRequest.Content)
			if params == nil {
				return "", MidjourneyErrorWrapper(constant.MjRequestError, "invalid_request"), false
			}
			action = params.Action
		case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition, relayconstant.RelayModeMidjourneyNotify:
			return "", nil, true
		default:
			return "", MidjourneyErrorWrapper(constant.MjRequestError, "unknown_relay_action"), false
		}
	}
	modelName := CovertMjpActionToModelName(action)
	return modelName, nil, true
}

func CoverPlusActionToNormalAction(midjRequest *dto.MidjourneyRequest) *dto.MidjourneyResponse {
	// "customId": "MJ::JOB::upsample::2::3dbbd469-36af-4a0f-8f02-df6c579e7011"
	customId := midjRequest.CustomId
	if customId == "" {
		return MidjourneyErrorWrapper(constant.MjRequestError, "custom_id_is_required")
	}
	splits := strings.Split(customId, "::")
	var action string
	if splits[1] == "JOB" {
		action = splits[2]
	} else {
		action = splits[1]
	}

	if action == "" {
		return MidjourneyErrorWrapper(constant.MjRequestError, "unknown_action")
	}
	if strings.Contains(action, "upsample") {
		index, err := strconv.Atoi(splits[3])
		if err != nil {
			return MidjourneyErrorWrapper(constant.MjRequestError, "index_parse_failed")
		}
		midjRequest.Index = index
		midjRequest.Action = constant.MjActionUpscale
	} else if strings.Contains(action, "variation") {
		midjRequest.Index = 1
		if action == "variation" {
			index, err := strconv.Atoi(splits[3])
			if err != nil {
				return MidjourneyErrorWrapper(constant.MjRequestError, "index_parse_failed")
			}
			midjRequest.Index = index
			midjRequest.Action = constant.MjActionVariation
		} else if action == "low_variation" {
			midjRequest.Action = constant.MjActionLowVariation
		} else if action == "high_variation" {
			midjRequest.Action = constant.MjActionHighVariation
		}
	} else if strings.Contains(action, "pan") {
		midjRequest.Action = constant.MjActionPan
		midjRequest.Index = 1
	} else if strings.Contains(action, "reroll") {
		midjRequest.Action = constant.MjActionReRoll
		midjRequest.Index = 1
	} else if action == "Outpaint" {
		midjRequest.Action = constant.MjActionZoom
		midjRequest.Index = 1
	} else if action == "CustomZoom" {
		midjRequest.Action = constant.MjActionCustomZoom
		midjRequest.Index = 1
	} else if action == "Inpaint" {
		midjRequest.Action = constant.MjActionInPaint
		midjRequest.Index = 1
	} else {
		return MidjourneyErrorWrapper(constant.MjRequestError, "unknown_action:"+customId)
	}
	return nil
}

func ConvertSimpleChangeParams(content string) *dto.MidjourneyRequest {
	split := strings.Split(content, " ")
	if len(split) != 2 {
		return nil
	}

	action := strings.ToLower(split[1])
	changeParams := &dto.MidjourneyRequest{}
	changeParams.TaskId = split[0]

	if action[0] == 'u' {
		changeParams.Action = "UPSCALE"
	} else if action[0] == 'v' {
		changeParams.Action = "VARIATION"
	} else if action == "r" {
		changeParams.Action = "REROLL"
		return changeParams
	} else {
		return nil
	}

	index, err := strconv.Atoi(action[1:2])
	if err != nil || index < 1 || index > 4 {
		return nil
	}
	changeParams.Index = index
	return changeParams
}

// RecordMidjourneyPolicyResponse distinguishes accepted tasks from errors inside
// HTTP 200 responses. Submissions are single-attempt: an ambiguous transport
// failure must never create a duplicate task on another channel.
func RecordMidjourneyPolicyResponse(c *gin.Context, response *dto.MidjourneyResponseWithStatusCode, requestErr error) bool {
	if response == nil {
		response = MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "empty_response", http.StatusBadGateway)
	}
	accepted := requestErr == nil && response.StatusCode == http.StatusOK && (response.Response.Code == 1 || response.Response.Code == 21 || response.Response.Code == 22)
	if accepted {
		properties, _ := response.Response.Properties.(map[string]any)
		if properties["status"] != "FAILURE" {
			return true
		}
	}
	state := RequestPolicy(c)
	event := PolicyEvent{ChannelID: c.GetInt("channel_id"), Status: response.StatusCode, ErrorCode: strconv.Itoa(response.Response.Code), ErrorSource: "upstream", Decision: PolicyDecision{Action: "failure", Reason: "upstream_failure", Source: "upstream"}}
	state.AddEvent(event)
	event.Decision, event.Health = PolicyDecision{Action: "stop", Reason: "non_retryable_error", Source: "system"}, "unchanged"
	if accepted {
		event.Decision.Reason = "task_accepted"
	}
	state.AddEvent(event)
	return false
}

func DoMidjourneyHttpRequest(c *gin.Context, timeout time.Duration, fullRequestURL string) (*dto.MidjourneyResponseWithStatusCode, []byte, error) {
	var nullBytes []byte
	//var requestBody io.Reader
	//requestBody = c.Request.Body
	// read request body to json, delete accountFilter and notifyHook
	var mapResult map[string]any
	// if get request, no need to read request body
	if c.Request.Method != "GET" {
		err := common.DecodeJson(c.Request.Body, &mapResult)
		if err != nil {
			return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "read_request_body_failed", http.StatusInternalServerError), nullBytes, err
		}
		if !setting.MjAccountFilterEnabled {
			delete(mapResult, "accountFilter")
		}
		if !setting.MjNotifyEnabled {
			delete(mapResult, "notifyHook")
		}
		//req, err := http.NewRequest(c.Request.Method, fullRequestURL, requestBody)
		// make new request with mapResult
	}
	if setting.MjModeClearEnabled {
		if prompt, ok := mapResult["prompt"].(string); ok {
			prompt = strings.Replace(prompt, "--fast", "", -1)
			prompt = strings.Replace(prompt, "--relax", "", -1)
			prompt = strings.Replace(prompt, "--turbo", "", -1)

			mapResult["prompt"] = prompt
		}
	}
	reqBody, err := common.Marshal(mapResult)
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "marshal_request_body_failed", http.StatusInternalServerError), nullBytes, err
	}
	req, err := http.NewRequest(c.Request.Method, fullRequestURL, strings.NewReader(string(reqBody)))
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "create_request_failed", http.StatusInternalServerError), nullBytes, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	// 使用带有超时的 context 创建新的请求
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
	req.Header.Set("Accept", c.Request.Header.Get("Accept"))
	auth := common.GetContextKeyString(c, constant.ContextKeyChannelKey)
	if auth != "" {
		auth = strings.TrimPrefix(auth, "Bearer ")
		req.Header.Set("mj-api-secret", auth)
	}
	defer cancel()
	resp, err := GetHttpClient().Do(req)
	if err != nil {
		common.SysLog(common.LogText("do request failed: %s", err.Error()))
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "do_request_failed", http.StatusInternalServerError), nullBytes, err
	}
	statusCode := resp.StatusCode
	//if statusCode != 200  {
	//	return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "bad_response_status_code", statusCode), nullBytes, nil
	//}
	err = req.Body.Close()
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "close_request_body_failed", statusCode), nullBytes, err
	}
	err = c.Request.Body.Close()
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "close_request_body_failed", statusCode), nullBytes, err
	}
	var midjResponse dto.MidjourneyResponse
	var midjourneyUploadsResponse dto.MidjourneyUploadResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "read_response_body_failed", statusCode), nullBytes, err
	}
	CloseResponseBodyGracefully(resp)
	logger.LogDebug(c, "midjourney response body: %s", responseBody)
	if len(responseBody) == 0 {
		return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "empty_response_body", statusCode), responseBody, nil
	} else {
		err = common.Unmarshal(responseBody, &midjResponse)
		if err != nil {
			err2 := common.Unmarshal(responseBody, &midjourneyUploadsResponse)
			if err2 != nil {
				return MidjourneyErrorWithStatusCodeWrapper(constant.MjErrorUnknown, "unmarshal_response_body_failed", statusCode), responseBody, err
			}
		}
	}
	//for k, v := range resp.Header {
	//	c.Writer.Header().Set(k, v[0])
	//}
	return &dto.MidjourneyResponseWithStatusCode{
		StatusCode: statusCode,
		Response:   midjResponse,
	}, responseBody, nil
}
