package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// 挂帐统一以人民币分记账，独立于钱包额度和显示币种。
// 上限保持在 JavaScript 安全整数以内，计算不经过浮点金额。
const MaxCreditCents int64 = 10000000000

// 三类销售内容共用一个授信账户，但每类仍须单独打开开关。
// 模块值同时参与债务唯一键和来源单据定位，不能接受任意字符串。
const (
	CreditModuleRecharge     = "recharge"
	CreditModuleTopup        = "topup"
	CreditModuleSubscription = "subscription"
)

const (
	creditStatusOutstanding = "outstanding"
	creditStatusSettled     = "settled"
	creditStatusCancelled   = "cancelled"
	creditStatusRefunded    = "refunded"
	creditStatusWriteOff    = "write_off"
)

// 可挂帐模块白名单集中维护，未知模块必须在入口被拒绝。
func isCreditModuleAllowed(module string) bool {
	return module == CreditModuleRecharge || module == CreditModuleTopup || module == CreditModuleSubscription
}

var (
	ErrCreditInvalid     = errors.New("Invalid credit account request")
	ErrCreditUnavailable = errors.New("On-account payment is not enabled for this user and module")
	ErrCreditLimit       = errors.New("Insufficient available credit")
	ErrCreditConflict    = errors.New("Credit record changed. Refresh and try again.")
)

// 每个用户只有一个授信账户，所有开放模块共用总额度。
// 未配置用户不创建默认授信；停用只阻止新挂帐，不删除欠款。
type CreditAccount struct {
	UserID              int   `json:"user_id" gorm:"primaryKey;autoIncrement:false"` // 用户编号也是唯一授信键。
	Enabled             bool  `json:"enabled"`                                       // 管理员明确开启后才能使用。
	LimitCents          int64 `json:"limit_cents"`                                   // 授信上限，不是钱包余额。
	UsedCents           int64 `json:"used_cents"`                                    // 只由记账、结清与撤销事务维护。
	RechargeEnabled     bool  `json:"recharge_enabled"`                              // 代充值模块开关。
	TopupEnabled        bool  `json:"topup_enabled"`                                 // 钱包充值和手动挂帐调额共用此开关。
	SubscriptionEnabled bool  `json:"subscription_enabled"`                          // 订阅挂帐模块开关。
	Version             int   `json:"version"`                                       // 只用于管理配置的并发编辑检查。
	UpdatedBy           int   `json:"updated_by"`                                    // 最近修改授信的管理员。
	UpdatedAt           int64 `json:"updated_at"`
}

// 每笔债务绑定一个业务交易号，不能因支付重试生成第二笔。
// 结清、退款和核销各自保留凭证，避免额度恢复后丢失对账依据。
type CreditEntry struct {
	ID             int    `json:"id"`
	UserID         int    `json:"user_id" gorm:"index"` // 债务归属，不接受浏览器指定。
	Module         string `json:"module" gorm:"size:32;uniqueIndex:idx_credit_trade,priority:1"`
	TradeNo        string `json:"trade_no" gorm:"size:64;uniqueIndex:idx_credit_trade,priority:2"`
	AmountCents    int64  `json:"amount_cents"`                // 创建后金额不可修改。
	Status         string `json:"status" gorm:"size:24;index"` // outstanding/settled/write_off/cancelled/refunded。
	CreatedAt      int64  `json:"created_at"`
	SettledAt      int64  `json:"settled_at"`                     // 未收款挂帐不能伪造收款时间。
	SettledBy      int    `json:"settled_by"`                     // 线下收款确认人。
	SettlementRef  string `json:"settlement_ref" gorm:"size:128"` // 必须填写线下收款凭证。
	SettlementNote string `json:"settlement_note" gorm:"size:500"`
	WriteOffNote   string `json:"write_off_note" gorm:"size:500"` // 核销原因独立保存，不覆盖结清说明。
	ClosedAt       int64  `json:"closed_at"`                      // 撤销欠款、退款或核销的时间。
	ClosedBy       int    `json:"closed_by"`
	RefundRef      string `json:"refund_ref" gorm:"size:128"` // 已结清订单退款保留原路退款证明。
}

