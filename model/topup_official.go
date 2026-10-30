package model

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// 官方支付订单保留十分钟支付窗口，重试前必须先关闭原上游订单。
const OfficialTopUpTTLSeconds int64 = 10 * 60

// 官方订单在下单时就冻结额度，缺失值意味着订单数据不完整。
// 历史易支付订单仍走原到账函数，不在此按当前汇率猜测付款权益。
// 同时复用钱包容量范围校验，非法金额不会转为额度赠送。
func (topUp *TopUp) QuotaToCredit() (int, error) {
	if _, err := topUpQuotaMaxCurrent(topUp.CreditedQuota); err != nil {
		return 0, err
	}
	return topUp.CreditedQuota, nil
}

// 官方支付厂商使用独立标识，不与易支付中的wechat方法名混淆。
// 该判断同时用于到期展示、重试资格及人工补单的冻结额度选择。
// 未知厂商不能因为名称相似而享用官方支付逻辑。
func IsOfficialPaymentProvider(provider string) bool {
	return provider == PaymentProviderAlipay || provider == PaymentProviderWechat
}

// 过期时间是展示派生字段，不持久化到新增数据库列。
// 只给官方订单补充十分钟窗口，其他网关保留原支付生命周期。
// 订单创建时间保持不变，读取账单不会延长二维码有效期。
// 前端使用Unix秒，不能在这里乘成毫秒。
func (topUp *TopUp) SetOfficialExpiresAt() {
	if IsOfficialPaymentProvider(topUp.PaymentProvider) {
		topUp.ExpiresAt = topUp.CreateTime + OfficialTopUpTTLSeconds
	}
}

// 判断付款入口窗口是否结束，不判断用户是否已经成功付款。
// 本地通知延迟不能作为拒绝入账的依据。
// 精确到窗口结束时刻即禁止继续支付，减少重复创建旧单风险。
// 非官方订单不受此窗口规则影响。
func (topUp *TopUp) IsOfficialExpired(now int64) bool {
	return IsOfficialPaymentProvider(topUp.PaymentProvider) &&
		now >= topUp.CreateTime+OfficialTopUpTTLSeconds
}

// 对验签成功通知执行原子充值，厂商和渠道必须同时匹配。
// 订单行锁保护重复或并发通知，不能只依赖进程内互斥锁。
// 事务包含成功状态和用户额度递增，二者必须同时提交。
// 失败回滚保持待付款状态，让真实通知能够安全重试。
// 金额比较统一到人民币分精度，避免浮点保存尾差误拒绝。
// 冻结额度必须有效，不能按当前汇率猜测历史付款应获权益。
// 钱包容量上限使用共享原子条件更新，阻止并发突破余额上限。
// 数据库事务外刷新缓存和记录审计，不让日志网络延迟占用订单锁。
// 已成功订单直接幂等返回，不重复增加额度和审计记录。
// 非待支付状态不能认领，避免被取消或替换的订单重复发放。
// 传入厂商是通知入口确定的值，不由用户提交覆盖。
// 回调到达时间不限制结算，只有订单与真实付款信息决定是否入账。
// 返回错误保持具体原因，控制器据此向上游报告失败。
// 这里不操作UserSubscription，钱包充值不能冒充订阅续费。
func CompleteOfficialTopUp(tradeNo string, expectedProvider string, expectedPaymentMethod string, paidAmount decimal.Decimal, callerIP string) error {
	if strings.TrimSpace(tradeNo) == "" {
		return common.NewMessage("Order number not provided")
	}
	if paidAmount.LessThanOrEqual(decimal.Zero) {
		return common.NewMessage("Invalid payment amount")
	}

	// 保留两类数据库标识符引用规则，查询值仍使用参数绑定。
	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}
	var completed bool // 只有本次事务实际发放才刷新缓存和写到账日志。
	var topUp TopUp
	var quotaToAdd int
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(&topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if topUp.PaymentProvider != expectedProvider || topUp.PaymentMethod != expectedPaymentMethod {
			return ErrPaymentMethodMismatch
		}
		// 重复成功通知必须幂等，不能再次增加钱包余额。
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}
		// 支付窗口控制用户能否继续付款，不按回调到达时间否定上游成功交易。
		// 已验签且金额正确的迟到通知仍需入账，避免已付款却拿不到额度。
		if !decimal.NewFromFloat(topUp.Money).Round(2).Equal(paidAmount.Round(2)) {
			return common.NewMessage("The payment amount does not match the order")
		}

		quota, err := topUp.QuotaToCredit()
		if err != nil {
			return err
		}
		quotaToAdd = quota // 使用校验后的冻结值，不读取当前模型或充值倍率。
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(&topUp).Error; err != nil {
			return err
		}
		// 共享原子条件更新同时保护钱包上限和并发到账。
		if err := creditTopUpQuota(tx, topUp.UserId, quotaToAdd, nil); err != nil {
			return err
		}
		completed = true
		return nil
	})
	if err != nil {
		return err
	}
	// 日志和缓存放在提交后，不把外部缓存失败变成重复入账机会。
	if completed {
		syncCreditUserQuotaCache(topUp.UserId, quotaToAdd, common.LogText("official topup"))
		RecordTopupLog(
			topUp.UserId,
			common.NewMessage("Official payment succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"quota": logger.FormatQuota(quotaToAdd), "amount": fmt.Sprintf("%.2f", topUp.Money)}),
			callerIP,
			topUp.PaymentMethod,
			topUp.PaymentProvider,
		)
	}
	return nil
}

