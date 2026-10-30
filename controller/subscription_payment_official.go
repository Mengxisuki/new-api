package controller

import (
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// SubscriptionRequestOfficialPay 为购买和续期创建独立的官方网关订单。
// 请求只包含套餐与渠道标识，售价和商户资料完全从服务端读取。
// 此入口不读取钱包余额，也不生成任何平台余额充值记录。
// 首购及同套餐续期共享付款流程，只有验签后的成功通知可以开通权益。
// 支付交互复用现有官方渠道SDK，微信可返回Native二维码或H5跳转。
// 支付窗口仅约束上游下单；回调仍按实际订单金额和渠道进行匹配。
// 直接订阅付款的收款金额来自套餐售价，不读取充值预设折扣。
// 渠道身份和套餐启用状态都在服务端查询，前端不能覆盖。
// 同套餐续期与首购使用相同请求协议，由到账事务决定延长或新建。
// 金额冻结在SubscriptionOrder，不混用TopUp的额度字段。
// 待付款订单创建失败时不应发出上游请求。
// 上游通知使用既有官方支付地址，不增加另一套签名实现。
// 后台修改套餐价格不会改变已创建订单的应付金额。
// 重复通知最多完成一次订单，由统一模型完成函数控制。
// 返回的二维码本身不授予权限，未付款期间原订阅保持原样。
// 订阅入口不提供“余额不足”分支，因为平台余额完全不参与购买。
// 官方网关失败后用户可以重新付款，但不会自动转用易支付。
// 本函数较长主要是线性的边界校验和订单组装，未抽机械单用辅助函数。
func SubscriptionRequestOfficialPay(c *gin.Context) {
	// 合规开关与现有订阅网关保持一致，不能从新增入口绕过。
	if !requirePaymentCompliance(c) {
		return
	}
	var req SubscriptionEpayPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	// 明确要求官方渠道，不将未知ID回退为易支付或其他网关。
	channel, ok := operation_setting.GetOfficialPaymentChannel(req.PaymentMethod, true)
	if !ok {
		common.ApiErrorT(c, "Payment method does not exist")
		return
	}
	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorT(c, "Subscription plan is not enabled")
		return
	}
	// 与套餐管理金额范围一致；在decimal转换前拒绝非法浮点值。
	if math.IsNaN(plan.PriceAmount) || math.IsInf(plan.PriceAmount, 0) || plan.PriceAmount < 0.01 || plan.PriceAmount > 9999 {
		common.ApiErrorT(c, "Plan amount is too low")
		return
	}
	userID := c.GetInt("id")
	// 此处预检用于购买提示，到账事务仍会在行锁内再次检查限购和续期。
	if err := model.CheckSubscriptionPurchaseLimit(userID, plan); err != nil {
		common.ApiError(c, err)
		return
	}
	// 官方人民币渠道按分收款，订单和发送金额保存同一份精度结果。
	amount := decimal.NewFromFloat(plan.PriceAmount).Round(2)
	provider := model.PaymentProviderAlipay
	if channel.ChannelType == operation_setting.PaymentChannelWechat {
		provider = model.PaymentProviderWechat
	}
	createdAt := common.GetTimestamp()
	order := &model.SubscriptionOrder{
		UserId: userID, PlanId: plan.Id, Money: amount.InexactFloat64(),
		TradeNo:       fmt.Sprintf("SUBUSR%dNO%s%d", userID, common.GetRandomString(6), time.Now().UnixNano()),
		PaymentMethod: channel.ID, PaymentProvider: provider,
		Status: common.TopUpStatusPending, CreateTime: createdAt,
	}
	if err := order.Insert(); err != nil {
		common.ApiErrorT(c, "Failed to create order")
		return
	}
	// 区分订阅单与充值单的前缀也用于回调分流，分流后禁止跨业务回退。
	notifyPath := "/api/user/alipay/notify/" + url.PathEscape(channel.ID)
	if channel.ChannelType == operation_setting.PaymentChannelWechat {
		notifyPath = "/api/user/wechat-pay/notify/" + url.PathEscape(channel.ID)
	}
	expiresAt := createdAt + model.OfficialTopUpTTLSeconds
	result, err := service.CreateOfficialPayment(c.Request.Context(), *channel, order.TradeNo, amount,
		"SUB:"+plan.Title, c.ClientIP(), strings.TrimRight(service.GetCallbackAddress(), "/")+notifyPath,
		paymentReturnPath("/wallet?pay=pending"), time.Unix(expiresAt, 0))
	if err != nil {
		// 下单失败保留失败订单；失效写入失败必须可定位，不能伪造付款成功。
		if expireErr := model.ExpireSubscriptionOrder(order.TradeNo, provider); expireErr != nil {
			logger.LogError(c.Request.Context(), common.LogText("failed to expire official subscription order trade_no=%s error=%q", order.TradeNo, expireErr.Error()))
		}
		logger.LogError(c.Request.Context(), common.LogText("official subscription payment failed to start trade_no=%s channel_id=%s error=%q", order.TradeNo, channel.ID, err.Error()))
		common.ApiErrorT(c, "Failed to start payment. Please check the channel configuration.")
		return
	}
	// 页面收到的只有支付交互与订单标识，不回传SDK响应或商户密钥。
	common.ApiSuccess(c, officialPaymentResponse(order.TradeNo, expiresAt, result))
}

// completeOfficialPaymentCallback 在验签后按订单业务分流，金额仍由模型事务核对。
// 订阅成功只能进入订阅订单完成函数，不能当作充值增加钱包余额。
// 同一平台公钥对应多个渠道，最终核对还必须包含具体渠道ID。
// 前缀不匹配或订阅单不存在时直接失败，不尝试认领另一类业务订单。
// 分流只能依据本平台生成的交易号标识，不能按客户端参数选择。
// SUBUSR订单必须命中订阅完成函数，不存在就返回错误。
// 普通充值单不能为订阅抵扣金额，也不能替代订阅续期。
// 两个业务共用渠道验签，但持久化和权益发放保持独立。
// 返回错误由原厂商通知处理器编码为fail或FAIL。
// 相同平台公钥并不替代具体PaymentMethod的渠道匹配。
// 此函数不预读订单再改写，所有匹配和发放由模型事务原子完成。
// 只传递上游交易引用作为审计信息，不持久化完整付款人资料。
func completeOfficialPaymentCallback(c *gin.Context, channel operation_setting.OfficialPaymentChannel, callback *service.OfficialPaymentCallback, provider string) error {
	if strings.HasPrefix(callback.TradeNo, "SUBUSR") {
		return model.CompleteOfficialSubscriptionOrder(callback.TradeNo, callback.ProviderRef, provider, channel.ID, callback.Amount)
	}
	return model.CompleteOfficialTopUp(callback.TradeNo, provider, channel.ID, callback.Amount, c.ClientIP())
}
