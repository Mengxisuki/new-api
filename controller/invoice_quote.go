package controller

import (
	"math"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// invoiceQuoteResponse保留既有data应付字符串，并追加独立开票费用说明。
// 用户充值额度仍在原报价字段中，不用含开票费用的实付反推到账。
// invoice为nil明确表示选择否，不生成申请也不暗中收附加费。
func invoiceQuoteResponse(base, pay float64, invoice *model.InvoiceRequest) gin.H {
	// 所有费用使用数字字段，原data字符串保持现有钱包报价协议。
	result := gin.H{"message": "success", "data": strconv.FormatFloat(pay, 'f', 2, 64), "invoice_required": invoice != nil, "invoice_base_amount": base, "invoice_fee_percent": 0, "invoice_fee_amount": 0}
	if invoice != nil { // 开票费用只取已经校验并冻结的模型结果。
		result["invoice_base_amount"], result["invoice_fee_percent"], result["invoice_fee_amount"] = invoice.BaseAmount, invoice.FeeRate, invoice.FeeAmount
	}
	return result // 此处不查询用户、不重算分组百分比，也不发放余额。
}

// SubscriptionInvoiceQuote只为本地人民币订阅提供含开票费用的预览。
// 实际付款会重新读服务端套餐与分组，浏览器不能提交自己的金额快照。
// 此入口不创建待付款申请，避免单纯浏览价格产生大量无效记录。
// 只有确认下单时才将抬头与金额一起写进订单事务。
func SubscriptionInvoiceQuote(c *gin.Context) {
	var req SubscriptionEpayPayRequest // 首购和续期保持同一参数结构。
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	plan, err := model.GetSubscriptionPlanById(req.PlanId) // 价格来自服务端套餐，不接受浏览器报价。
	if err != nil {
		common.ApiError(c, err) // 原始业务原因由既有多语言协议传递。
		return
	}
	if !plan.Enabled { // 下架套餐不提供新的付款入口。
		common.ApiErrorT(c, "Subscription plan is not enabled")
		return
	}
	if math.IsNaN(plan.PriceAmount) || math.IsInf(plan.PriceAmount, 0) || plan.PriceAmount < 0.01 || plan.PriceAmount > 9999 {
		common.ApiErrorT(c, "Invalid invoice payment amount")
		return
	}
	group, err := model.GetUserGroup(c.GetInt("id"), true) // 不允许客户端伪装费率更低的分组。
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// 是否需要发票的默认值为false，明确选择是才要求抬头并加价。
	invoice, pay, err := model.NewInvoiceRequest(c.GetInt("id"), group, "subscription", plan.PriceAmount, req.InvoiceRequired)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, invoiceQuoteResponse(plan.PriceAmount, pay, invoice)) // 这只是报价，不能视为订阅已购买。
}

// validateInvoiceCallbackAmount仅收紧本轮开票订单，不改变历史非开票合同。
// 签名只能证明报文来源，必须另核对包含附加费的冻结实收。
// 金额使用十进制精确匹配，不能把少付或非整分金额舍入成足额。
// 调用方必须在SDK验签后读取原订单，再传入冻结的应收总价。
// 已成功订单的重复通知也遵循相同金额核对，不给错误通知成功确认。
func validateInvoiceCallbackAmount(required bool, expected float64, paid string) error {
	if !required {
		return nil // 真实历史非开票订单不新增金额解析约束。
	}
	if math.IsNaN(expected) || math.IsInf(expected, 0) || expected <= 0 {
		return common.NewMessage("The payment amount does not match the order")
	}
	actual, err := decimal.NewFromString(paid) // 不经float解析，避免少付金额在二进制转换中失真。
	if err != nil || actual.LessThanOrEqual(decimal.Zero) || !actual.Equal(actual.Round(2)) || !actual.Equal(decimal.NewFromFloat(expected).Round(2)) {
		return common.NewMessage("The payment amount does not match the order")
	}
	return nil
}
