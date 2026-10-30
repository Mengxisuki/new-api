package controller

import (
	"fmt"
	"io"
	"net/http"
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

// 创建官方钱包充值订单，并向上游获取实际付款入口。
// 该函数只由已执行平台额度和钱包容量校验的支付入口调用。
// 渠道最低充值量是额外限制，不能替代平台统一的最大金额检查。
// 商户来源是服务端有效配置，不从客户端JSON直接构造。
// 冻结额度随订单落库，付款通知不会按后续变化的价格重新计算。
// 本地订单必须先创建成功，再向上游发起请求，保证通知可定位。
// 交易号使用用户标识加随机部分，不能与订阅SUBUSR前缀混用。
// 二维码和H5跳转统一作为待付款结果返回，不提前增加余额。
// 上游下单失败记录失败状态，状态更新失败也必须有日志。
// 错误日志只含交易号、渠道和错误阶段，不记录私钥或APIv3密钥。
// 成功下单日志不输出带签名的付款URL，避免凭据进入日志系统。
// 返回前补充十分钟支付窗口，供前端展示剩余付款时间。
// 付款成功只能由经过验签的异步通知及模型事务确认。
func requestOfficialTopUp(c *gin.Context, channel operation_setting.OfficialPaymentChannel, requestAmount int64, creditedQuota int, payMoney float64, userID int) {
	if channel.MinTopUp > 0 && requestAmount < channel.MinTopUp {
		paymentError(c, common.NewMessage("Top-up amount cannot be less than {{min}}", map[string]any{"min": channel.MinTopUp}))
		return
	}

	// 厂商由服务端渠道类型确定，与前端渠道显示名称无关。
	provider := model.PaymentProviderAlipay
	if channel.ChannelType == operation_setting.PaymentChannelWechat {
		provider = model.PaymentProviderWechat
	}
	tradeNo := fmt.Sprintf("USR%dNO%s%d", userID, common.GetRandomString(6), time.Now().Unix())
	topUp := &model.TopUp{
		UserId:          userID,
		Amount:          requestAmount,
		CreditedQuota:   creditedQuota, // 冻结权益，通知不能按修改后的费率重算。
		Money:           payMoney,
		TradeNo:         tradeNo,
		PaymentMethod:   channel.ID, // 具体商户渠道必须在通知时完全匹配。
		PaymentProvider: provider,   // 防止跨支付厂商的签名回调认领订单。
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("官方支付创建充值订单失败 user_id=%d channel_id=%s error=%q", userID, channel.ID, err.Error()))
		paymentError(c, common.NewMessage("Failed to create order"))
		return
	}
	topUp.SetOfficialExpiresAt() // 到期时间只描述付款入口，不作为到账凭据。

	// 持久化成功后才创建上游支付，确保回调有可核对的本地订单。
	result, err := createOfficialTopUpPayment(c, channel, topUp)
	if err != nil {
		// 上游失败与本地失败状态必须都可定位，禁止吞掉持久化错误。
		if statusErr := model.UpdatePendingTopUpStatus(tradeNo, provider, common.TopUpStatusFailed); statusErr != nil {
			logger.LogError(c.Request.Context(), common.LogText("failed to mark official top-up order failed trade_no=%s error=%q", tradeNo, statusErr.Error()))
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("官方支付拉起失败 user_id=%d trade_no=%s channel_id=%s error=%q", userID, tradeNo, channel.ID, err.Error()))
		paymentError(c, common.NewMessage("Failed to start payment. Please check the channel configuration."))
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("官方支付充值订单创建成功 user_id=%d trade_no=%s channel_id=%s provider=%s amount=%d money=%.2f", userID, tradeNo, channel.ID, provider, requestAmount, payMoney))
	common.ApiSuccess(c, officialPaymentResponse(topUp.TradeNo, topUp.ExpiresAt, result))
}

// 把已持久化的充值订单转换为官方下单参数。
// 金额从订单读取并统一到分精度，不能重算本次充值价格。
// 通知地址按渠道类型选择，渠道ID编码为路由路径段。
// 部署默认回调域名由共享服务提供，避免硬编码线上地址。
// 前端回跳进入钱包页，回跳状态只提示等待确认。
// 订单过期时间在SDK请求中传给上游，避免二维码无限有效。
// 客户端IP来自Gin上下文，支付服务会拒绝非法地址。
// 订单标题只包含充值数量，不暴露用户账户或会话密钥。
// 此层不修改订单状态，调用者负责下单失败的持久化处理。
// 服务返回的二维码或跳转链接必须保持与渠道交互方式一致。
func createOfficialTopUpPayment(c *gin.Context, channel operation_setting.OfficialPaymentChannel, topUp *model.TopUp) (*service.OfficialPaymentResult, error) {
	callbackAddress := strings.TrimRight(service.GetCallbackAddress(), "/")
	notifyPath := "/api/user/alipay/notify/" + url.PathEscape(channel.ID)
	if channel.ChannelType == operation_setting.PaymentChannelWechat {
		notifyPath = "/api/user/wechat-pay/notify/" + url.PathEscape(channel.ID)
	}
	return service.CreateOfficialPayment(
		c.Request.Context(),
		channel,
		topUp.TradeNo,
		decimal.NewFromFloat(topUp.Money).Round(2),
		fmt.Sprintf("TUC%d", topUp.Amount),
		c.ClientIP(),
		callbackAddress+notifyPath,
		paymentReturnPath("/wallet?pay=pending"),
		time.Unix(topUp.ExpiresAt, 0),
	)
}

