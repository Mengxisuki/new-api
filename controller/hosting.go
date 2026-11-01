package controller

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/hosting"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// hostingError 统一屏蔽数据库语句、管理地址和授权材料。
// 404 对不存在和他人资源相同，错误码供界面显示可执行的下一步。
// 客户端只能得到稳定错误分类，不暴露 GORM 原始查询与连接字符串。
// 归属不匹配与真正不存在都返回相同的 404，避免资源枚举。
// 版本冲突属于可刷新后重试的 409，不应被显示为服务器失效。
// 策略格式错误属于 400；真实数据库故障仍保持 500。
// 执行层错误已经经过 Client 的传输边界脱敏，可保留其公共错误码。
// 调用者必须在写出错误后结束，不能继续发送成功响应。
func hostingError(c *gin.Context, err error) {
	status, code := http.StatusInternalServerError, "hosting_internal_error"
	var remote *hosting.Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		status, code = http.StatusNotFound, "hosting_not_found"
	case errors.Is(err, model.ErrHostingConflict):
		status, code = http.StatusConflict, "hosting_conflict"
	case errors.Is(err, model.ErrResourcePolicyInvalid):
		status, code = http.StatusBadRequest, "resource_policy_invalid"
	case errors.Is(err, model.ErrResourcePolicyConflict):
		status, code = http.StatusConflict, "resource_policy_conflict"
	case errors.As(err, &remote):
		status, code = remote.Status, remote.Code
	}
	message := "Hosting request failed"
	// 配置错误提供可翻译的操作提示，具体部署值仍然保持私有。
	// 英文原文作为控制台七语言词条；调用方不应展示原始错误字符串。
	// 只区分可操作的部署问题，未知内部故障继续使用通用提示。
	// 地址无效与执行层离线不同：前者能本地确定，后者只能由真实调用确认。
	switch code {
	case "hosting_invalid_control_url":
		message = "Account hosting control service URL is invalid. Please contact an administrator."
	case "hosting_encryption_not_configured":
		message = "Account hosting encryption is not configured. Please contact an administrator."
	}
	c.JSON(status, gin.H{"success": false, "message": message, "code": code})
}

// hostingBody 给账号管理请求设置独立小体积上限，不接受无界授权正文。
// JSON 错误只显示通用信息，不把解析器原文写入前端或日志。
// 授权回调文本可能包含临时凭据，因此解析错误不回显输入。
// 体积限制在绑定前设置，避免自定义字段或巨型字符串消耗无界内存。
// 字段语义由具体处理器校验，例如 UUID 版本、模型长度和备注上限。
// 此函数不执行身份验证，必须位于已认证的挂靠路由组内。
// 请求解析失败时返回 false，调用者不能继续发出控制面请求。
// 错误响应保留现有前端可识别的 success/message/code 结构。
// 挂靠入口不接收原厂 token JSON，增大请求体上限不能作为绕开方式。
// 该限制与模型推理输入限制无关，不应复用推理的宽松体积设置。
// 解析成功仅代表结构可读取，不代表敏感操作已经得到许可。
func hostingBody(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32*1024)
	if c.ShouldBindJSON(target) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid parameters", "code": "hosting_invalid_request"})
		return false
	}
	return true
}

// hostingAccount 从真实会话取得 owner，所有账号操作共用这一授权入口。
// 路径中的账号编号只是查找条件，不能作为用户归属的依据。
// 所有者来自认证中间件写入的上下文，而不是请求正文。
// 数字解析失败和越权均按不存在处理，不向用户泄露别人的账号。
// 删除墓碑可能被历史查询使用，是否可变更由具体操作检查。
// 函数不查询 gpt-load，列表与历史读取不会触发原厂访问。
// 返回 false 时响应已写出，处理器应立即结束。
func hostingAccount(c *gin.Context) (*model.HostingAccount, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		hostingError(c, gorm.ErrRecordNotFound)
		return nil, false
	}
	account, err := model.GetHostingAccount(c.GetInt("id"), id)
	if err != nil {
		hostingError(c, err)
		return nil, false
	}
	return account, true
}

// hostingProof 绑定具体动作、账号和不可逆操作 UUID，不能跨操作复用验证。
// 同一授权的回调和消费由原授权归属保护，不要求用户每一步再次验证。
// 证明的上下文以服务端结构编码，浏览器不能替换账号编号。
// reset 同时绑定操作 UUID，另一张重置请求的证明不能借用。
// 证明成功后被现有验证服务单次消费；网络失败也不能复用证明。
// 用户可以重新验证后用原业务 UUID 恢复，避免重复消耗原厂次数。
// 接入证明只授权创建一个短期流程，后续由会话绑定维持权限。
// 权限证明和业务幂等是两层不同约束，不能互相替代。
func hostingProof(c *gin.Context, action string, accountID int, operationID string) bool {
	context, err := common.Marshal(service.HostingManageContext{Action: action, AccountID: accountID, OperationID: operationID})
	if err != nil {
		hostingError(c, err)
		return false
	}
	return middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeHostingManage, Context: context}) != nil
}

