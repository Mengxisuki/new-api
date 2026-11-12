package controller

import (
	"crypto/sha256"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// 请求只描述购买意图；用户归属始终来自登录上下文。
// quoted_cents 是用户确认的报价，不是可直接落账的金额。
// 两个业务共用幂等协议，业务标识参与订单号摘要以避免互相认领。
type creditCheckoutRequest struct {
	Amount          int64  `json:"amount"`           // 钱包请求沿用原充值数量单位。
	PlanID          int    `json:"plan_id"`          // 订阅只能提交已有套餐编号。
	InvoiceRequired bool   `json:"invoice_required"` // 挂帐阶段没有实收，明确拒绝开票。
	RequestID       string `json:"request_id"`       // 由用户端为一次购买意图生成UUIDv4。
	QuotedCents     int64  `json:"quoted_cents"`     // 服务端重算后精确比较，不将此值直接记账。
}

// RequestCreditTopUp 只供会话路由调用，不把 credit 加入易支付渠道列表。
// 钱包面额沿用现有本地支付报价和冻结额度合同。
func RequestCreditTopUp(c *gin.Context) { requestCreditCheckout(c, model.CreditModuleTopup) }

// RequestCreditSubscription 只供会话路由调用，不使用钱包余额购买订阅。
// 首购和续期都由模型的公共开通函数在事务内决定。
func RequestCreditSubscription(c *gin.Context) {
	requestCreditCheckout(c, model.CreditModuleSubscription)
}

// requestCreditCheckout 先恢复原请求，再为真正的新单计算价格和冻结权益。
// 查询、建单、付款三步允许失败重试；唯一订单号与原请求摘要共同保证幂等。
// 合规关闭仍阻止新付款，已成功订单的结果恢复由下方原单路径完成。
// 同一 UUID 在不同用户和不同业务下是不同订单，不泄露其他用户交易。
func requestCreditCheckout(c *gin.Context, module string) {
	var req creditCheckoutRequest // 统一协议避免两个模块采用不同幂等规则。
	userID := c.GetInt("id")      // 会话路由负责认证，本层不读取可伪造的user_id。
	if err := c.ShouldBindJSON(&req); err != nil || userID <= 0 || req.InvoiceRequired || req.QuotedCents <= 0 || req.QuotedCents > model.MaxCreditCents {
		common.ApiError(c, model.ErrCreditInvalid)
		return
	}
	// 仅接收标准 UUIDv4；规范为小写再摘要，大小写不会绕开幂等。
	key, err := uuid.Parse(req.RequestID)
	if err != nil || key.Version() != 4 || key.Variant() != uuid.RFC4122 || len(req.RequestID) != 36 { // 拒绝非随机版本和宽松解析器接受的非标准写法。
		common.ApiError(c, model.ErrCreditInvalid)
		return
	}
	if (module == model.CreditModuleTopup && (req.Amount <= 0 || req.PlanID != 0)) || (module == model.CreditModuleSubscription && (req.PlanID <= 0 || req.Amount != 0)) { // 拒绝跨业务混合参数，摘要含义保持单一。
		common.ApiError(c, model.ErrCreditInvalid)
		return
	}
	// 固定宽度前缀和摘要合计32字符，不随用户ID长度增长。
	idHash := sha256.Sum256([]byte(fmt.Sprintf("%d/%s/%s", userID, module, key.String())))
	tradeNo := fmt.Sprintf("CR%x", idHash[:15])
	// 摘要保存归一化原意图，包括确认报价；改价不改变原意图。
	requestDigest := sha256.Sum256([]byte(fmt.Sprintf("%s/%d/%d/%d/false", module, req.Amount, req.PlanID, req.QuotedCents)))
	requestHash := fmt.Sprintf("%x", requestDigest) // 存完整摘要以检查同键异参，交易号仅用短摘要。
	// 进程内锁减少同实例竞争，跨实例仍由数据库唯一键和付款行锁保障。
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo) // 任何校验或数据库错误都释放本实例锁。
	found, err := model.FindCreditCheckout(userID, module, tradeNo, requestHash)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !found { // 恢复订单时不重新检查报价和当前购买上限。
		if !requirePaymentCompliance(c) {
			return
		}
		// 只有新单读取当前价格；原单重试不能被新的最低充值额或优惠覆盖。
		err = createCreditCheckout(userID, module, tradeNo, requestHash, req)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	// 合规检查在模型完成函数内只对尚未完成的单生效。
	// 付款 helper 再核对授权和容量，预报价不能替代原子授信检查。
	if module == model.CreditModuleTopup {
		err = model.PayTopUpOnAccount(tradeNo, userID) // 行锁后的冻结金额决定实际占额。
	} else {
		err = model.PaySubscriptionOnAccount(tradeNo, userID) // 原权益快照决定首次开通或续期。
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"trade_no": tradeNo, "status": "success", "payment_kind": model.PaymentProviderCredit}) // 只有完整履约事务成功才对用户确认。
}

