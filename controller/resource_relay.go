package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/hosting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ResourceRelay 在平台选渠道之前执行一次完整来源策略。
// 未配置策略的 Key 继续原调用链；配置后的请求由本处理器独占结束。
// 每个候选使用独立上下文，防止渠道密钥、计费会话和重试状态互相污染。
// 路由装配约束：本处理器必须位于 TokenAuth 与总请求限流之后。
// 它必须位于 Distribute 之前，否则免费来源也会被平台渠道门禁拒绝。
// 不调用普通 Relay：后者自行重试并在退出时异步退款，不适合跨来源决策。
// Gin Copy 得到的上下文没有可继续执行的 handler 链，仅作为一次尝试的数据容器。
// 所有尝试共享同一个下游 Writer，因此“已经输出”始终是整个请求的事实。
// Request.Clone 隔离请求头，避免平台适配器修改下一来源的认证头。
// 请求正文仍由现有 BodyStorage 复用，最外层中间件负责最终关闭存储。
// 策略快照在鉴权时读取；处理中用户修改排序只影响下一次请求。
// 遍历最多包含已保存的有限来源，不回到先前尝试过的来源形成循环。
// 用户勾选钱包就是付费回退授权，执行器不能另外插入隐含钱包来源。
// 来源改变不改变请求模型；模型映射仅由平台既有渠道设置决定。
// 用户要求不同模型的 Auto 选择属于另一层功能，不在这里猜测替代模型。
// 返回 skipped 证明尚未发起生成；返回 error 则表示当前请求必须结束。
// 这一返回值合同比 HTTP 状态更严格，不能把所有 429 当作安全回退信号。
// 本处理器不自动使用重置机会，耗卡只能通过带单次证明的控制接口发起。
func ResourceRelay() gin.HandlerFunc {
	return func(c *gin.Context) {
		value, exists := c.Get("oak.resource_policy")
		if !exists {
			c.Next()
			return
		}
		policy, ok := value.(*model.ResourcePolicySnapshot)
		if !ok || policy == nil {
			resourceRelayError(c, "invalid_resource_policy", http.StatusInternalServerError)
			return
		}
		c.Abort() // 阻断剩余handler，避免来源执行后又落到旧收费链。
		format := types.RelayFormatOpenAI
		switch c.Request.URL.Path {
		case "/v1/responses":
			format = types.RelayFormatOpenAIResponses
		case "/v1/responses/compact":
			format = types.RelayFormatOpenAIResponsesCompaction
		case "/v1/chat/completions":
		default:
			resourceRelayError(c, "resource_policy_endpoint_unsupported", http.StatusBadRequest)
			return
		}
		// 复用同一严格 JSON 入口，拒绝重复 model 键，避免目录校验和上游解析分歧。
		envelope, err := middleware.ResourceModelRequest(c)
		if err != nil {
			status := http.StatusBadRequest
			if common.IsRequestBodyTooLargeError(err) {
				status = http.StatusRequestEntityTooLarge
			}
			resourceRelayError(c, "invalid_request", status)
			return
		}
		modelName := envelope.Model
		if modelName == "" {
			resourceRelayError(c, "model_required", http.StatusBadRequest)
			return
		}
		if c.GetBool("token_model_limit_enabled") {
			limits, _ := c.Get("token_model_limit")
			allowed, _ := limits.(map[string]bool)
			if !middleware.TokenModelLimitAllows(allowed, modelName) {
				resourceRelayError(c, "token_model_forbidden", http.StatusForbidden)
				return
			}
		}
		// 数量与最大输出上限必须在免费透传前验证，不能绕过现有协议输入约束。
		var stateful struct {
			Background         bool   `json:"background"`
			PreviousResponseID string `json:"previous_response_id"`
		}
		// 异步任务与上游持久会话需要来源绑定，首期不能允许自动切换账号。
		if err := common.UnmarshalBodyReusable(c, &stateful); err != nil || stateful.Background || stateful.PreviousResponseID != "" {
			resourceRelayError(c, "resource_policy_stateful_request_unsupported", http.StatusBadRequest)
			return
		}
		if _, err := helper.GetAndValidateRequest(c, format); err != nil {
			status := http.StatusBadRequest
			if common.IsRequestBodyTooLargeError(err) {
				status = http.StatusRequestEntityTooLarge
			}
			resourceRelayError(c, "invalid_request", status)
			return
		}
		common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
		reasons := []string{}
		for index, source := range policy.Sources {
			if !source.Enabled {
				continue
			}
			if c.Request.Context().Err() != nil {
				return
			}
			attempt := c.Copy()
			attempt.Writer = c.Writer // 所有来源共享同一响应提交边界。
			attempt.Request = c.Request.Clone(c.Request.Context())
			attempt.Set("oak.resource_source", source.Kind)
			attempt.Set("oak.resource_revision", policy.Revision)      // 留下本次请求冻结的版本。
			attempt.Set("oak.resource_reasons", slices.Clone(reasons)) // 后续追加原因不能回写前一日志快照。
			attempt.Set("oak.resource_account", source.AccountID)
			// 原请求 ID 留在访问/消费日志，订阅幂等 ID 在计费信息中单独派生。
			var skipped string
			if source.Kind == "hosting" {
				skipped, err = relayHostingResource(attempt, source.AccountID, modelName)
			} else if source.Kind == "wallet" || source.Kind == "subscription" {
				skipped, err = relayPlatformResource(attempt, source.Kind, modelName, format, index)
			} else {
				err = errors.New("invalid_resource_source")
			}
			if err != nil {
				// 已输出时只能在原流内报告错误，绝不切换来源再次执行。
				if !c.Writer.Written() {
					resourceRelayError(c, err.Error(), http.StatusBadGateway)
				} else if strings.HasPrefix(c.Writer.Header().Get("Content-Type"), "text/event-stream") {
					// 同一流内输出稳定错误，不透露内部原始错误或重放已发正文。
					writeRelayStreamError(c, format, types.NewErrorWithStatusCode(err, "resource_execution_failed", http.StatusBadGateway))
				}
				return
			}
			if skipped == "" { // 空原因表示已执行结束，不能继续遍历其他资源。
				return
			}
			reasons = append(reasons, fmt.Sprintf("%s:%d:%s", source.Kind, source.AccountID, skipped))
		}
		c.Header("X-Oak-Resource-Policy", policy.Revision)
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"type": "resource_unavailable", "code": "resource_sources_exhausted", "message": i18n.T(c, i18n.MsgResourceSourcesExhausted), "reasons": reasons}})
	}
}

