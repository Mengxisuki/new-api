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
const CreditModuleRecharge = "recharge"

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
	RechargeEnabled     bool  `json:"recharge_enabled"`                              // 首期唯一可实际使用的模块。
	TopupEnabled        bool  `json:"topup_enabled"`                                 // 钱包充值预留，当前拒绝启用。
	SubscriptionEnabled bool  `json:"subscription_enabled"`                          // 订阅预留，当前拒绝启用。
	Version             int   `json:"version"`                                       // 只用于管理配置的并发编辑检查。
	UpdatedBy           int   `json:"updated_by"`                                    // 最近修改授信的管理员。
	UpdatedAt           int64 `json:"updated_at"`
}

// 每笔债务绑定一个业务交易号，不能因支付重试生成第二笔。
// 保留结清和退款凭证，避免额度恢复后丢失对账依据。
type CreditEntry struct {
	ID             int    `json:"id"`
	UserID         int    `json:"user_id" gorm:"index"` // 债务归属，不接受浏览器指定。
	Module         string `json:"module" gorm:"size:32;uniqueIndex:idx_credit_trade,priority:1"`
	TradeNo        string `json:"trade_no" gorm:"size:64;uniqueIndex:idx_credit_trade,priority:2"`
	AmountCents    int64  `json:"amount_cents"`                // 创建后金额不可修改。
	Status         string `json:"status" gorm:"size:24;index"` // outstanding/settled/cancelled/refunded。
	CreatedAt      int64  `json:"created_at"`
	SettledAt      int64  `json:"settled_at"`                     // 未收款挂帐不能伪造收款时间。
	SettledBy      int    `json:"settled_by"`                     // 线下收款确认人。
	SettlementRef  string `json:"settlement_ref" gorm:"size:128"` // 必须填写线下收款凭证。
	SettlementNote string `json:"settlement_note" gorm:"size:500"`
	ClosedAt       int64  `json:"closed_at"` // 撤销欠款或退还已收款的时间。
	ClosedBy       int    `json:"closed_by"`
	RefundRef      string `json:"refund_ref" gorm:"size:128"` // 已结清订单退款保留原路退款证明。
}

// 管理输入只允许配置字段；占用额和审计字段不能由前端覆盖。
// 版本零仅能创建，修改必须带当前版本，避免并发授权静默覆盖。
// 输入中的用户必须真实存在，不能授信给未注册编号。
// 启用状态与用户停用状态在同一事务里核对。
// 持有用户锁后再读取授信，首次创建也有明确的竞争顺序。
// 不同管理员同时添加同一用户时，只允许首个配置成功。
// 金额可以为零，表示保留配置但没有可消费额度。
// 零额度不会清除尚未结清的账单。
// 修改上限不能小于当前占用，避免可用额度变成负数。
// 停用与模块关闭可以在存在欠款时执行。
// 已有欠款仍保留结清和退款处理入口。
// 只更新配置白名单，不能接收前端提交的占用或审计值。
// 未来接入充值或订阅时需同时扩展支付事务与模块判定。
// 不能只放开这里的布尔值而绕过实际付款链路。
func SaveCreditAccount(input CreditAccount, actor int) error {
	if input.UserID <= 0 || input.Version < 0 || input.LimitCents < 0 || input.LimitCents > MaxCreditCents || input.TopupEnabled || input.SubscriptionEnabled {
		return ErrCreditInvalid
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		// 锁用户保证首次配置也串行化；不依赖不存在记录上的行锁。
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
			input.UsedCents, input.Version = 0, 1 // 新账户只能从零债务开始。
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
			"recharge_enabled": input.RechargeEnabled, "topup_enabled": false, "subscription_enabled": false,
			"version": current.Version + 1, "updated_by": actor, "updated_at": common.GetTimestamp(),
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

// 模块白名单在后端唯一判定，预留字段不等同于支付接入完成。
// 报价检查只提供反馈，付款仍须在同一数据库事务中检查并占额。
func CheckCreditAvailable(account *CreditAccount, module string, amount int64) error {
	if account == nil || !account.Enabled || module != CreditModuleRecharge || !account.RechargeEnabled {
		return ErrCreditUnavailable
	}
	if amount < 0 || amount > MaxCreditCents {
		return ErrCreditInvalid
	}
	if account.UsedCents > account.LimitCents-amount {
		return ErrCreditLimit
	}
	return nil
}

// 结清仅代表财务已经核实线下整笔收款，不触发支付网关扣款。
// 同一记录的重复确认保留第一次凭证，不再次恢复额度。
// 查账本用于定位原业务订单，不以第一次读取结果决定资金动作。
// 获得订单锁之后重新读取账本，确保状态来自本事务。
// 正在退款的订单不能同时登记收款，避免退款与结清语义冲突。
// 已完成履约仍然允许财务收款，办理状态不是欠款状态。
// 授信已关闭也不影响旧债务结清。
// 凭证和备注只作为对账依据，不用于支付渠道调用。
// 当前只支持整笔结清，不接受部分金额字段。
// 重试已结清记录不修改首次登记的时间和操作人。
// 额度归还失败时整个结清事务回滚。
// 不生成余额充值记录，因为收到款项并未购买平台钱包额度。
func SettleCreditEntry(id, actor int, reference, note string) error {
	reference, note = strings.TrimSpace(reference), strings.TrimSpace(note)
	if id <= 0 || reference == "" || len(reference) > 128 || len([]rune(note)) > 500 {
		return ErrCreditInvalid
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var entry CreditEntry
		if err := tx.First(&entry, id).Error; err != nil {
			return err
		}
		// 与付款、退款保持订单在前的锁序，防止财务结清和退款交叉执行。
		if entry.Module != CreditModuleRecharge {
			return ErrCreditInvalid
		}
		var order AccountRechargeOrder
		if err := lockForUpdate(tx).Where("trade_no = ?", entry.TradeNo).First(&order).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&entry, id).Error; err != nil {
			return err
		}
		if entry.Status == "settled" {
			return nil
		}
		if entry.Status != "outstanding" || order.PaymentStatus != "paid" {
			return ErrCreditConflict
		}
		if err := releaseCreditTx(tx, &entry); err != nil {
			return err
		}
		return tx.Model(&entry).Updates(map[string]any{
			"status": "settled", "settled_at": common.GetTimestamp(), "settled_by": actor,
			"settlement_ref": reference, "settlement_note": note,
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