// 继续支付前按用户和交易号一起查询，阻止读取他人的付款单。
// 要求订单为官方厂商且处于待支付状态。
// 行锁保证检查时看到已提交的订单状态，而非失效缓存。
// 超时返回明确错误，但不把数据库订单改为失败。
// 保留待支付状态允许先前成功付款的迟到通知完成入账。
// 查询失败不会生成一个空订单继续执行上游关单。
// 返回对象补充派生过期时间，调用者不需要重算窗口。
// 此方法不增加或扣减余额，也不创建替代交易。
func GetPendingOfficialTopUpForRetry(userID int, tradeNo string) (*TopUp, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if userID <= 0 || tradeNo == "" {
		return nil, ErrTopUpNotFound
	}

	var topUp TopUp
	var expired bool // 超时只拒重试，不能改掉已付款迟到通知的可结算状态。
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("trade_no = ? AND user_id = ?", tradeNo, userID).First(&topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if !IsOfficialPaymentProvider(topUp.PaymentProvider) {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}
		if !topUp.IsOfficialExpired(common.GetTimestamp()) {
			return nil
		}
		// 只拒绝再次支付，不改订单状态，以便接受已经付款的迟到回调。
		expired = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if expired {
		return nil, ErrTopUpExpired
	}
	topUp.SetOfficialExpiresAt()
	return &topUp, nil
}

// 上游关单成功后，以单事务替换原待付款订单。
// 事务内再次核对用户归属，防止控制器预检后订单状态变化。
// 同一原单只能替换一次，重复并发操作会被行锁与状态检查拦截。
// 只有官方订单允许此替换，易支付和订阅有各自生命周期。
// 过期只拒绝继续支付，不写失败状态阻断迟到通知。
// 原单标记失败和新单创建必须一起提交，不能出现半完成替换。
// 新单沿用原金额、冻结额度、渠道和用户，不受当前定价影响。
// 调用者提供新交易号，数据库唯一约束防止意外碰撞。
// 新创建时间重建十分钟窗口，但不会修改已关闭原单的创建时间。
// 任何数据库错误都会回滚，不留下已失败原单却无替代单。
// 本方法不调用外部支付API，调用者必须先确认原上游订单关闭。
// 替代订单依然待支付，不授予充值额度。
// 返回值只供再次上游下单，不作为已付款证明。
func ReplacePendingOfficialTopUpForRetry(userID int, tradeNo string, replacementTradeNo string, createTime int64) (*TopUp, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	replacementTradeNo = strings.TrimSpace(replacementTradeNo)
	if userID <= 0 || tradeNo == "" || replacementTradeNo == "" || createTime <= 0 {
		return nil, ErrTopUpNotFound
	}

	var topUp TopUp
	var expired bool // 超时只拒重试，不能改掉已付款迟到通知的可结算状态。
	var replacement TopUp
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("trade_no = ? AND user_id = ?", tradeNo, userID).First(&topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if !IsOfficialPaymentProvider(topUp.PaymentProvider) {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}
		if topUp.IsOfficialExpired(common.GetTimestamp()) {
			// 本地超时不等于上游未付款，旧订单仍保留可结算状态。
			expired = true
			return nil
		}
		// 上游关单确认由控制器负责，此处原子替换本地订单。
		topUp.Status = common.TopUpStatusFailed
		if err := tx.Save(&topUp).Error; err != nil {
			return err
		}
		// 所有价值字段取自原单，调用者仅提供新编号和创建时刻。
		replacement = TopUp{
			UserId:          topUp.UserId,
			Amount:          topUp.Amount,
			CreditedQuota:   topUp.CreditedQuota, // 重试保持原冻结权益。
			Money:           topUp.Money,
			TradeNo:         replacementTradeNo,
			PaymentMethod:   topUp.PaymentMethod,   // 禁止重试时切换具体商户渠道。
			PaymentProvider: topUp.PaymentProvider, // 保持原支付厂商，避免跨网关认领。
			CreateTime:      createTime,
			Status:          common.TopUpStatusPending, // 创建新单不代表已收款。
		}
		// 创建失败触发事务回滚，原单失败状态也不会单独提交。
		return tx.Create(&replacement).Error
	})
	if err != nil {
		return nil, err
	}
	if expired {
		return nil, ErrTopUpExpired
	}
	replacement.SetOfficialExpiresAt()
	return &replacement, nil
}

// 账单只推导展示状态，不因读取列表拒绝已支付但回调稍晚的订单。
// 是否可以再次支付仍由带用户归属校验和行锁的重试入口判断。
// 只修改本次返回对象的展示状态，不执行订单状态更新SQL。
// 展示超时有助于隐藏无效继续支付按钮，但不否定真实上游付款。
// 数据库仍保留待付款状态，以接受延迟投递的成功通知。
// 已成功订单保持成功，不因创建时间早而显示失败。
// 同一列表中的其他厂商不套用官方十分钟窗口。
// 函数不读全表、不修改其他用户的订单，避免查询具有隐藏副作用。
func prepareOfficialTopUps(topups []*TopUp) {
	for _, topUp := range topups {
		topUp.SetOfficialExpiresAt()
		if topUp.Status == common.TopUpStatusPending && topUp.IsOfficialExpired(common.GetTimestamp()) {
			// 上游关单确认由控制器负责，此处原子替换本地订单。
			topUp.Status = common.TopUpStatusFailed
		}
	}
}