// createCreditCheckout 将服务端报价与用户确认值比较后冻结订单。
// 报价按现有本地渠道合同舍入到分，冻结后的金额必须通过严格整分校验。
// 并发唯一键冲突由模型在失败事务结束后恢复，不能在 PostgreSQL 失败事务中查询。
// 此函数不发额、不占授信，付款失败时留下可恢复且有期限的待付款原单。
func createCreditCheckout(userID int, module, tradeNo, requestHash string, req creditCheckoutRequest) error {
	var money float64               // 沿用订单金额字段，转换到分时执行严格边界检查。
	var quota int                   // 只有钱包业务冻结额度，订阅不增加钱包。
	var plan model.SubscriptionPlan // 本地副本供快照序列化，不修改套餐缓存。
	var err error
	if module == model.CreditModuleTopup {
		if req.Amount < getMinTopup() { // 仅新单按当前最低面额限制。
			return common.NewMessage("Top-up amount cannot be less than {{min}}", map[string]any{"min": getMinTopup()})
		}
		quota, err = validateEpayTopUpQuotaForUser(userID, req.Amount) // 复用人民币换算和钱包容量统一入口。
		if err != nil {
			return err
		}
		// 分组、优惠和币种均读取服务端配置，客户端只能提交原面额。
		group, err := model.GetUserGroup(userID, true)
		if err != nil {
			return err
		}
		money = getPayMoney(req.Amount, group)
	} else {
		// 不使用可能过期的套餐缓存，快照取自实际持久配置。
		if err := model.DB.First(&plan, req.PlanID).Error; err != nil {
			return err
		}
		if !plan.Enabled { // 下架套餐不能发起新的挂帐订单。
			return model.ErrCreditUnavailable
		}
		money = plan.PriceAmount
		// 与官方人民币订阅渠道一致：PriceAmount 就是本次应收，不乘充值汇率。
		if money < 0.01 || money > 9999 {
			return model.ErrCreditInvalid
		}
		if err := model.CheckSubscriptionPurchaseLimit(userID, &plan); err != nil { // 预检用于反馈，付款事务仍再次执行限购。
			return err
		}
	}
	if math.IsNaN(money) || math.IsInf(money, 0) { // 坏服务端配置不能触发decimal转换panic。
		return model.ErrCreditInvalid
	}
	money = decimal.NewFromFloat(money).Round(2).InexactFloat64() // 与官方支付发送金额精度一致，不截去厘金额。
	cents, err := model.CreditMoneyCents(money)
	if err != nil {
		return err
	}
	if cents != req.QuotedCents { // 报价变化必须重新确认，不静默收取新价。
		return common.NewMessage("Payment quote changed. Please confirm the new amount.")
	}
	// 债务尚未产生，预检只用于尽早报告未授权或额度不足。
	account, err := model.GetCreditAccount(userID)
	if err != nil {
		return err
	}
	if err := model.CheckCreditAvailable(account, module, cents); err != nil {
		return err
	}
	if module == model.CreditModuleTopup {
		order := &model.TopUp{UserId: userID, Amount: req.Amount, CreditedQuota: quota, Money: money, TradeNo: tradeNo, RequestHash: requestHash, PaymentProvider: model.PaymentProviderCredit, PaymentMethod: model.PaymentProviderCredit, CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending}
		return model.InsertCreditCheckout(order, userID, module, tradeNo, requestHash)
	}
	// 整体快照包括周期、额度、分组和限购，付款不拼接当前权益。
	snapshot, err := common.Marshal(plan) // 使用项目JSON包装，序列化错误不能留下空权益。
	if err != nil {
		return err
	}
	order := &model.SubscriptionOrder{UserId: userID, PlanId: plan.Id, PlanSnapshot: string(snapshot), Money: money, TradeNo: tradeNo, RequestHash: requestHash, PaymentProvider: model.PaymentProviderCredit, PaymentMethod: model.PaymentProviderCredit, CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending}
	return model.InsertCreditCheckout(order, userID, module, tradeNo, requestHash)
}