// 管理输入只允许配置字段；占用额和审计字段不能由前端覆盖。
// 总开关与三个模块开关可独立关闭，保存关闭全部模块的配置以便撤销整项授权。
func SaveCreditAccount(input CreditAccount, actor int) error {
	if input.UserID <= 0 || input.Version < 0 || input.LimitCents < 0 || input.LimitCents > MaxCreditCents {
		return ErrCreditInvalid
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		// 先锁用户保证首次配置也串行化，不依赖不存在记录上的行锁。
		var user User
		if err := lockForUpdate(tx).Select("id", "status").First(&user, input.UserID).Error; err != nil {
			return err
		}
		if input.Enabled && user.Status != common.UserStatusEnabled {
			return ErrCreditUnavailable
		}
		var current CreditAccount
		err := lockForUpdate(tx).First(&current, input.UserID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if input.Version != 0 {
				return ErrCreditConflict
			}
			input.UsedCents, input.Version = 0, 1
			input.UpdatedBy, input.UpdatedAt = actor, common.GetTimestamp()
			return tx.Create(&input).Error
		}
		if err != nil {
			return err
		}
		if current.Version != input.Version {
			return ErrCreditConflict
		}
		// 允许停用有欠款账户，但不能把上限降低至实际占用之下。
		if input.LimitCents < current.UsedCents {
			return ErrCreditLimit
		}
		return tx.Model(&current).Updates(map[string]any{
			"enabled": input.Enabled, "limit_cents": input.LimitCents,
			"recharge_enabled": input.RechargeEnabled, "topup_enabled": input.TopupEnabled,
			"subscription_enabled": input.SubscriptionEnabled,
			"version":              current.Version + 1, "updated_by": actor, "updated_at": common.GetTimestamp(),
		}).Error
	})
}

// 没有账户是正常的未授权状态；数据库故障必须向上返回。
// 用户目录只读取本人账户，不能枚举其他人的授信。
func GetCreditAccount(userID int) (*CreditAccount, error) {
	var account CreditAccount
	err := DB.First(&account, userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &account, err
}

// 模块开关和总额度在这里统一判断，付款事务仍须再次调用并原子占额。
func CheckCreditAvailable(account *CreditAccount, module string, amount int64) error {
	if !isCreditModuleAllowed(module) {
		return ErrCreditInvalid
	}
	if account == nil || !account.Enabled {
		return ErrCreditUnavailable
	}
	switch module {
	case CreditModuleRecharge:
		if !account.RechargeEnabled {
			return ErrCreditUnavailable
		}
	case CreditModuleTopup:
		if !account.TopupEnabled {
			return ErrCreditUnavailable
		}
	case CreditModuleSubscription:
		if !account.SubscriptionEnabled {
			return ErrCreditUnavailable
		}
	}
	if amount < 0 || amount > MaxCreditCents {
		return ErrCreditInvalid
	}
	if account.UsedCents > account.LimitCents-amount {
		return ErrCreditLimit
	}
	return nil
}

// ChargeCreditTx 是所有挂帐入口共用的原子占用函数。
// 它只加入调用者事务，不自行提交；查询失败和写入失败均原样返回，不能伪装成未授权。
// 账户预读用于返回明确业务错误，条件 UPDATE 才是并发下的最终额度判断。
func ChargeCreditTx(tx *gorm.DB, userID int, module, tradeNo string, amountCents int64) (*CreditEntry, error) {
	if tx == nil || userID <= 0 || !isCreditModuleAllowed(module) || strings.TrimSpace(tradeNo) == "" || len(tradeNo) > 64 {
		return nil, ErrCreditInvalid
	}
	if amountCents <= 0 || amountCents > MaxCreditCents {
		return nil, ErrCreditInvalid
	}
	tradeNo = strings.TrimSpace(tradeNo)
	var account CreditAccount
	if err := tx.First(&account, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCreditUnavailable
		}
		return nil, err
	}
	if err := CheckCreditAvailable(&account, module, amountCents); err != nil {
		return nil, err
	}
	entry := &CreditEntry{
		UserID: userID, Module: module, TradeNo: tradeNo, AmountCents: amountCents,
		Status: creditStatusOutstanding, CreatedAt: common.GetTimestamp(),
	}
	// 先写唯一债务再做条件占用；占用失败时由调用者事务回滚债务，
	// 不让账户出现没有来源单的 used_cents，也让唯一键先挡住重复业务交易号。
	if err := tx.Create(entry).Error; err != nil {
		return nil, err
	}
	query := tx.Model(&CreditAccount{}).Where("user_id = ? AND enabled = ? AND used_cents <= limit_cents - ?", userID, true, amountCents)
	switch module {
	case CreditModuleRecharge:
		query = query.Where("recharge_enabled = ?", true)
	case CreditModuleTopup:
		query = query.Where("topup_enabled = ?", true)
	case CreditModuleSubscription:
		query = query.Where("subscription_enabled = ?", true)
	default:
		return nil, ErrCreditInvalid
	}
	result := query.UpdateColumn("used_cents", gorm.Expr("used_cents + ?", amountCents))
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrCreditLimit
	}
	return entry, nil
}