// resourceRelayError 使用稳定错误码，不将内网地址、密钥或上游原始错误回传。
// 流式输出后的错误由执行分支处理，避免向已提交响应追加第二份 JSON。
// code 必须来自本文件定义的稳定分类，不能传入任意网络或数据库错误文本。
// 原始错误有可能包含内网 URL 或上游响应，用户只获得可定位的故障分类。
// HTTP 状态区分本地输入拒绝、资源耗尽和执行失败，不能统一伪装成成功。
// 外层请求日志仍携带原 request_id，来源重试不会生成多个客户端请求身份。
func resourceRelayError(c *gin.Context, code string, status int) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"type": "resource_error", "code": code, "message": i18n.T(c, i18n.MsgResourceRequestFailed, map[string]any{"Code": code})}})
}

// resourcePlatformAllowed 保留用户组、令牌组与平台倍率组的既有权限交集。
// 挂靠不依赖这些组；平台候选必须在选择渠道前通过检查。
// 空令牌分组继承用户分组，遵守平台已有的分组默认值。
// 显式 auto 仍需出现在用户可用组中，不能通过自由排序增加组权限。
// 非 auto 组还需有当前倍率定义，已下架组不能被策略持久引用复活。
// 这只是平台权限校验，不把账号归属换算成平台组名称。
// 管理员固定渠道的既有行为由选渠道器处理，但不会绕过此处资金来源选择。
func resourcePlatformAllowed(c *gin.Context) bool {
	group := common.GetContextKeyString(c, constant.ContextKeyTokenGroup) // 使用鉴权解析的组，而非客户端正文的group。
	if group == "" {
		return true
	}
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	_, allowed := service.GetUserUsableGroups(userGroup)[group]
	return allowed && (group == "auto" || ratio_setting.ContainsGroupRatio(group))
}