// hostingAccountView 只输出已定义的安全字段，观测必须通过白名单解码。
// 坏持久数据返回错误，不用空额度掩盖损坏。
// 内部资源引用、身份指纹和加密访问密钥都不会进入响应。
// 额度缓存使用强类型结构再次过滤，旧持久数据也不能旁路脱敏。
// 没有观测时返回 null，前端应显示未知，而不能显示额度为零。
// verified_model 与 verified_protocol 标记已实际验证的能力范围。
// 暂停状态独立于验证记录，暂停后仍可解释曾经验证过的结果。
// 备注和邮箱掩码仅用于区分本人多个账号，不用于身份认证。
func hostingAccountView(account model.HostingAccount) (gin.H, error) {
	var observation *hosting.Observation
	if account.ObservationJSON != "" {
		if err := common.UnmarshalJsonStr(account.ObservationJSON, &observation); err != nil {
			return nil, err
		}
	}
	return gin.H{"id": account.ID, "provider": account.Provider, "alias": account.Alias, "email_mask": account.EmailMask, "status": account.Status, "paused": account.Paused, "verified_at": account.VerifiedAt, "verified_model": account.VerifiedModel, "verified_protocol": account.VerifiedProtocol, "last_error": account.LastError, "created_at": account.CreatedAt, "updated_at": account.UpdatedAt, "observation": observation}, nil
}

// GetHostingAccounts 列表只读本地投影，避免打开页面就并发刷新所有上游。
// 入口只返回当前用户的非删除账号，管理员也不扩大此用户视图。
// 创建中和删除中记录仍显示，便于用户看到未完成状态。
// 按稳定 ID 排序，避免后台观察更新时间导致卡片不断跳动。
// 空列表返回 items 空数组，前端可以直接进入接入引导。
// 任一持久观测损坏都会显式失败，不能部分成功隐藏故障账号。
// 此列表不把多个账号额度相加，各账号窗口保持原厂意义。
func GetHostingAccounts(c *gin.Context) {
	var accounts []model.HostingAccount
	if err := model.DB.Where("user_id = ? AND status <> ?", c.GetInt("id"), "deleted").Order("id ASC").Find(&accounts).Error; err != nil {
		hostingError(c, err)
		return
	}
	items := make([]gin.H, 0, len(accounts))
	for _, account := range accounts {
		view, err := hostingAccountView(account)
		if err != nil {
			hostingError(c, err)
			return
		}
		items = append(items, view)
	}
	common.ApiSuccess(c, gin.H{"items": items})
}