// 构造充值和直接订阅共用的前端支付交互结果。
// 只返回订单标识、有效期和一种付款入口，不返回原始SDK对象。
// 二维码存在时明确标为qr，否则要求服务提供redirect链接。
// 前端按payment_kind选择弹窗或跳转，不通过URL格式猜测。
// 交易号仅供用户识别与本地重试，不能当作已付款凭证。
// 到期时间以Unix秒输出，与前端倒计时及账单保持同一单位。
func officialPaymentResponse(tradeNo string, expiresAt int64, result *service.OfficialPaymentResult) gin.H {
	data := gin.H{"trade_no": tradeNo, "expires_at": expiresAt} // 不包含原SDK响应和商户配置。
	if result.QRCode != "" {
		data["payment_kind"] = "qr"
		data["qr_code"] = result.QRCode
	} else {
		data["payment_kind"] = "redirect"
		data["redirect_url"] = result.RedirectURL
	}
	return data
}

type RetryOfficialTopUpRequest struct {
	TradeNo string `json:"trade_no"`
}

// 继续支付既有未付款充值单，不能用于订阅购买或余额兑换。
// 用户ID来自认证上下文，订单查询必须同时匹配用户归属。
// 合规开关关闭后不允许借继续支付重新下单。
// 无效请求在调用模型或上游之前拒绝。
// 订单超时只阻止再次支付，不依据本地时间否定迟到成功通知。
// 原渠道必须仍可用于新付款，不能切换商户认领旧订单。
// 先向上游关单成功，再创建本地替代订单，防止重复付款。
// 上游已付款或关单失败都应提示用户确认真实订单状态。
// 替换操作在数据库事务中重新验证归属与待付款状态。
// 新订单复制原金额和冻结额度，防止重试时价格变化。
// 旧单在上游关闭后才标记失败，新单继续保留可定位的交易号。
// 新单发起失败记录状态与错误，不静默回退为原二维码。
// 返回交互与初次下单一致，前端可复用二维码弹窗。
// 重试不增加钱包额度，也不对用户已有余额执行扣款。
func RetryOfficialTopUp(c *gin.Context) {
	// 停用收款能力后不能借重试入口继续创建新支付单。
	if !requirePaymentCompliance(c) {
		return
	}
	var req RetryOfficialTopUpRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.TradeNo) == "" {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}

	// 重试独立编号；原付款金额和权益由模型事务复制。
	newTradeNo := fmt.Sprintf("USR%dNO%s%d", c.GetInt("id"), common.GetRandomString(6), time.Now().Unix())
	oldTopUp, err := model.GetPendingOfficialTopUpForRetry(c.GetInt("id"), req.TradeNo)
	if err != nil {
		if err == model.ErrTopUpExpired {
			common.ApiErrorT(c, "The payment order has expired. Please create a new top-up order.")
			return
		}
		common.ApiErrorT(c, "The order does not exist or cannot be paid again in its current state.")
		return
	}
	// 只能继续使用原渠道，不能从客户端选择另一个收款商户。
	channel, ok := operation_setting.GetOfficialPaymentChannel(oldTopUp.PaymentMethod, true)
	if !ok {
		common.ApiErrorT(c, "The original payment channel is currently unavailable.")
		return
	}
	if err := service.CloseOfficialPayment(c.Request.Context(), *channel, oldTopUp.TradeNo); err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("官方支付重试关闭旧订单失败 user_id=%d trade_no=%s channel_id=%s error=%q", c.GetInt("id"), oldTopUp.TradeNo, channel.ID, err.Error()))
		common.ApiErrorT(c, "Unable to close the original payment order. Check whether it has been paid before retrying.")
		return
	}
	topUp, err := model.ReplacePendingOfficialTopUpForRetry(c.GetInt("id"), req.TradeNo, newTradeNo, common.GetTimestamp())
	if err != nil {
		common.ApiErrorT(c, "The order status has changed. Refresh and try again.")
		return
	}

	result, err := createOfficialTopUpPayment(c, *channel, topUp)
	if err != nil {
		// 上游失败与本地失败状态必须都可定位，禁止吞掉持久化错误。
		if statusErr := model.UpdatePendingTopUpStatus(topUp.TradeNo, topUp.PaymentProvider, common.TopUpStatusFailed); statusErr != nil {
			logger.LogError(c.Request.Context(), common.LogText("failed to mark official top-up retry failed trade_no=%s error=%q", topUp.TradeNo, statusErr.Error()))
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("官方支付重试失败 user_id=%d old_trade_no=%s trade_no=%s channel_id=%s error=%q", c.GetInt("id"), req.TradeNo, topUp.TradeNo, channel.ID, err.Error()))
		common.ApiErrorT(c, "Failed to restart payment. Please try again later.")
		return
	}
	common.ApiSuccess(c, officialPaymentResponse(topUp.TradeNo, topUp.ExpiresAt, result))
}