// relayPlatformResource 只对发送前明确不可用的候选返回 skipped。
// 进入 relayHandler 后，无论 429、超时还是断流，均不再跨来源重放。
// 预算检查与实际预留分开：此处零预算预检省去不必要的渠道查询。
// 非零预算仍由数据库原子预留约束，不能依赖鉴权时的余额快照承诺可用。
// 用户钱包金额与 Key 上限是两个约束，任何一个不足都不能透支执行新请求。
// 渠道选择沿用模型、路径、自动组与会话亲和性规则，不另造渠道调度器。
// 首期同步执行器不接任务插件，避免把异步提交结果误当成完整回答结算。
// 定价仍由 PrepareRequestBilling 产生，动态价格和倍率的可信来源没有变化。
// ForcePreConsume 关闭信任用户旁路，跨来源回退必须建立在真实预留之上。
// 预留来源通过请求上下文指定，用户旧 BillingPreference 不参与二次回退。
// 订阅 request_id 追加尝试下标，避免同一请求先退款后再次命中旧预留记录。
// 在预留后重新读取正文若失败，也需同步释放额度，不能留下悬挂扣款。
// 预扣的数据库错误、价格错误与额度不足分别处理，只有明确不足可继续。
// PrepareTieredBilling 的补充预留也必须完成回滚才能报告候选不可用。
// 退款失败保留在会话内，任何再次调用都不能把未知状态覆盖成成功。
// relayHandler 一旦调用就视为可能执行，无论是否已经收到响应头。
// 同一来源内部不进行自动渠道重试，以免上游实际执行后再次扣额度。
// 成功时结算仍由既有适配器完成，来源在 BillingSession 中冻结。
// 实际消耗高于估算时不会从排在后面的来源分摊同一份输出。
// 发生错误时保留原渠道健康处理，使既有自动停用与诊断继续工作。
// 亲和性只在成功后更新，失败候选不能污染后续请求的渠道绑定。
// 流已经输出时只能追加同协议错误事件，不能写第二份 HTTP 响应。
func relayPlatformResource(c *gin.Context, source, modelName string, format types.RelayFormat, index int) (string, error) {
	if !resourcePlatformAllowed(c) {
		return "group_forbidden", nil
	}
	// 预算耗尽只跳过付费候选，免费挂靠不受钱包/令牌预算约束。
	if !c.GetBool("token_unlimited_quota") && c.GetInt("token_quota") <= 0 {
		return "token_budget_exhausted", nil
	}
	constraints := service.GetChannelConstraints(c)
	constraints.AddFilter(hostdto.ChannelFilter{Kind: hostdto.FilterRequestPath, RequestPath: c.Request.URL.Path})
	channel, _, selectErr := service.SelectChannelForRequest(c, modelName, &service.RetryParam{Ctx: c, TokenGroup: common.GetContextKeyString(c, constant.ContextKeyUsingGroup), ModelName: modelName, RequestPath: c.Request.URL.Path})
	if selectErr != nil {
		if selectErr.NoAvailableChannel {
			return "model_unavailable", nil
		}
		return "", errors.New("platform_channel_selection_failed")
	}
	// 首期平台来源沿用同步内置适配器；任务插件不得意外被当成同步文本适配器。
	if channel.Type == constant.ChannelTypeTaskPlugin || channel.GetSetting().TaskPluginKey != "" {
		return "protocol_unavailable", nil
	}
	if err := middleware.SetupContextForSelectedChannel(c, channel, modelName); err != nil {
		return "", errors.New("platform_channel_setup_failed")
	}
	request, err := helper.GetAndValidateRequest(c, format)
	if err != nil {
		return "", errors.New("invalid_request")
	}
	info, err := relaycommon.GenRelayInfo(c, format, request, nil)
	if err != nil {
		return "", errors.New("platform_request_setup_failed")
	}
	// 来源一旦选中就冻结，避免钱包/订阅内部默认偏好再次排序。
	info.RequestId = fmt.Sprintf("%s.resource.%d", info.RequestId, index)
	info.ForcePreConsume = true         // 跨来源尝试不允许信任额度旁路。
	c.Set("oak.billing_source", source) // 仅影响当前上下文，持久化偏好保持原值。
	billingErr := relay.PrepareRequestBilling(c, info)
	if billingErr == nil {
		billingErr = service.PrepareTieredBillingForSelectedGroup(c, info)
	}
	if billingErr != nil {
		logger.LogError(c, common.LogText("resource billing preparation failed: %s", common.LocalLogPreview(billingErr.Error())))
		// 仅已确认的不足可以回退；同步退款失败会覆盖不足状态并停止。
		if session, ok := info.Billing.(*service.BillingSession); ok {
			if err := session.RefundSync(); err != nil {
				return "", errors.New("resource_refund_failed")
			}
		}
		if billingErr.GetErrorCode() == types.ErrorCodeInsufficientUserQuota || errors.Is(billingErr, service.ErrInsufficientTokenBudget) {
			return "quota_exhausted", nil
		}
		return "", errors.New("platform_billing_preparation_failed")
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		if session, ok := info.Billing.(*service.BillingSession); ok {
			if refundErr := session.RefundSync(); refundErr != nil {
				return "", errors.New("resource_refund_failed")
			}
		}
		return "", errors.New("request_body_unavailable")
	}
	c.Request.Body = io.NopCloser(storage)   // 外层存储管理负责生命周期，适配器不能提前销毁缓存。
	service.AppendUsedChannel(c, channel.Id) // 保留原平台渠道诊断记录。
	c.Header("X-Oak-Resource-Source", source)
	apiErr := relayHandler(c, info) // 从此处开始，不再具备安全跨来源重放资格。
	if apiErr != nil {
		if session, ok := info.Billing.(*service.BillingSession); ok {
			if err := session.RefundSync(); err != nil {
				return "", errors.New("resource_refund_failed")
			}
		}
		// 上游失败仍保留渠道健康处理，但不调用普通 Relay 的跨渠道重试循环。
		processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), apiErr, info)
		return "", errors.New("platform_execution_failed")
	}
	service.RecordChannelAffinity(c, channel.Id)
	return "", nil
}

