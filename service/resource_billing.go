package service

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// ResourcePlatformAllowed 保留用户组与 Key 分组的交集，不以来源排序扩大权限。
// 免费挂靠不依赖平台分组；平台调用和目录统一使用此规则。
// 未显式指定分组时继承用户组，自动组仍须处于用户可用集合。
// 读取的组名均来自鉴权上下文，不采纳请求正文或自定义归属头。
// 已删除倍率组不可因 Key 仍保存该组名而恢复使用。
func ResourcePlatformAllowed(c *gin.Context) bool {
	group := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
	if group == "" {
		return true
	}
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	_, allowed := GetUserUsableGroups(userGroup)[group]
	return allowed && (group == "auto" || ratio_setting.ContainsGroupRatio(group))
}

// ValidateResourcePlatformAccess 用于交回平台协议前的权限预检，不选择资金来源。
// 不检查余额、不冻结第一候选，因此真实预扣仍可按完整顺序回退。
// 无策略属于旧合同；空策略与纯挂靠策略必须显式拒绝平台执行。
// 适用于图片、音频等原平台链的入口，具体协议校验仍由其适配器负责。
// 查询任务等只读操作保留已有归属校验，不强制重新授权已经完成的消费。
func ValidateResourcePlatformAccess(c *gin.Context) *types.NewAPIError {
	value, exists := c.Get("oak.resource_policy")
	if !exists {
		return nil
	}
	policy, ok := value.(*model.ResourcePolicySnapshot)
	// 存在但类型错误表示内部接线故障，与历史缺省策略严格区分。
	if !ok || policy == nil {
		return resourceFundingError("invalid_resource_policy", http.StatusInternalServerError)
	}
	if !ResourcePlatformAllowed(c) {
		return resourceFundingError("resource_group_forbidden", http.StatusForbidden)
	}
	for _, source := range policy.Sources {
		// 动态账号池已经展开为 hosting，任何挂靠条目都不是平台收费授权。
		if source.Enabled && (source.Kind == BillingSourceWallet || source.Kind == BillingSourceSubscription) {
			return nil
		}
	}
	return resourceFundingError("resource_platform_source_disabled", http.StatusForbidden)
}

// NewBillingSession 是所有平台协议共用的来源入口，图片、任务和 WebSocket 亦适用。
// 已有会话由 PreConsumeBilling 复用；只有首次预扣可以选择资金来源。
// 没有策略的存量 Key 保留原 BillingPreference 与订阅溢出规则。
// 有策略时只尝试明确启用的平台来源，挂靠能力由独立执行器处理。
// 一次成功预扣后来源被冻结，重试、流式补扣与后台任务不得重新择源。
// 只有确定额度不足且预扣已回滚才尝试下一来源，数据库错误必须停止。
// 此处发生在发往上游之前，不能被用于重放已经生成的请求。
// 非文本协议不经过挂靠执行器，但不得因此恢复用户全局收费偏好。
// 输入快照在鉴权时取得；处理途中修改策略不会改变当前请求合同。
// 订阅资格、周额度和钱包余额都在原资金实现中验证，不另建预算缓存。
// 资金工厂只返回实际预扣成功的会话，调用者随后才能发送生成请求。
func NewBillingSession(c *gin.Context, info *relaycommon.RelayInfo, quota int) (*BillingSession, *types.NewAPIError) {
	value, exists := c.Get("oak.resource_policy")
	if !exists || c.GetString("oak.billing_source") != "" {
		// 同步跨来源执行器已精确选源时，沿用该单来源的预扣与退款约束。
		return newBillingSession(c, info, quota)
	}
	policy, ok := value.(*model.ResourcePolicySnapshot)
	if !ok || policy == nil || info == nil {
		return nil, resourceFundingError("invalid_resource_policy", http.StatusInternalServerError)
	}
	if !ResourcePlatformAllowed(c) {
		return nil, resourceFundingError("resource_group_forbidden", http.StatusForbidden)
	}
	requestID := info.RequestId
	// 保存外层请求身份，失败候选不能污染后续订阅的幂等标识。
	var lastErr *types.NewAPIError
	for index, source := range policy.Sources {
		if !source.Enabled || (source.Kind != BillingSourceWallet && source.Kind != BillingSourceSubscription) {
			continue
		}
		// 显式单来源禁止旧偏好再次回退；订阅幂等记录按本次候选区分。
		c.Set("oak.billing_source", source.Kind)
		info.RequestId = fmt.Sprintf("%s.resource.%d", requestID, index)
		session, apiErr := newBillingSession(c, info, quota)
		if apiErr == nil {
			// 成功会话保留候选 RequestId，订阅退款必须引用最初预扣记录。
			freezeResourceFunding(c, policy, source.Kind)
			return session, nil
		}
		// 失败候选不留下冻结来源，原请求身份用于下一候选重新派生。
		c.Set("oak.billing_source", "")
		info.RequestId = requestID
		if apiErr.GetErrorCode() != types.ErrorCodeInsufficientUserQuota {
			// Key 预算不足、权限和未知提交错误均不能靠更换资金来源解决。
			return nil, apiErr
		}
		lastErr = apiErr
	}
	if lastErr != nil {
		// 已试过平台但都不足时保留资金错误；无平台授权则单独返回禁用。
		return nil, lastErr
	}
	return nil, resourceFundingError("resource_platform_source_disabled", http.StatusForbidden)
}