// GetHostingAccount 读取单个账号时保持与列表相同的脱敏边界。
// 单账号与列表共用投影，不能出现详情页额外泄露内部字段。
// 调用者无法通过更换路径编号读取别人的密文或邮箱摘要。
// 查询不会刷新额度，也不会测试账号或生成新的远端凭据。
// 删除墓碑可以用于解释历史，但不意味着该账号仍可调用。
// 模型可用目录由专用端点提供，避免读取详情就增加远端请求。
// 任何数据库读取失败都保留错误，不能返回空卡片冒充成功。
func GetHostingAccount(c *gin.Context) {
	account, ok := hostingAccount(c)
	if !ok {
		return
	}
	view, err := hostingAccountView(*account)
	if err != nil {
		hostingError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// BeginHostingAuthorization 首期仅接受 Codex 浏览器授权，文件导入尚未开放。
// 敏感验证在创建远端登录任务之前完成。
// provider 必须明确为 codex，未支持的厂商不能隐式落入默认适配器。
// account_id 为零表示新接入，正数表示对原账号重新授权。
// 重新授权先查本人归属，再消费绑定该账号的证明。
// 服务端先持久化本地操作 UUID，控制面超时仍能返回恢复身份。
// 带 data 的失败响应表示结果未知，前端必须保留其中的授权 ID。
// 重新点击应继续该 ID，不能自动创建另一项授权绕过未决状态。
// 创建授权本身不会把账号加入任何 Key 的资源策略。
// 授权 URL 含登录上下文，只给发起会话展示，不写审计正文。
// 执行层只允许同一原厂身份归属一个用户，邮箱掩码不能替代该约束。
// 不同浏览器会话应分别发起自己的验证，不共享短期接入材料。
// 失败后的恢复入口不要求再次输入原厂密码，也不会让 Oak 保存密码。
// 多个本人账号独立接入，后续策略才决定模型调用使用顺序。
func BeginHostingAuthorization(c *gin.Context) {
	identity, ok := hostingIdentity(c)
	if !ok {
		return
	}
	var input struct {
		Provider  string `json:"provider"`
		AccountID int    `json:"account_id"`
	}
	if !hostingBody(c, &input) {
		return
	}
	if input.Provider != "codex" || input.AccountID < 0 {
		hostingError(c, &hosting.Error{Status: 400, Code: "hosting_invalid_request"})
		return
	}
	action := "connect"
	if input.AccountID > 0 {
		if _, err := model.GetHostingAccount(c.GetInt("id"), input.AccountID); err != nil {
			hostingError(c, err)
			return
		}
		action = "reauthorize"
	}
	// 缺少服务器配置不是用户验证失败，预检必须先于单次证明消费。
	if _, err := hosting.NewConfiguredClient(); err != nil {
		hostingError(c, err)
		return
	}
	if !hostingProof(c, action, input.AccountID, "") {
		return
	}
	view, err := hosting.BeginAuthorization(c.Request.Context(), identity, input.AccountID)
	if err != nil {
		// starting ID 可用于明确恢复；未知创建不能用再次点击偷偷换一个操作。
		if view != nil {
			c.JSON(502, gin.H{"success": false, "code": "hosting_control_outcome_unknown", "message": "Hosting request failed", "data": view})
			return
		}
		hostingError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// GetHostingAuthorization 的 GET 只查询已有 Stage，不重新发起创建。
// 此接口只观察原流程，不重启 OAuth 或替用户提交回调。
// 会话或认证版本变化会使旧授权无法通过归属查询。
// 过期授权显示 expired，不能因轮询延长原厂有效期。
// 已消费、取消或正在消费的授权不会重新展示登录网址。
// 请求可以读取已取得的 Stage 状态，但不会新建 Stage。
// 跨会话继续时返回不存在，用户需要在原会话完成或重新接入。
func GetHostingAuthorization(c *gin.Context) {
	identity, ok := hostingIdentity(c)
	if !ok {
		return
	}
	view, err := hosting.LoadAuthorization(c.Request.Context(), identity, c.Param("id"), false)
	if err != nil {
		hostingError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// ResumeHostingAuthorization 明确恢复同一个短期操作，不生成新的 Stage 身份。
// 恢复用于最初创建 Stage 的结果未知场景，必须是显式 POST。
// 操作沿用本地 UUID，因此执行层可以返回同一个授权结果。
// 已有 Stage 时仅查询它，不生成新的登录地址或授权目的。
// 原流程已过期时不会因为点击恢复而延长登录有效期。
// 恢复仍受发起会话和认证版本限制，不是任意持有 ID 即可继续。
// 此操作不消费原厂重置机会，也不改变用户的资源回退策略。
func ResumeHostingAuthorization(c *gin.Context) {
	identity, ok := hostingIdentity(c)
	if !ok {
		return
	}
	view, err := hosting.LoadAuthorization(c.Request.Context(), identity, c.Param("id"), true)
	if err != nil {
		hostingError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// CompleteHostingAuthorization 只把本人回调交给受控 Stage，原始 URL 不入日志。
// 输入是浏览器回调地址的文本，服务器不会访问其中的主机。
// 回调形状、固定端口与 code/state 单值由业务层校验。
// 执行层才持有 PKCE 和 state，Oak 不复制第二套 OAuth 校验。
// 回调内容不持久化在 Oak，不写入业务审计或错误正文。
// 完成回调只使授权就绪，账号创建仍由独立确认动作执行。
// 过期、取消或已消费授权不能用历史回调复活。
func CompleteHostingAuthorization(c *gin.Context) {
	identity, ok := hostingIdentity(c)
	if !ok {
		return
	}
	var input struct {
		CallbackURL string `json:"callback_url"`
	}
	if !hostingBody(c, &input) {
		return
	}
	view, err := hosting.CompleteAuthorization(c.Request.Context(), identity, c.Param("id"), input.CallbackURL)
	if err != nil {
		hostingError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// CancelHostingAuthorization 取消后不允许通过历史回调再次创建账号。
// 取消先关闭本地消费入口，再通知执行层清理临时 Stage。
// 远端清理失败会返回错误，但本地不会重新允许使用回调。
// 已经开始消费的授权不能取消，以免制造已创建但不可管理的资源。
// 取消请求只作用于发起会话所属的授权，不能按远端 Stage 随意取消。
// 远端失败后的重复取消可以继续清理同一 Stage。
// 用户必须看到明确的取消结论，不能把网络错误伪装为已清理。
func CancelHostingAuthorization(c *gin.Context) {
	identity, ok := hostingIdentity(c)
	if !ok {
		return
	}
	if err := hosting.CancelAuthorization(c.Request.Context(), identity, c.Param("id")); err != nil {
		hostingError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"status": "cancelled"})
}

// CreateHostingAccount 消费已验证的本地授权，账号不自动加入任何策略。
// 用户必须在策略编辑器明确授权位置和可能的后续付费回退。
// 只接受本地 authorization_id，不接受远端账号引用、访问密钥或凭据文件。
// 备注经去空白后校验，并限制持久字段长度。
// 同一授权的重复创建读取同一账号，不再次占用五账号名额。
// 激活失败时保留 provisioning 记录，后台可以恢复同一个操作。
// 创建成功仅表示 connected，模型测试成功之前不能参与数据面调用。
// 服务端不会自动开启钱包回退，来源位置必须由用户另行保存。
// 会话绑定防止同一账号的另一登录窗口接管敏感授权流程。
// 账号上限统计在事务中完成，并发点击不能绕过五账号限制。
// 远端 pending 是可恢复的中间状态，不能直接当成已启用成功展示。
// 本地加密配置缺失时提前拒绝，不建立无法重启恢复的凭据记录。
// 已有账号重新授权必须沿用身份引用，不能在此换成另一个人的账号。
func CreateHostingAccount(c *gin.Context) {
	identity, ok := hostingIdentity(c)
	if !ok {
		return
	}
	var input struct {
		AuthorizationID string `json:"authorization_id"`
		Alias           string `json:"alias"`
	}
	if !hostingBody(c, &input) {
		return
	}
	input.Alias = strings.TrimSpace(input.Alias)
	if len(input.Alias) == 0 || len(input.Alias) > 80 || input.AuthorizationID == "" {
		hostingError(c, &hosting.Error{Status: 400, Code: "hosting_invalid_request"})
		return
	}
	account, err := hosting.CreateAccount(c.Request.Context(), identity, input.AuthorizationID, input.Alias)
	if err != nil {
		hostingError(c, err)
		return
	}
	view, err := hostingAccountView(*account)
	if err != nil {
		hostingError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// UpdateHostingAccount 仅接受备注与暂停状态，不能修改原厂身份和归属。
// 可编辑字段仅为备注和暂停标记，原厂身份不能在此替换。
// 空变更请求被拒绝，避免无意义控制面调用和虚假的操作成功。
// 暂停优先关闭 Oak 本地路由，执行层暂时不可用也不会重新开放。
// 恢复要先确认执行层成功，之后才恢复本地可用状态。
// 恢复不会生成验证事实；从未测试过的账号仍保持 connected。
// 删除中、已删除和创建中账号不能通过普通编辑越过生命周期。
// 备注只影响显示，不影响 Key 的资源顺序或计费规则。
func UpdateHostingAccount(c *gin.Context) {
	account, ok := hostingAccount(c)
	if !ok {
		return
	}
	var input struct {
		Alias  *string `json:"alias"`
		Paused *bool   `json:"paused"`
	}
	if !hostingBody(c, &input) {
		return
	}
	if input.Alias == nil && input.Paused == nil {
		hostingError(c, &hosting.Error{Status: 400, Code: "hosting_invalid_request"})
		return
	}
	if input.Alias != nil {
		*input.Alias = strings.TrimSpace(*input.Alias)
		if len(*input.Alias) == 0 || len(*input.Alias) > 80 {
			hostingError(c, &hosting.Error{Status: 400, Code: "hosting_invalid_request"})
			return
		}
	}
	updated, err := hosting.ChangeAccount(c.Request.Context(), c.GetInt("id"), account.ID, input.Alias, input.Paused)
	if err != nil {
		hostingError(c, err)
		return
	}
	view, err := hostingAccountView(*updated)
	if err != nil {
		hostingError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// DeleteHostingAccount 的权限证明绑定账号，不接受另一账号的删除证明。
// 删除证明绑定当前账号，不能用删除另一个账号的证明替代。
// 本地先进入 deleting，新的请求立即停止选择该来源。
// 执行层确认删除之后才清除密文，未知结果保留恢复所需引用。
// 历史重置操作和策略中的引用不会被静默改成钱包来源。
// 如果原厂重置仍未决，执行层可以拒绝删除，界面保留待处理状态。
// 此接口解绑平台挂靠，不表示注销或删除用户的原厂账号。
func DeleteHostingAccount(c *gin.Context) {
	account, ok := hostingAccount(c)
	if !ok {
		return
	}
	if !hostingProof(c, "delete", account.ID, "") {
		return
	}
	if err := hosting.DeleteAccount(c.Request.Context(), c.GetInt("id"), account.ID); err != nil {
		hostingError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"status": "deleted"})
}

// GetHostingAccountModels 返回私有账号目录，不调用平台计费模型过滤。
// 模型列表来自当前账号的私有执行组，不读取公共付费模型池。
// 目录存在不代表每个模型都通过测试，应结合 verified_model 展示。
// 已删除和删除中的账号不能继续查询远端私有目录。
// 账号目录查询不消耗平台余额，也不会尝试付费来源回退。
// 控制面错误向用户报告，不能把失败伪装为空模型列表。
// 该端点与统一 Key 模型目录用途不同，后者还必须考虑显式策略。
func GetHostingAccountModels(c *gin.Context) {
	account, ok := hostingAccount(c)
	if !ok {
		return
	}
	if account.Status == "deleted" || account.Status == "deleting" {
		hostingError(c, model.ErrHostingConflict)
		return
	}
	client, err := hosting.NewClient()
	if err != nil {
		hostingError(c, err)
		return
	}
	var result hosting.Models
	if err := client.Call(c.Request.Context(), http.MethodGet, "/accounts/"+url.PathEscape(account.RemoteRef)+"/models", account.UserID, nil, "", &result); err != nil {
		hostingError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

// GetHostingObservation 不触发刷新，主窗口与重置卡未知状态原样保留。
// 读取的是已脱敏的本地观测投影，不会触发原厂刷新。
// 未观察或不可读取时保持未知，不能换算为无限额度。
// 主窗口和重置卡各自带新鲜度，前端不应合并成一个可信标记。
// 额度窗口之间可能重叠，不能对剩余百分比求和。
// 观察值仅供展示和调度参考，不是平台可扣费账户余额。
// 用户要取得新观察值必须显式调用刷新端点。
func GetHostingObservation(c *gin.Context) {
	account, ok := hostingAccount(c)
	if !ok {
		return
	}
	view, err := hostingAccountView(*account)
	if err != nil {
		hostingError(c, err)
		return
	}
	common.ApiSuccess(c, view["observation"])
}

// RefreshHostingObservation 只刷新观察值，永远不消耗原厂重置机会。
// 刷新只读原厂事实，不提交重置、不测试模型、不切换资源来源。
// 返回字段经过 DTO 过滤，完整原厂账户信息不会进入 Oak。
// 执行层返回失败时保留已有缓存，不用空快照覆盖历史数据。
// 持久化失败必须反馈错误，不能声称刷新已可靠保存。
// 已删除和删除中账号拒绝刷新，以免后台操作延长已解绑资源寿命。
// 暂停账号允许查看额度，这并不等于恢复数据面调用权限。
func RefreshHostingObservation(c *gin.Context) {
	account, ok := hostingAccount(c)
	if !ok {
		return
	}
	if account.Status == "deleted" || account.Status == "deleting" {
		hostingError(c, model.ErrHostingConflict)
		return
	}
	client, err := hosting.NewClient()
	if err != nil {
		hostingError(c, err)
		return
	}
	var result hosting.Observation
	if err := client.Call(c.Request.Context(), http.MethodPost, "/accounts/"+url.PathEscape(account.RemoteRef)+"/observation-refresh", account.UserID, gin.H{}, "", &result); err != nil {
		hostingError(c, err)
		return
	}
	if err := hosting.SaveObservation(account, &result); err != nil {
		hostingError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

// TestHostingAccount 的真实探测由用户显式触发，只走该账号的免费资源。
// 验证失败不会转用包月或钱包，也不生成虚假的成功状态。
// 测试是明确的用户动作，可能消耗原厂账号额度但不扣平台钱包。
// 只测试请求的模型，执行协议固定为 OpenAI Responses。
// 返回的模型和协议必须与请求匹配，否则按坏控制面响应处理。
// passed 才能写入 active 和验证时间，failed/inconclusive 均不伪造验证。
// 并发暂停或删除会通过条件更新阻止测试结果重新激活账号。
// 测试不修改任何 Key 策略，也不为 Key 自动添加其他来源。
// 响应只包含结论、原因和延迟，不回传模型生成的正文。
// 验证标记证明一次实际调用，不承诺账号未来永远可用。
// 暂停账号必须先显式恢复，测试按钮不能成为隐式恢复入口。
// 测试请求不接受客户端指定协议，以免界面成功与实际支持能力不一致。
// 模型名称只作为执行层目录中的标识，不参与 URL 或管理地址拼接。
// 已通过其他模型的历史验证不代表当前测试失败时应该转用钱包。
func TestHostingAccount(c *gin.Context) {
	account, ok := hostingAccount(c)
	if !ok {
		return
	}
	if account.Status == "deleted" || account.Status == "deleting" || account.Paused {
		hostingError(c, model.ErrHostingConflict)
		return
	}
	var input struct {
		Model string `json:"model"`
	}
	if !hostingBody(c, &input) {
		return
	}
	if input.Model == "" || len(input.Model) > 256 {
		hostingError(c, &hosting.Error{Status: 400, Code: "hosting_invalid_request"})
		return
	}
	client, err := hosting.NewClient()
	if err != nil {
		hostingError(c, err)
		return
	}
	var result hosting.ProbeResult
	if err := client.Call(c.Request.Context(), http.MethodPost, "/accounts/"+url.PathEscape(account.RemoteRef)+"/test", account.UserID, gin.H{"model": input.Model, "protocol": "openai-responses"}, "", &result); err != nil {
		hostingError(c, err)
		return
	}
	if result.Model != input.Model || result.Protocol != "openai-responses" {
		hostingError(c, &hosting.Error{Status: 502, Code: "hosting_invalid_probe_result"})
		return
	}
	if result.Outcome == "passed" {
		if err := model.DB.Model(account).Where("paused = ? AND status NOT IN ?", false, []string{"deleting", "deleted"}).Updates(map[string]any{"status": "active", "verified_at": time.Now().Unix(), "verified_model": input.Model, "verified_protocol": result.Protocol, "last_error": "", "updated_at": time.Now().Unix()}).Error; err != nil {
			hostingError(c, err)
			return
		}
	} else if result.Outcome != "failed" && result.Outcome != "inconclusive" {
		hostingError(c, &hosting.Error{Status: 502, Code: "hosting_invalid_probe_result"})
		return
	}
	common.ApiSuccess(c, result)
}

// resetView 只解析已脱敏的业务结果，用户重载页面可继续同一个操作。
// 业务 UUID 是用户查找原操作的身份，不是支付订单编号。
// 持久结果先经过固定结构解码，不能直接透传任意历史 JSON。
// 尚无最终结果时 result 允许为 null，界面应显示处理中或未知。
// 观测刷新失败不改变已成功消费的事实，两个结论必须分开。
// 创建与更新时间便于解释长时间未知状态，不代表重试授权。
// 损坏的历史结果返回错误，不能重新消费来掩盖坏记录。
func resetView(operation model.HostingResetOperation) (gin.H, error) {
	var result *hosting.ResetResult
	if operation.ResultJSON != "" {
		if err := common.UnmarshalJsonStr(operation.ResultJSON, &result); err != nil {
			return nil, err
		}
	}
	return gin.H{"operation_id": operation.ID, "account_id": operation.AccountID, "status": operation.Status, "result": result, "created_at": operation.CreatedAt, "updated_at": operation.UpdatedAt}, nil
}

// ConsumeHostingResetCredit 将幂等键绑定到账号和敏感操作证明。
// UUID 由客户端在确认前生成，重复提交复用，不因错误自动更换。
// 客户端确认前生成规范的小写 UUIDv4，服务端拒绝其他格式。
// 敏感证明同时绑定账号和 UUID，重复操作必须重新验证。
// 模型层用账号行锁保证同一账号只有一个未决重置操作。
// 网络超时或控制面故障保留原 UUID，前端不得自动换新 UUID。
// 失败响应中的 data 用于恢复原业务操作，不表示可以直接重试扣卡。
// 业务结果与后续额度观测分开，观察失败不能诱导第二次消费。
// 原厂重置机会不属于平台售卖卡密，与代充值业务没有依赖。
// 操作历史保存足够信息让刷新页面继续原请求，不能依赖浏览器内存。
// 重置成功可能只影响原厂部分窗口，不据此把全部账号额度显示满额。
// 用户要看到明确的未知结论，不能把网络错误显示成“没有扣卡”。
// 业务 UUID 由已验证上下文约束，不能使用 query 参数偷偷替换。
func ConsumeHostingResetCredit(c *gin.Context) {
	account, ok := hostingAccount(c)
	if !ok {
		return
	}
	var input struct {
		OperationID string `json:"operation_id"`
	}
	if !hostingBody(c, &input) {
		return
	}
	id, err := uuid.Parse(input.OperationID)
	if err != nil || id.Version() != 4 || id.String() != input.OperationID {
		hostingError(c, &hosting.Error{Status: 400, Code: "hosting_invalid_operation_id"})
		return
	}
	if !hostingProof(c, "reset", account.ID, input.OperationID) {
		return
	}
	operation, err := hosting.ResetCredits(c.Request.Context(), account.UserID, account.ID, input.OperationID)
	if err != nil {
		if operation != nil {
			view, viewErr := resetView(*operation)
			if viewErr == nil {
				c.JSON(502, gin.H{"success": false, "message": "Hosting request failed", "code": "hosting_reset_outcome_unknown", "data": view})
				return
			}
		}
		hostingError(c, err)
		return
	}
	view, err := resetView(*operation)
	if err != nil {
		hostingError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// GetHostingResetOperations 展示持久历史，未知操作不会因刷新页面丢失。
// 列表只包含当前用户指定账号的操作，删除墓碑也可追溯历史。
// 最近一百条按创建时间排列，避免无限量读取业务日志。
// 准备中和未知状态不能过滤掉，否则重载页面会丢失待处理操作。
// 每一条都使用同一脱敏投影，列表和详情保持一致。
// 查询不向原厂发起消费，也不会替用户自动恢复未知操作。
// 空历史使用 items 空数组，便于前端展示没有操作记录。
func GetHostingResetOperations(c *gin.Context) {
	account, ok := hostingAccount(c)
	if !ok {
		return
	}
	var operations []model.HostingResetOperation
	if err := model.DB.Where("account_id = ? AND user_id = ?", account.ID, account.UserID).Order("created_at DESC").Limit(100).Find(&operations).Error; err != nil {
		hostingError(c, err)
		return
	}
	items := make([]gin.H, 0, len(operations))
	for _, operation := range operations {
		view, err := resetView(operation)
		if err != nil {
			hostingError(c, err)
			return
		}
		items = append(items, view)
	}
	common.ApiSuccess(c, gin.H{"items": items})
}

// GetHostingResetOperation 只查询远端已有状态，不重新执行消费。
// 删除墓碑之后本地最终结果仍然可读，便于解释历史操作。
// 只读查询可以对齐执行层已经保存的状态，但不能重新消费。
// prepared 和 outcome_unknown 需要继续观察，最终状态直接读取本地。
// 远端纯查询找不到操作时返回真实错误，不擅自推断从未消费。
// 最终结果不可被晚到的 unknown 查询回包回退。
// 删除墓碑仍可展示已保存的最终结果，不重新建立远端资源。
// 此入口不要求再次敏感证明，因为它没有原厂写入副作用。
// 所有查询都绑定本人的业务 UUID，知道他人 UUID 也不能读取结果。
func GetHostingResetOperation(c *gin.Context) {
	var operation model.HostingResetOperation
	if err := model.DB.Where("id = ? AND user_id = ?", c.Param("id"), c.GetInt("id")).First(&operation).Error; err != nil {
		hostingError(c, err)
		return
	}
	if operation.Status == "prepared" || operation.Status == "outcome_unknown" {
		account, err := model.GetHostingAccount(operation.UserID, operation.AccountID)
		if err != nil {
			hostingError(c, err)
			return
		}
		if account.Status != "deleted" {
			client, err := hosting.NewClient()
			if err != nil {
				hostingError(c, err)
				return
			}
			var result hosting.ResetResult
			if err := client.Call(c.Request.Context(), http.MethodGet, "/accounts/"+url.PathEscape(account.RemoteRef)+"/reset-operations/"+operation.ID, operation.UserID, nil, "", &result); err != nil {
				hostingError(c, err)
				return
			}
			if err := hosting.PersistResetResult(&operation, &result); err != nil {
				hostingError(c, err)
				return
			}
		}
	}
	view, err := resetView(operation)
	if err != nil {
		hostingError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// ResumeHostingResetOperation 复用原 UUID，未决结果不能通过恢复产生第二次消费。
// 恢复始终读取持久操作的账号和 UUID，不接受客户端改写归属。
// 重新验证证明后才允许显式 POST，GET 观察永远不走本入口。
// 执行层按同一幂等身份恢复，不能再创建一个新的消费意图。
// 已经成功或明确拒绝的操作直接返回已存结果，不再次向原厂消费。
// 未知结果仍未收敛时保持未知，用户可以继续查询并等待确认。
// 恢复失败不能删除原操作记录，否则会丢失阻止重复消费的依据。
func ResumeHostingResetOperation(c *gin.Context) {
	var operation model.HostingResetOperation
	if err := model.DB.Where("id = ? AND user_id = ?", c.Param("id"), c.GetInt("id")).First(&operation).Error; err != nil {
		hostingError(c, err)
		return
	}
	if !hostingProof(c, "reset", operation.AccountID, operation.ID) {
		return
	}
	updated, err := hosting.ResetCredits(c.Request.Context(), operation.UserID, operation.AccountID, operation.ID)
	if err != nil {
		hostingError(c, err)
		return
	}
	view, err := resetView(*updated)
	if err != nil {
		hostingError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// hostingIdentity 延续敏感证明所绑定的真实会话或 PAT 身份。
// 旧式无会话凭据不能启动或接手原厂授权流程。
// 会话身份从认证中间件取得，不接受请求传入 session_id。
// 浏览器会话和有范围 PAT 都具有独立可验证的版本身份。
// 旧凭据缺少这种身份时明确拒绝，不能退回只比较用户编号。
// 此身份仅用于短期授权延续，普通账号列表仍按用户归属读取。
func hostingIdentity(c *gin.Context) (model.AuthSessionIdentity, bool) {
	identity, ok := middleware.GetStepUpIdentity(c)
	if !ok {
		hostingError(c, &hosting.Error{Status: 401, Code: "hosting_authentication_required"})
	}
	return identity, ok
}

// AuditHostingRequests 为所有挂靠变更留下独立安全审计事件。
// 只记录模板路径和 HTTP 结论；授权回调、证明、密钥及请求正文永不读取。
// 审计在处理器结束后记录最终 HTTP 状态，错误操作也保留事实。
// 读操作不生成业务变更事件，避免轮询淹没有价值的审计。
// 模板路由来自 Gin，不能记录含 OAuth code 的原始 URL。
// 此中间件必须放在身份认证之后，确保操作者编号与角色可靠。
// 审计写入沿用项目的可观测错误路径，不另建日志存储。
// 接口失败的详细凭据或上游正文永远不进入 Content。
func AuditHostingRequests(c *gin.Context) {
	c.Next()
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
		return
	}
	model.RecordAuditLog(c, model.AuditLog{UserId: c.GetInt("id"), ActorRole: c.GetInt("role"), Category: model.AuditCategoryOperation, Action: "hosting.manage", Success: c.Writer.Status() < 400, Content: "挂靠资源管理操作"})
}

// GetHostingStatus 为已登录用户返回可安全展示的本地配置状态。
// configured 仅表示配置完整，不代表控制面在线或原厂账号已授权。
// 不访问网络，不消费证明，不创建授权，也不返回管理地址或密钥。
// 正常配置的 code 为空；坏配置使用固定错误码供界面准确提示。
// 未配置属于一次成功的状态查询，因此返回 200 和 configured=false。
// 不写缓存，管理员修复环境后新请求会立即看到当前进程的配置。
// 意外的内部错误仍走失败响应，不能伪装成可修复的缺配置。
func GetHostingStatus(c *gin.Context) {
	if _, err := hosting.NewConfiguredClient(); err != nil {
		var configuration *hosting.Error
		if !errors.As(err, &configuration) {
			hostingError(c, err)
			return
		}
		common.ApiSuccess(c, gin.H{"configured": false, "code": configuration.Code})
		return
	}
	common.ApiSuccess(c, gin.H{"configured": true, "code": ""})
}

// VerifyHostingControlRequest 为执行层核验当前请求，不能用于登录或签发票据。
// 此入口不用 UserAuth：普通登录 JWT 必须被专用用途签名校验拒绝。
// 执行层传入真实请求绑定，成功只返回 owner_ref，不回传任何凭据或声明。
// 只读核验不消费 nonce；30 秒内重复核验有效，业务幂等仍由原操作 UUID 保障。
// 认证失败统一响应并记录无秘密的诊断，不泄露用户存在性或签名匹配细节。
// 限制正文和 Bearer 长度，避免未认证调用造成无界解析或数据库工作。
// 成功返回体与执行层协议固定一致，不包含控制台常用的附加数据。
// 此处理器既不信任普通登录身份，也不把已核验的用户设为控制台会话。
func VerifyHostingControlRequest(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8*1024)
	var binding hosting.RequestBinding
	raw, bearer := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	var owner string
	var err error
	if !bearer || len(c.Request.Header.Values("Authorization")) != 1 || c.ShouldBindJSON(&binding) != nil {
		err = &hosting.Error{Status: http.StatusUnauthorized, Code: "hosting_request_unauthorized"}
	} else {
		owner, err = hosting.VerifyRequestAuthorization(raw, binding)
	}
	if err != nil {
		status := http.StatusServiceUnavailable
		var rejected *hosting.Error
		if errors.As(err, &rejected) && rejected.Status == http.StatusUnauthorized {
			status = http.StatusUnauthorized
		}
		// 日志不记录 Bearer、绑定正文、用户引用或底层验证错误。
		logger.LogWarn(c.Request.Context(), common.LogText("Hosting request authorization failed"))
		c.JSON(status, gin.H{"success": false, "message": "Hosting request authorization failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"owner_ref": owner}})
}