// 支付宝官方异步通知入口，与用户浏览器回跳分离。
// 通知无需用户会话，但必须按渠道配置公钥验证签名。
// 停用渠道仍能验证已付款历史通知，避免用户付款后丢单。
// 路由渠道类型必须是支付宝，不能借微信ID执行支付宝验签。
// 只解析POST表单，参数解析失败直接返回fail。
// 非成功交易状态确认收到后不发放权益。
// 币种必须匹配当前官方人民币收款约定。
// 验签完成后按订单前缀严格分流充值和订阅。
// 订单金额、厂商和具体渠道再次由模型事务核对。
// 事务失败返回fail，使上游按其机制重投而非伪造到账成功。
// 重复通知成功响应由模型幂等状态保证，不重复充值或续期。
// 日志只记录渠道、交易号与错误，不记录完整付款人通知。
func OfficialAlipayNotify(c *gin.Context) {
	channel, ok := operation_setting.GetOfficialPaymentChannel(c.Param("channel_id"), false)
	if !ok || channel.ChannelType != operation_setting.PaymentChannelAlipay {
		c.String(http.StatusNotFound, "fail")
		return
	}
	if err := c.Request.ParseForm(); err != nil {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	// 原始表单先验证签名，未可信金额不会进入数据库结算。
	callback, err := service.VerifyOfficialAlipayCallback(*channel, c.Request.PostForm)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("支付宝官方回调验签失败 channel_id=%s client_ip=%s error=%q", channel.ID, c.ClientIP(), err.Error()))
		c.String(http.StatusUnauthorized, "fail")
		return
	}
	if callback.Status != "TRADE_SUCCESS" && callback.Status != "TRADE_FINISHED" {
		c.String(http.StatusOK, "success")
		return
	}
	// 币种不符不能只按数值匹配订单，避免不同货币等额认领。
	if callback.Currency != "CNY" {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	if err := completeOfficialPaymentCallback(c, *channel, callback, model.PaymentProviderAlipay); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("支付宝官方回调入账失败 trade_no=%s channel_id=%s error=%q", callback.TradeNo, channel.ID, err.Error()))
		c.String(http.StatusBadRequest, "fail")
		return
	}
	c.String(http.StatusOK, "success")
}

// 微信支付APIv3异步通知入口，同时服务充值和直接订阅付款。
// 路由先定位渠道，拒绝不存在或非微信类型的渠道ID。
// 回调原文由路由的匿名请求体上限保护，再交SDK验签解密。
// 签名、公钥ID、商户号和应用ID全部验证后才能进入结算。
// 解析失败不输出原报文，避免泄露密文或付款人资料。
// 非SUCCESS交易状态只确认收到，不触发任何权益发放。
// 已验签成功交易仍须确认币种为人民币。
// 充值与订阅按固定订单前缀分流，不尝试另一业务作为回退。
// 数据库事务同时验证金额、渠道和订单幂等状态。
// 到账失败返回微信FAIL协议，促使上游重试并保留定位信息。
// 成功返回协议约定的SUCCESS，不能用前端页面跳转代替通知。
// 迟到通知按真实付款结果结算，不因本地显示窗口已过而丢弃。
// 用户无需在线，通知也能完成发放；页面之后刷新查询实际权益。
func OfficialWechatNotify(c *gin.Context) {
	channel, ok := operation_setting.GetOfficialPaymentChannel(c.Param("channel_id"), false)
	if !ok || channel.ChannelType != operation_setting.PaymentChannelWechat {
		c.JSON(http.StatusNotFound, gin.H{"code": "FAIL", "message": "channel not found"})
		return
	}
	// 请求体已有路由上限，不将原始通知正文写入日志。
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "FAIL", "message": "invalid body"})
		return
	}
	// SDK同时验证平台签名、商户归属并解密资源。
	callback, err := service.VerifyOfficialWechatCallback(c.Request.Context(), *channel, c.Request.Header, body)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("微信支付官方回调验签失败 channel_id=%s client_ip=%s error=%q", channel.ID, c.ClientIP(), err.Error()))
		c.JSON(http.StatusUnauthorized, gin.H{"code": "FAIL", "message": "signature verification failed"})
		return
	}
	if callback.Status != "SUCCESS" {
		c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "成功"})
		return
	}
	// 币种不符不能只按数值匹配订单，避免不同货币等额认领。
	if callback.Currency != "CNY" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "FAIL", "message": "currency mismatch"})
		return
	}
	if err := completeOfficialPaymentCallback(c, *channel, callback, model.PaymentProviderWechat); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("微信支付官方回调入账失败 trade_no=%s channel_id=%s error=%q", callback.TradeNo, channel.ID, err.Error()))
		c.JSON(http.StatusBadRequest, gin.H{"code": "FAIL", "message": "settlement failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "成功"})
}