// AuthorizeFreePlatformResource 让零价模型遵守平台授权，同时保持真实零扣费。
// 免费不等于可绕过空策略或仅挂靠策略，亦不强制订阅预扣一个计费单位。
// 该入口只用于定价已明确 FreeModel 的请求，收费请求必须真实预扣。
// 无策略 Key 保留既有免费模型行为，不改变旧计费合同。
// 是否免预扣由站点价格配置决定，这里不覆盖管理员免费模型预扣开关。
// 此分支只记录来源归属，不创建伪订阅记录，也不要求零价模型消耗余额。
// 后续日志的金额仍由真实价格计算，来源标签不代表发生过资金扣减。
// 任务免费分支与同步免费分支共用此规则，不能各自放宽来源授权。
func AuthorizeFreePlatformResource(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	value, exists := c.Get("oak.resource_policy")
	if !exists {
		return nil
	}
	policy, ok := value.(*model.ResourcePolicySnapshot)
	if !ok || policy == nil || info == nil {
		return resourceFundingError("invalid_resource_policy", http.StatusInternalServerError)
	}
	if !ResourcePlatformAllowed(c) {
		return resourceFundingError("resource_group_forbidden", http.StatusForbidden)
	}
	for _, source := range policy.Sources {
		// 只扫描服务端保存的顺序，不补默认钱包，不将 disabled 当作候选。
		if source.Enabled && (source.Kind == BillingSourceWallet || source.Kind == BillingSourceSubscription) {
			// 同步执行器已指定来源时，不能因免费检查再改变候选顺序。
			if frozen := c.GetString("oak.billing_source"); frozen != "" && frozen != source.Kind {
				continue
			}
			info.BillingSource = source.Kind
			freezeResourceFunding(c, policy, source.Kind)
			return nil
		}
	}
	return resourceFundingError("resource_platform_source_disabled", http.StatusForbidden)
}

// freezeResourceFunding 仅记录已选来源，日志与响应使用同一策略版本。
// 不写用户偏好或 Key 策略，编辑只影响后续请求。
// 响应头是诊断信息，客户端不能通过反向提交这些头覆盖服务端选择。
// 会话仍是资金结算的权威状态，上下文仅供其他既有组件读取与记录。
// 异步任务会将会话已冻结的来源持久化，后台无需再次读取用户排序。
// WebSocket 每次新生成使用独立请求上下文，因此不会跨生成继承此冻结。
func freezeResourceFunding(c *gin.Context, policy *model.ResourcePolicySnapshot, source string) {
	c.Set("oak.billing_source", source)
	c.Set("oak.resource_source", source)
	c.Set("oak.resource_revision", policy.Revision)
	c.Header("X-Oak-Resource-Source", source)
	c.Header("X-Oak-Resource-Policy", policy.Revision)
}

// resourceFundingError 返回稳定分类，禁止重试失败的权限决策。
// 错误正文不携带数据库消息、私有账号身份或资金快照。
// 明确拒绝使用来源属于权限错误，不冒充网络故障引发上游重试。
func resourceFundingError(code string, status int) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(code), types.ErrorCode(code), status, types.ErrOptionWithSkipRetry())
}
