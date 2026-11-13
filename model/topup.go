package model

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type TopUp struct {
	Id     int   `json:"id"`
	UserId int   `json:"user_id" gorm:"index"`
	Amount int64 `json:"amount"`
	// 官方支付冻结下单额度，回调不按可能变化的汇率重新计算。
	CreditedQuota   int             `json:"credited_quota" gorm:"column:credited_quota;type:bigint;not null;default:0"`
	Money           float64         `json:"money"`
	InvoiceRequired bool            `json:"invoice_required" gorm:"not null;default:false"`
	Invoice         *InvoiceRequest `json:"-" gorm:"-"`
	TradeNo         string          `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string          `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider string          `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	CreateTime      int64           `json:"create_time"`
	CompleteTime    int64           `json:"complete_time"`
	Status          string          `json:"status"`
	// 到期时间由创建时间推导，仅用于展示，不另存状态。
	ExpiresAt int64 `json:"expires_at" gorm:"-"`
	// 请求摘要不建立新唯一键，旧记录空值不会造成迁移冲突。
	RequestHash string `json:"-" gorm:"size:64"`
	// 补单仅记录已核实的原渠道漏单，不改变资金分类或原金额。
	ManualCompleted  bool   `json:"manual_completed"`
	ManualActor      int    `json:"manual_actor"`
	ManualReceiptRef string `json:"manual_receipt_ref" gorm:"size:128"`
	ManualNote       string `json:"manual_note" gorm:"size:500"`
}

const (
	PaymentMethodStripe       = "stripe"
	PaymentMethodCreem        = "creem"
	PaymentMethodWaffo        = "waffo"
	PaymentMethodWaffoPancake = "waffo_pancake"
)

const (
	// 挂帐渠道与业务模块独立，三个模块均使用相同 provider。
	PaymentProviderCredit       = "credit"
	PaymentProviderEpay         = "epay"
	PaymentProviderStripe       = "stripe"
	PaymentProviderCreem        = "creem"
	PaymentProviderWaffo        = "waffo"
	PaymentProviderWaffoPancake = "waffo_pancake"
	PaymentProviderAlipay       = "official_alipay"
	PaymentProviderWechat       = "official_wechat"
)

var (
	ErrPaymentMethodMismatch = errors.New("payment method mismatch")
	ErrTopUpNotFound         = errors.New("topup not found")
	ErrTopUpStatusInvalid    = errors.New("topup status invalid")
	ErrTopUpExpired          = common.NewMessage("The payment order has expired. Please create a new top-up order.")
	// The quota errors reach the web console, so they carry message keys.
	ErrInvalidTopUpQuota        error = common.NewMessage("Invalid top-up quota")
	ErrTopUpQuotaLimitExceeded  error = common.NewMessage("Top-up quota limit exceeded")
	ErrWalletQuotaLimitExceeded error = common.NewMessage("Wallet quota limit exceeded")
)

// Insert在同一事务中保存付款订单及可选开票快照。
// 无发票订单沿用原路径，不依赖发票表以保持历史行为。
func (topUp *TopUp) Insert() error {
	if topUp.Invoice == nil {
		return DB.Create(topUp).Error
	}
	topUp.InvoiceRequired = true
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(topUp).Error; err != nil {
			return err
		}
		return CreateInvoiceForOrderTx(tx, topUp.Invoice, topUp.TradeNo)
	})
}

func topUpQuotaMaxCurrent(creditedQuota int) (int, error) {
	if creditedQuota <= 0 || creditedQuota > common.MaxWalletQuota {
		return 0, ErrInvalidTopUpQuota
	}
	return common.MaxWalletQuota - creditedQuota, nil
}

// ValidateTopUpQuotaCapacity performs the user-facing pre-payment check. The
// settlement path repeats the same invariant with an atomic conditional
// update, because the wallet balance can change after checkout creation.
func ValidateTopUpQuotaCapacity(userId int, creditedQuota int) error {
	maxCurrentQuota, err := topUpQuotaMaxCurrent(creditedQuota)
	if err != nil {
		return err
	}

	var user User
	if err := DB.Select("quota").Where("id = ?", userId).First(&user).Error; err != nil {
		return err
	}
	if user.Quota > maxCurrentQuota {
		return ErrTopUpQuotaLimitExceeded
	}
	return nil
}

// creditTopUpQuota atomically enforces the wallet ceiling while adding quota.
// Keeping the predicate and increment in one UPDATE prevents two
// concurrent callbacks from both passing a separate read/check.
func creditTopUpQuota(tx *gorm.DB, userId int, creditedQuota int, updates map[string]any) error {
	maxCurrentQuota, err := topUpQuotaMaxCurrent(creditedQuota)
	if err != nil {
		return err
	}

	updateFields := make(map[string]any, len(updates)+1)
	maps.Copy(updateFields, updates)
	updateFields["quota"] = gorm.Expr("quota + ?", creditedQuota)

	result := tx.Model(&User{}).
		Where("id = ? AND quota <= ?", userId, maxCurrentQuota).
		Updates(updateFields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}

	var count int64
	if err := tx.Model(&User{}).Where("id = ?", userId).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return ErrTopUpQuotaLimitExceeded
}

// creditHubTopUpQuota 在实际入账事务中同时写安全到账事件。
// 只供钱包充值调用；订阅生成的账单不会经过这里。
// 各网关仍保留自己的验签、冻结额度和幂等状态检查。
// 发票保存失败会回滚本事件，不能提前向消费者宣告到账。
func creditHubTopUpQuota(tx *gorm.DB, topUp *TopUp, quota int, updates map[string]any) error {
	if err := creditTopUpQuota(tx, topUp.UserId, quota, updates); err != nil {
		return err
	}
	// 未启动事件服务的独立模型工具保留原入账合同。
	// 正式启动只有迁移完成后才允许开启捕获。
	if !HubCaptureEnabled() {
		return nil
	}
	// 白名单不包含邮箱、支付原始通知或任何商户凭据。
	// 实付金额使用十进制文本，跨币种不猜测人民币含义。
	payload := map[string]any{
		"user_id": topUp.UserId, "trade_no": topUp.TradeNo,
		"paid_amount":    decimal.NewFromFloat(topUp.Money).StringFixed(2),
		"credited_quota": quota, "payment_provider": topUp.PaymentProvider,
		"payment_method": topUp.PaymentMethod,
	}
	// 原微信事件白名单不变；只有挂帐增加应收，现金实收明确为零。
	// 债务金额独立于充值额度，消费者不能把履约时间当成收款时间。
	if topUp.PaymentProvider == PaymentProviderCredit {
		payload["paid_amount"] = "0.00"
		payload["receivable_amount"] = decimal.NewFromFloat(topUp.Money).StringFixed(2)
	}
	return AppendHubEventTx(tx, fmt.Sprintf("topup:%d", topUp.Id), HubTopicTopup, topUp.UserId, payload)
}

func (topUp *TopUp) Update() error {
	var err error
	err = DB.Save(topUp).Error
	return err
}

func GetTopUpById(id int) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("id = ?", id).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func GetTopUpByTradeNo(tradeNo string) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("trade_no = ?", tradeNo).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func UpdatePendingTopUpStatus(tradeNo string, expectedPaymentProvider string, targetStatus string) error {
	if tradeNo == "" {
		return errors.New("payment order number not provided")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		topUp.Status = targetStatus
		return tx.Save(topUp).Error
	})
}

// epayQuotaToCredit 区分新冻结订单和仍待付款的历史美元数量订单。
// 非零冻结值必须完整校验，负值不能借历史路径重新计算出正额度。
// 精确零只代表迁移前已持久化且没有冻结值的真实旧单。
// 旧单只按其原 Amount 契约结算，不读取当前人民币或自定义展示汇率。
func (topUp *TopUp) epayQuotaToCredit() (int, error) {
	if topUp.CreditedQuota != 0 {
		return topUp.QuotaToCredit() // 复用钱包上限及正值校验，不另建一份边界规则。
	}
	// 保留旧订单原有美元数量含义，切换站点币种不能追溯改写已购权益。
	return common.WalletQuotaFromDecimalStrict(
		decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
	)
}

// RechargeEpay 原子完成易支付订单：订单行锁、状态校验、成功更新与用户额度增加
// 在同一个事务内完成，因此同一订单的并发/重复回调（包括多实例部署下）最多充值一次。
// alreadyDone=true 表示订单此前已完成，本次为幂等重复回调。
// 进程内的 LockOrder 只是优化，正确性由本函数的数据库行锁保证。
func RechargeEpay(tradeNo string, actualPaymentMethod string, callerIp string) (alreadyDone bool, err error) {
	if tradeNo == "" {
		return false, errors.New("payment order number not provided")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	var quotaToAdd int
	topUp := &TopUp{}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if topUp.PaymentProvider != PaymentProviderEpay {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess {
			alreadyDone = true
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}
		if actualPaymentMethod != "" && topUp.PaymentMethod != actualPaymentMethod {
			topUp.PaymentMethod = actualPaymentMethod
		}
		var quotaErr error
		// 新订单已按购买时的货币换算冻结权益，不能再把人民币输入当作美元。
		quotaToAdd, quotaErr = topUp.epayQuotaToCredit()
		if quotaErr != nil || quotaToAdd <= 0 {
			return ErrInvalidTopUpQuota
		}
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}
		if err := creditHubTopUpQuota(tx, topUp, quotaToAdd, nil); err != nil {
			return err
		}
		if topUp.InvoiceRequired {
			return MarkInvoicePaidTx(tx, topUp.TradeNo)
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, ErrTopUpNotFound) && !errors.Is(err, ErrPaymentMethodMismatch) && !errors.Is(err, ErrTopUpStatusInvalid) {
			common.SysError(common.LogText("epay topup failed: %s", err.Error()))
		}
		return false, err
	}
	if alreadyDone {
		return true, nil
	}
	syncCreditUserQuotaCache(topUp.UserId, quotaToAdd, common.LogText("epay topup"))

	common.SysLog(common.LogText("Epay top-up succeeded trade_no=%s user_id=%d quota_to_add=%d money=%.2f", topUp.TradeNo, topUp.UserId, quotaToAdd, topUp.Money))
	RecordTopupLog(topUp.UserId, common.NewMessage("Online top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"quota": logger.FormatQuota(quotaToAdd), "amount": fmt.Sprintf("%f", topUp.Money)}), callerIp, topUp.PaymentMethod, PaymentProviderEpay)
	return false, nil
}

func Recharge(referenceId string, customerId string, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("payment order number not provided")
	}

	var quota int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("top-up order not found")
		}

		if topUp.PaymentProvider != PaymentProviderStripe {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("invalid top-up order status")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		err = tx.Save(topUp).Error
		if err != nil {
			return err
		}

		quota, err = common.WalletQuotaFromDecimalStrict(
			decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
		)
		if err != nil || quota <= 0 {
			return ErrInvalidTopUpQuota
		}
		return creditHubTopUpQuota(tx, topUp, quota, map[string]any{
			"stripe_customer": customerId,
		})
	})

	if err != nil {
		common.SysError(common.LogText("topup failed: %s", err.Error()))
		return errors.New("top-up failed")
	}
	syncCreditUserQuotaCache(topUp.UserId, quota, common.LogText("stripe topup"))

	RecordTopupLog(topUp.UserId, common.NewMessage("Online top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"quota": logger.FormatQuota(quota), "amount": topUp.Amount}), callerIp, topUp.PaymentMethod, PaymentMethodStripe)

	return nil
}

// topUpQueryWindowSeconds 限制充值记录查询的时间窗口（秒）。
const topUpQueryWindowSeconds int64 = 30 * 24 * 60 * 60

// topUpQueryCutoff 返回允许查询的最早 create_time（秒级 Unix 时间戳）。
func topUpQueryCutoff() int64 {
	return common.GetTimestamp() - topUpQueryWindowSeconds
}

func GetUserTopUps(userId int, pageInfo *common.PageInfo, paymentMethod ...string) (topups []*TopUp, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	cutoff := topUpQueryCutoff()

	// Get total count within transaction
	query := tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, cutoff)
	if len(paymentMethod) > 0 && paymentMethod[0] != "" {
		query = query.Where("payment_method = ?", paymentMethod[0])
	}
	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated topups within same transaction
	err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	prepareOfficialTopUps(topups)

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// GetAllTopUps 获取全平台的充值记录（管理员使用，不限制时间窗口）
func GetAllTopUps(pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err = tx.Model(&TopUp{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	prepareOfficialTopUps(topups)

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// searchTopUpCountHardLimit 搜索充值记录时 COUNT 的安全上限，
// 防止对超大表执行无界 COUNT 触发 DoS。
const searchTopUpCountHardLimit = 10000

// SearchUserTopUps 按订单号搜索某用户的充值记录
func SearchUserTopUps(userId int, keyword string, pageInfo *common.PageInfo, paymentMethod ...string) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, topUpQueryCutoff())
	if len(paymentMethod) > 0 && paymentMethod[0] != "" {
		query = query.Where("payment_method = ?", paymentMethod[0])
	}
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError(common.LogText("failed to count search topups: %s", err.Error()))
		return nil, 0, common.NewMessage("Failed to search top-up records")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError(common.LogText("failed to search topups: %s", err.Error()))
		return nil, 0, common.NewMessage("Failed to search top-up records")
	}

	prepareOfficialTopUps(topups)

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// SearchAllTopUps 按订单号搜索全平台充值记录（管理员使用，不限制时间窗口）
func SearchAllTopUps(keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{})
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError(common.LogText("failed to count search topups: %s", err.Error()))
		return nil, 0, common.NewMessage("Failed to search top-up records")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError(common.LogText("failed to search topups: %s", err.Error()))
		return nil, 0, common.NewMessage("Failed to search top-up records")
	}

	prepareOfficialTopUps(topups)

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// PayTopUpOnAccount 在同一事务完成钱包履约、债务和授信占用。
// 锁序为订单→用户→债务→授信，失败时不留下半笔余额。
// 已成功请求先返回，关闭授信不影响原成功请求恢复。
// 金额只能来自冻结订单，不使用客户端声称的应付或现行汇率。
func PayTopUpOnAccount(tradeNo string, userID int) error {
	tradeNo = strings.TrimSpace(tradeNo) // 交易号空白不影响查找，但不能扩大用户范围。
	if tradeNo == "" || userID <= 0 {
		return ErrTopUpNotFound
	}
	var topUp TopUp
	var credited int // 零值表示本次未履约，幂等退出不触发提交后副作用。
	err := DB.Transaction(func(tx *gorm.DB) error {
		// 原单行锁同时保护重复点击与跨实例并发。
		if err := lockForUpdate(tx).Where("trade_no = ? AND user_id = ?", tradeNo, userID).First(&topUp).Error; err != nil {
			return err
		}
		if topUp.PaymentProvider != PaymentProviderCredit { // 业务模块名不是支付渠道，不能误认现金单。
			return ErrPaymentMethodMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess { // 先恢复成功结果，再处理授权或过期检查。
			return nil
		}
		if topUp.Status != common.TopUpStatusPending { // 明确失效的订单不能复活。
			return ErrTopUpStatusInvalid
		}
		// 自助挂帐不是网关已收款通知，过期原单不得继续付款。
		if err := creditCheckoutPending(topUp.CreateTime); err != nil {
			return err
		}
		if topUp.InvoiceRequired { // 即使坏数据绕过控制器，也禁止挂帐开票。
			return ErrCreditInvalid
		}
		quota, err := topUp.QuotaToCredit() // 只兑现已保存的整数额度，不能按现价补算。
		if err != nil {
			return err
		}
		cents, err := CreditMoneyCents(topUp.Money) // 应收为人民币分，与钱包额度面值无关。
		if err != nil {
			return err
		}
		// 停用用户不能通过仍开启的授信取得新额度。
		var user User
		if err := lockForUpdate(tx).Select("id", "status").First(&user, userID).Error; err != nil {
			return err
		}
		if user.Status != common.UserStatusEnabled { // 用户停用和授信停用是两项独立约束。
			return ErrCreditUnavailable
		}
		// 公共 helper 统一三模块授信合同，不在充值路径复制占额 SQL。
		if _, err := ChargeCreditTx(tx, userID, CreditModuleTopup, tradeNo, cents); err != nil {
			return err
		}
		topUp.Status, topUp.CompleteTime = common.TopUpStatusSuccess, common.GetTimestamp() // 此时间表示履约，并非现金收款。
		if err := tx.Save(&topUp).Error; err != nil {                                       // 保存失败同时撤销已创建债务。
			return err
		}
		// Hub 事件与余额同事务；挂帐事件实收零，应收单列。
		if err := creditHubTopUpQuota(tx, &topUp, quota, nil); err != nil {
			return err
		}
		credited = quota // 只在事件和余额均成功后标记本次确实入账。
		return nil
	})
	if err != nil {
		return err // 不以缓存或日志成功掩盖事务失败。
	}
	// 只有本次实际完成才更新缓存和日志，重复成功不产生副作用。
	if credited > 0 {
		syncCreditUserQuotaCache(userID, credited, common.LogText("on-account topup"))
		RecordTopupLog(userID, common.NewMessage("Top-up purchased on account, amount: {{amount}}", map[string]any{"amount": fmt.Sprintf("%.2f", topUp.Money)}), "", PaymentProviderCredit, PaymentProviderCredit)
	}
	return nil
}

// ManualCompleteTopUp 仅补回已核实收款但遗漏到账的原现金订单。
// 凭证、操作者与标记随订单同事务保存，不额外写现金账本重复计收。
// 拒绝挂帐和订阅镜像；不能借人工入口将债务转成现金充值。
// 国际订单的实收币种与 Money 含义未统一核实，因此明确拒绝。
func ManualCompleteTopUp(tradeNo, callerIP string, actor int, receiptRef, note string) error {
	tradeNo, receiptRef, note = strings.TrimSpace(tradeNo), strings.TrimSpace(receiptRef), strings.TrimSpace(note)
	if tradeNo == "" || actor <= 0 || receiptRef == "" || len(receiptRef) > 128 || len([]rune(note)) > 500 { // 凭证必填且按持久字段容量限制。
		return common.NewMessage("Invalid payment receipt")
	}
	var topUp TopUp
	var credited int // 提交后副作用只基于本次实际发放。
	err := DB.Transaction(func(tx *gorm.DB) error {
		// 与原渠道通知竞争同一原单锁，不能分别发放两次。
		if err := lockForUpdate(tx).Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
			return err
		}
		if !IsOfficialPaymentProvider(topUp.PaymentProvider) && topUp.PaymentProvider != PaymentProviderEpay { // 仅保留已核实本地现金合同。
			return ErrPaymentMethodMismatch
		}
		// 不能按 Amount=0 猜镜像，使用真正的订阅订单关联排除。
		var subscriptions int64 // 同交易号的订阅付款不能补成钱包充值。
		if err := tx.Model(&SubscriptionOrder{}).Where("trade_no = ?", tradeNo).Count(&subscriptions).Error; err != nil {
			return err
		}
		if subscriptions > 0 { // 即便镜像状态异常也不授予钱包额度。
			return ErrPaymentMethodMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess { // 保留首次到账事实，不覆盖原凭证。
			return nil
		}
		if topUp.Status != common.TopUpStatusPending { // 失败或撤销单须先对账，不能人工强制恢复。
			return ErrTopUpStatusInvalid
		}
		// 此处是线下凭证确认过的已收款，不能套用自助付款过期判断。
		// 金额必须有效但保持原值，不按最新配置或凭证内容重算。
		// 现金补单不占授信，不套用授信金额上限或改写历史小数精度。
		if topUp.Money <= 0 || math.IsNaN(topUp.Money) || math.IsInf(topUp.Money, 0) {
			return common.NewMessage("Invalid payment amount")
		}
		var quota int
		var err error
		if topUp.PaymentProvider == PaymentProviderEpay {
			quota, err = topUp.epayQuotaToCredit() // 历史零冻结值仍遵循原易支付数量合同。
		} else {
			quota, err = topUp.QuotaToCredit() // 官方单缺失冻结额度时不猜测权益。
		}
		if err != nil {
			return err
		}
		// 原 provider、money、credited_quota 均不改写。
		topUp.Status, topUp.CompleteTime = common.TopUpStatusSuccess, common.GetTimestamp()
		topUp.ManualCompleted, topUp.ManualActor = true, actor      // 标记与权益共用事务，不写独立现金收入。
		topUp.ManualReceiptRef, topUp.ManualNote = receiptRef, note // 持久审计不依赖可关闭的日志库。
		if err := tx.Save(&topUp).Error; err != nil {               // 标记失败不允许余额单独到账。
			return err
		}
		if err := creditHubTopUpQuota(tx, &topUp, quota, nil); err != nil { // 事件失败也回滚人工确认。
			return err
		}
		// 开票申请跟随原已收款事实完成，失败则整笔补单回滚。
		if topUp.InvoiceRequired {
			if err := MarkInvoicePaidTx(tx, tradeNo); err != nil {
				return err
			}
		}
		credited = quota // 只有首次补单完成才需要更新缓存。
		return nil
	})
	if err != nil {
		return err
	}
	// 幂等命中不覆盖首次凭证，也不重复日志、缓存或开票。
	if credited > 0 {
		syncCreditUserQuotaCache(topUp.UserId, credited, common.LogText("manual topup"))
		RecordTopupLog(topUp.UserId, common.NewMessage("Administrator completed the order, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"quota": logger.FormatQuota(credited), "amount": fmt.Sprintf("%f", topUp.Money)}), callerIP, topUp.PaymentMethod, topUp.PaymentProvider)
	}
	return nil
}
func RechargeCreem(referenceId string, customerEmail string, customerName string, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("payment order number not provided")
	}

	var quota int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("top-up order not found")
		}

		if topUp.PaymentProvider != PaymentProviderCreem {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("invalid top-up order status")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		err = tx.Save(topUp).Error
		if err != nil {
			return err
		}

		// Creem 直接使用 Amount 作为充值额度（整数）
		quota, err = common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(topUp.Amount))
		if err != nil || quota <= 0 {
			return ErrInvalidTopUpQuota
		}

		// 构建更新字段，优先使用邮箱，如果邮箱为空则使用用户名
		updateFields := map[string]any{}

		// 如果有客户邮箱，尝试更新用户邮箱（仅当用户邮箱为空时）
		if customerEmail != "" {
			// 先检查用户当前邮箱是否为空
			var user User
			err = tx.Where("id = ?", topUp.UserId).First(&user).Error
			if err != nil {
				return err
			}

			// 如果用户邮箱为空，则更新为支付时使用的邮箱
			if user.Email == "" {
				updateFields["email"] = customerEmail
			}
		}

		return creditHubTopUpQuota(tx, topUp, quota, updateFields)
	})

	if err != nil {
		common.SysError(common.LogText("creem topup failed: %s", err.Error()))
		return errors.New("top-up failed")
	}
	syncCreditUserQuotaCache(topUp.UserId, quota, common.LogText("creem topup"))

	RecordTopupLog(topUp.UserId, common.NewMessage("{{provider}} top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"provider": "Creem", "quota": quota, "amount": fmt.Sprintf("%.2f", topUp.Money)}), callerIp, topUp.PaymentMethod, PaymentMethodCreem)

	return nil
}

func RechargeWaffo(tradeNo string, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("payment order number not provided")
	}

	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("top-up order not found")
		}

		if topUp.PaymentProvider != PaymentProviderWaffo {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil // 幂等：已成功直接返回
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("invalid top-up order status")
		}

		quotaToAdd, err = common.WalletQuotaFromDecimalStrict(
			decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
		)
		if err != nil || quotaToAdd <= 0 {
			return ErrInvalidTopUpQuota
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		return creditHubTopUpQuota(tx, topUp, quotaToAdd, nil)
	})

	if err != nil {
		common.SysError(common.LogText("waffo topup failed: %s", err.Error()))
		return errors.New("top-up failed")
	}
	syncCreditUserQuotaCache(topUp.UserId, quotaToAdd, common.LogText("waffo topup"))

	if quotaToAdd > 0 {
		RecordTopupLog(topUp.UserId, common.NewMessage("{{provider}} top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"provider": "Waffo", "quota": logger.FormatQuota(quotaToAdd), "amount": fmt.Sprintf("%.2f", topUp.Money)}), callerIp, topUp.PaymentMethod, PaymentMethodWaffo)
	}

	return nil
}

func RechargeWaffoPancake(tradeNo string) (err error) {
	if tradeNo == "" {
		return errors.New("payment order number not provided")
	}

	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("top-up order not found")
		}

		if topUp.PaymentProvider != PaymentProviderWaffoPancake {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("invalid top-up order status")
		}

		quotaToAdd, err = common.WalletQuotaFromDecimalStrict(
			decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
		)
		if err != nil || quotaToAdd <= 0 {
			return ErrInvalidTopUpQuota
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		return creditHubTopUpQuota(tx, topUp, quotaToAdd, nil)
	})

	if err != nil {
		common.SysError(common.LogText("waffo pancake topup failed: %s", err.Error()))
		return errors.New("top-up failed")
	}
	syncCreditUserQuotaCache(topUp.UserId, quotaToAdd, common.LogText("waffo pancake topup"))

	if quotaToAdd > 0 {
		RecordLog(topUp.UserId, LogTypeTopup, common.NewMessage("{{provider}} top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"provider": "Waffo Pancake", "quota": logger.FormatQuota(quotaToAdd), "amount": fmt.Sprintf("%.2f", topUp.Money)}))
	}

	return nil
}