type creditSource struct {
	recharge     *AccountRechargeOrder
	topup        *TopUp
	subscription *SubscriptionOrder
	ledger       *QuotaLedger
}

// lockCreditSourceTx 按“债务初读→来源单→用户→债务”锁定来源。
// TopUp 中由订阅镜像生成的记录没有真实支付来源，会继续定位人工账本。
func lockCreditSourceTx(tx *gorm.DB, entry *CreditEntry) (*creditSource, error) {
	source := &creditSource{}
	switch entry.Module {
	case CreditModuleRecharge:
		var order AccountRechargeOrder
		if err := lockForUpdate(tx).Where("trade_no = ?", entry.TradeNo).First(&order).Error; err != nil {
			return nil, err
		}
		if order.PaymentProvider != PaymentProviderCredit || order.UserID != entry.UserID || order.MoneyCents != entry.AmountCents {
			return nil, ErrCreditConflict
		}
		source.recharge = &order
	case CreditModuleTopup:
		var topup TopUp
		err := lockForUpdate(tx).Where("trade_no = ?", entry.TradeNo).First(&topup).Error
		if err == nil {
			if topup.PaymentProvider != PaymentProviderCredit {
				if topup.PaymentProvider != "" {
					return nil, ErrCreditConflict
				}
			} else {
				// 订阅到账会镜像一条 TopUp，但该镜像不能冒充钱包充值来源。
				var subscriptionMirror SubscriptionOrder
				mirrorErr := lockForUpdate(tx).Where("trade_no = ?", entry.TradeNo).First(&subscriptionMirror).Error
				if mirrorErr != nil && !errors.Is(mirrorErr, gorm.ErrRecordNotFound) {
					return nil, mirrorErr
				}
				if mirrorErr != nil {
					if topup.UserId != entry.UserID {
						return nil, ErrCreditConflict
					}
					cents, err := CreditMoneyCents(topup.Money)
					if err != nil || cents != entry.AmountCents {
						return nil, ErrCreditConflict
					}
					source.topup = &topup
					return source, nil
				}
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		var ledger QuotaLedger
		if err := lockForUpdate(tx).Where("trade_no = ?", entry.TradeNo).First(&ledger).Error; err != nil {
			return nil, err
		}
		if ledger.UserID != entry.UserID || ledger.Source != ManualSourceQuota || ledger.ReceivableCents != entry.AmountCents || ledger.CreditEntryID != entry.ID || ledger.Delta <= 0 {
			return nil, ErrCreditConflict
		}
		source.ledger = &ledger
	case CreditModuleSubscription:
		var order SubscriptionOrder
		err := lockForUpdate(tx).Where("trade_no = ?", entry.TradeNo).First(&order).Error
		if err == nil {
			if order.PaymentProvider != PaymentProviderCredit || order.UserId != entry.UserID {
				return nil, ErrCreditConflict
			}
			cents, err := CreditMoneyCents(order.Money)
			if err != nil || cents != entry.AmountCents {
				return nil, ErrCreditConflict
			}
			source.subscription = &order
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		} else {
			var ledger QuotaLedger
			if err := lockForUpdate(tx).Where("trade_no = ?", entry.TradeNo).First(&ledger).Error; err != nil {
				return nil, err
			}
			if ledger.UserID != entry.UserID || ledger.Source != ManualSourceSubscription || ledger.ReceivableCents != entry.AmountCents || ledger.CreditEntryID != entry.ID {
				return nil, ErrCreditConflict
			}
			source.ledger = &ledger
		}
	default:
		return nil, ErrCreditInvalid
	}
	return source, nil
}

// validateCreditSourceForActive 只在仍为 outstanding 时检查可结清来源。
// 已核销的代充值随后可以进入退款流程，因此重复核销不要求来源仍是 paid。
func validateCreditSourceForActive(entry *CreditEntry, source *creditSource) error {
	switch {
	case source.recharge != nil:
		if source.recharge.PaymentStatus != "paid" {
			return ErrCreditConflict
		}
	case source.topup != nil:
		if source.topup.Status != common.TopUpStatusSuccess {
			return ErrCreditConflict
		}
	case source.subscription != nil:
		if source.subscription.Status != common.TopUpStatusSuccess {
			return ErrCreditConflict
		}
	case source.ledger != nil:
		// 人工账本没有支付状态；来源和应收金额已在锁源时完成严格核对。
	default:
		return ErrCreditConflict
	}
	return nil
}

// lockCreditUserTx 把用户行放在债务行之前锁定，随后 releaseCreditTx 再条件更新授信账户。
func lockCreditUserTx(tx *gorm.DB, userID int) error {
	var user User
	return lockForUpdate(tx).Select("id").Where("id = ?", userID).First(&user).Error
}

// SettleCreditEntry 只登记线下整笔收款，不产生钱包额度。
// 锁序固定为初读债务、锁来源、锁用户、锁债务、校验后释放授信。
func SettleCreditEntry(id, actor int, reference, note string) error {
	reference, note = strings.TrimSpace(reference), strings.TrimSpace(note)
	if id <= 0 || reference == "" || len(reference) > 128 || len([]rune(note)) > 500 {
		return ErrCreditInvalid
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var initial CreditEntry
		if err := tx.First(&initial, id).Error; err != nil {
			return err
		}
		source, err := lockCreditSourceTx(tx, &initial)
		if err != nil {
			return err
		}
		if err := lockCreditUserTx(tx, initial.UserID); err != nil {
			return err
		}
		var entry CreditEntry
		if err := lockForUpdate(tx).First(&entry, id).Error; err != nil {
			return err
		}
		if entry.Module != initial.Module || entry.TradeNo != initial.TradeNo || entry.UserID != initial.UserID || entry.AmountCents != initial.AmountCents {
			return ErrCreditConflict
		}
		if entry.Status == creditStatusSettled {
			return nil
		}
		if entry.Status != creditStatusOutstanding || validateCreditSourceForActive(&entry, source) != nil {
			return ErrCreditConflict
		}
		if err := releaseCreditTx(tx, &entry); err != nil {
			return err
		}
		return tx.Model(&entry).Updates(map[string]any{
			"status": creditStatusSettled, "settled_at": common.GetTimestamp(), "settled_by": actor,
			"settlement_ref": reference, "settlement_note": note,
		}).Error
	})
}

// WriteOffCreditEntry 核销整笔坏账，只释放占用，不计实收、不撤销既有权益。
// 重复核销返回成功且保留首次操作者与原因；其他终态不得再次释放额度。
func WriteOffCreditEntry(id, actor int, reason string) error {
	reason = strings.TrimSpace(reason)
	if id <= 0 || reason == "" || len([]rune(reason)) > 500 {
		return ErrCreditInvalid
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var initial CreditEntry
		if err := tx.First(&initial, id).Error; err != nil {
			return err
		}
		source, err := lockCreditSourceTx(tx, &initial)
		if err != nil {
			return err
		}
		if err := lockCreditUserTx(tx, initial.UserID); err != nil {
			return err
		}
		var entry CreditEntry
		if err := lockForUpdate(tx).First(&entry, id).Error; err != nil {
			return err
		}
		if entry.Module != initial.Module || entry.TradeNo != initial.TradeNo || entry.UserID != initial.UserID || entry.AmountCents != initial.AmountCents {
			return ErrCreditConflict
		}
		if entry.Status == creditStatusWriteOff {
			return nil
		}
		if entry.Status != creditStatusOutstanding {
			return ErrCreditConflict
		}
		if err := validateCreditSourceForActive(&entry, source); err != nil {
			return err
		}
		if err := releaseCreditTx(tx, &entry); err != nil {
			return err
		}
		return tx.Model(&entry).Updates(map[string]any{
			"status": creditStatusWriteOff, "closed_at": common.GetTimestamp(), "closed_by": actor,
			"write_off_note": reason,
		}).Error
	})
}

// 额度归还与债务终态必须使用调用者事务；损坏账本直接报错。
// 条件更新同时保护并发结清不同订单，不会出现负占用。
func releaseCreditTx(tx *gorm.DB, entry *CreditEntry) error {
	result := tx.Model(&CreditAccount{}).Where("user_id = ? AND used_cents >= ?", entry.UserID, entry.AmountCents).
		UpdateColumn("used_cents", gorm.Expr("used_cents - ?", entry.AmountCents))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrCreditConflict
	}
	return nil
}