// hostingQuotaExhausted 只采纳近期、尚未到重置时刻且明确适用的耗尽观测。
// 缺失、过期、未知模型范围都不能伪装成零额度，也不阻止一次真实尝试。
// 这里判断的是“是否有充分理由跳过”，不是预测本次请求必然成功。
// 只有整个账号窗口或明确列出当前模型的窗口才影响当前请求。
// 某个模型族的限额不能套到同账号其他模型，更不能套到另一账号。
// 时间从调用方传入，测试不依赖睡眠，也能覆盖恰好到达重置时刻。
// 拒绝未来时间戳，防止两服务时钟偏差让错误观测长期保持有效。
// 一分钟是保守新鲜度门槛，超时后让执行层作真实判定。
// 必须有未来重置时刻，已经过期的零余额不能永久冻结用户资源。
// nil 剩余额度代表未知，而显式零才可能代表耗尽。
// 上游也可能只报告 exhausted 状态，因此不要求一定提供数值余额。
// 自动观察和手动重置会更新执行层缓存，下一请求再读取最新快照。
func hostingQuotaExhausted(observation *hosting.Observation, modelName string, now time.Time) bool {
	if observation == nil || observation.State != "fresh" || observation.Snapshot == nil || observation.ObservedAtMS == nil {
		return false
	}
	age := now.UnixMilli() - *observation.ObservedAtMS // 与执行层统一使用毫秒时间戳。
	if age < 0 || age > int64(time.Minute/time.Millisecond) {
		return false
	}
	for _, window := range observation.Snapshot.QuotaWindows {
		if window.Scope != "account" && !slices.Contains(window.ModelIDs, modelName) {
			continue
		}
		if window.ResetAtMS == nil || *window.ResetAtMS <= now.UnixMilli() { // 重置后旧耗尽值不能继续阻断账号。
			continue
		}
		if window.State == "exhausted" || (window.Remaining != nil && *window.Remaining <= 0) {
			return true
		}
	}
	return false
}

// relayHostingResource 只发送内部受限 Key，不转发浏览器 Cookie、管理凭据或路由头。
// 不跟随重定向，不启用代理环境变量，取消请求会关闭上游连接。
// accountID 来自已授权策略，但每次执行仍重新读取本人归属与停用状态。
// 这样删除或暂停账号对新请求立即生效，不依赖策略缓存过期。
// 私有账号被删除时可以安全跳过；数据库和控制面故障则必须明确失败。
// 身份已连接不代表模型调用通过，只有主动测试成功后的 active 才执行。
// ResolveRoute 返回的地址仅来自服务器回环配置，调用方不能提交目标地址。
// 内部 Key 只允许唯一私有组，执行层再次校验，避免单层权限失误泄漏资源。
// 账号模型目录是发送前能力检查，不会将平台模型混进私人目录。
// 不从浏览器转发 Cookie、组织选择或用户指定的归属头，避免越权和凭据泄漏。
// 原始请求正文透传以保留显式零值、工具内容及上游扩展字段。
// 上游接收正文之前已通过公共数量上限和模型访问限制。
// 服务端只限制等待响应头的时间，不用短总超时截断正常长答案。
// 不允许 HTTP 重定向，防止内部 Key 随 307/308 流向其他服务。
// 不读取系统代理环境，保证管理配置的回环服务不会意外经过出站代理。
// 请求取消传递给 Transport；客户端断开后不再等待或启动下一来源。
// 非成功 HTTP 状态不能证明未执行，因此不在这里向钱包重发正文。
// 接受的成功媒体类型仅为 JSON 与 SSE，HTML 错误页不能冒充模型输出。
// 下发头采用白名单，不能把内部管理信息、Cookie 或传输控制头暴露出去。
// 内容不落到账号表，也不为记录日志缓存完整回答，内存占用与块大小相关。
// JSON 读错误与 SSE 断流都属于结果未知，日志不将其记成确定完成。
// forwarded 仅表示字节转发结束，不等于上游业务成功或用量已知。
// 未解析上游 usage 的记录使用 unknown，不把零 token 伪装成真实计量。
// 消费日志 quota 固定为零；RecordConsumeLog 本身不会修改钱包或令牌余额。
// 不在免费链路调用 SettleBilling，避免缺失 BillingSession 时回落旧钱包路径。
func relayHostingResource(c *gin.Context, accountID int, modelName string) (string, error) {
	route, err := hosting.ResolveRoute(c.Request.Context(), c.GetInt("id"), accountID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "account_unavailable", nil
	}
	if err != nil {
		return "", errors.New("hosting_route_unavailable")
	}
	if route.Paused || route.State != "active" {
		return "account_unavailable", nil
	}
	if !slices.Contains(route.Models, modelName) {
		return "model_unavailable", nil
	}
	if hostingQuotaExhausted(route.Observation, modelName, time.Now()) {
		return "quota_exhausted", nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return "", errors.New("request_body_unavailable")
	}
	request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, route.BaseURL+c.Request.URL.Path, io.NopCloser(storage))
	if err != nil {
		return "", errors.New("hosting_request_invalid")
	}
	request.Header.Set("Authorization", "Bearer "+route.AccessKey) // 仅受限执行Key，绝不使用管理Key。
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	// 受控的单组执行端仍可能结果未知，因此任何传输错误均为终止错误。
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // 回环流量不经系统代理。
	transport.ResponseHeaderTimeout = 2 * time.Minute
	defer transport.CloseIdleConnections() // 独立传输器的空闲连接随请求释放。
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return "", errors.New("hosting_execution_outcome_unknown")
	}
	defer response.Body.Close() // 客户端取消或复制失败都释放上游连接。
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", errors.New("hosting_execution_rejected")
	}
	// 只复制用户需要的协议头，内部服务器信息与凭据永不进入客户端响应。
	contentType := response.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "application/json") && !strings.HasPrefix(contentType, "text/event-stream") {
		return "", errors.New("hosting_response_invalid")
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "no-store") // 账号输出不能被共享代理缓存。
	c.Header("X-Oak-Resource-Source", "hosting")
	c.Header("X-Oak-Hosting-Account", fmt.Sprint(accountID))
	c.Status(response.StatusCode)
	isStream := strings.HasPrefix(contentType, "text/event-stream")
	writer := io.Writer(c.Writer) // JSON直接写出，SSE额外要求及时刷新。
	if isStream {
		writer = resourceStreamWriter{c.Writer}
	}
	_, copyErr := io.Copy(writer, response.Body)
	// 免费调用不预扣任何平台余额；未知 token usage 明确标识，不能估成已知。
	other := model.NewLogOther()
	service.AppendResourceLogInfo(c, other)
	other.SetPublic("usage_status", "unknown") // 未取得usage时不能给日志用户显示伪造用量。
	if copyErr != nil {
		other.SetPublic("execution_status", "outcome_unknown")
	} else {
		other.SetPublic("execution_status", "forwarded")
	}
	model.RecordConsumeLog(c, c.GetInt("id"), model.RecordConsumeLogParams{ModelName: modelName, TokenId: c.GetInt("token_id"), TokenName: c.GetString("token_name"), IsStream: isStream, Other: other})
	if copyErr != nil {
		return "", errors.New("hosting_stream_interrupted")
	}
	return "", nil
}

// resourceStreamWriter 在每次收到上游内容时及时刷新 SSE，避免缓冲整个回答。
// 写入错误直接返回上层，不能把客户端断开当成可重试的新请求。
type resourceStreamWriter struct{ gin.ResponseWriter }

// io.Copy 可能以任意块边界写入；这里不解析或重组 SSE 事件。
// 因此工具调用、终止事件和未知扩展事件都按原字节顺序保留。
// Flush 会提交响应头，这也是后续禁止跨来源切换的不可逆边界。
// 写失败时不 Flush，向上传递原写错误使上游响应体及时关闭。
func (w resourceStreamWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p) // 保留底层写错误，以便上游关闭而不是重试。
	if err == nil {
		w.Flush()
	}
	return n, err
}
