package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var (
	ErrInvalidUserQuotaAdjustment = errors.New("invalid user quota adjustment")
	ErrUserQuotaPermission        = errors.New("cannot adjust quota for this user role")
)

// UserQuotaAdjustment 是已提交的余额快照，缓存中未结算预扣不包含在其中。
// Duplicate 表示恢复原请求，控制器不能因此重复记录成功业务日志。
type UserQuotaAdjustment struct {
	UserID    int
	Username  string
	Before    int
	After     int
	LedgerID  int
	Duplicate bool
}

// AdjustUserQuota 是唯一的人工调额入口，分类不再是可省略的审计装饰字段。
// 余额与分类记录及债务在一个事务内提交；真实现金和欠款从不由 quota 推导。
// 请求身份在用户锁内再次检查，重复请求返回原快照而不是再计算一次覆盖差额。
//
// 数值边界：
//   - add/subtract 的输入是正的内部额度，override 是新的目标总额。
//   - 负目标余额沿用原调整合同，只能归账务更正，不能伪装现金交易。
//   - 初始余额也必须在安全整数范围内，不能用一次调整掩盖损坏数据。
//   - 正向补偿/赠送只改变可消费权益，不在财务上记一笔现金收入。
//   - 现金字段记录管理员已核实的金额，不等于本次发放额度的面值。
//   - 应收字段参与人民币授信检查，既有消费倍率与此无关。
//   - 覆盖到同一余额只允许无资金的更正，不能制造空转挂帐。
//
// 原记录关联：
//   - OriginalID 只能关联本人的钱包操作，不能指向别人的收入记录。
//   - 撤回赠送和撤回补偿分别核对来源分类，不混淆两种发放原因。
//   - 更正仅记钱包变化，原收款凭证、欠款和实际退款流程均保持不变。
//   - 操作者身份用于请求唯一键，不允许前端把重试改记到另一个人名下。
//
// 提交与可观测性：
//   - 先写来源单再写债务，唯一键冲突时本次余额和占用都回滚。
//   - 锁用户既保护并发覆盖，也与后台订阅和授信设置保持同向锁序。
//   - 已提交结果保留首次 Before/After，后续消费不能改变这份操作证据。
//   - 服务端审计和用户业务日志由调用方按 Duplicate 标志区分。
//   - 缓存只同步首次提交的差额，重试不把同一发放叠加第二次。
//   - 缓存错误沿用既有可观测日志，不通过重跑数据库事务修复。
//   - SQLite 写锁冲突可按原请求重试，不能更换请求键伪装新业务。
//   - PostgreSQL 唯一键冲突必须回滚后再读取并发成功结果。
//   - 模型函数不调用商户、不请求退款，也不修改订阅使用周期。

func AdjustUserQuota(userID, operatorRole int, mode string, value, actor int, finance ManualFinanceInput) (*UserQuotaAdjustment, error) {
	if userID <= 0 || (mode != "add" && mode != "subtract" && mode != "override") || (mode != "override" && value <= 0) {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if actor <= 0 || operatorRole < common.RoleAdminUser { // 不能把财务读写权限当成人工发放权限。
		return nil, ErrUserQuotaPermission
	}
	if value > common.MaxWalletQuota || value < -common.MaxWalletQuota {
		return nil, ErrWalletQuotaLimitExceeded
	}
	if err := finance.Normalize(); err != nil { // 输入语义先校验，不能缺分类后再默认赠送。
		return nil, err
	}
	hash, err := manualFinanceHash(ManualSourceQuota, userID, mode, value, finance)
	if err != nil {
		return nil, err
	}
	var ledger QuotaLedger                     // 用于返回首次提交结果，避免重试读当前余额。
	completed, attemptedInsert := false, false // 缓存副作用与并发恢复分别受两个标记保护。
	err = DB.Transaction(func(tx *gorm.DB) error {
		var user User // 真实角色和余额来自加锁行，而不是客户端预览。
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		if operatorRole != common.RoleRootUser && operatorRole <= user.Role {
			return ErrUserQuotaPermission
		}
		previous, err := findManualFinanceEntry(tx, actor, finance, hash) // 同一请求键不能换用户、金额或原因。
		if err != nil {
			return err
		}
		if previous != nil { // 相同请求已完成，本次不再执行权益变更。
			ledger = *previous
			return nil
		}
		if user.Quota > common.MaxWalletQuota || user.Quota < -common.MaxWalletQuota {
			return ErrWalletQuotaLimitExceeded
		}
		// Decimal 算法与旧入口相同，只在安全钱包上限内转为整数。
		quota := decimal.NewFromInt(int64(value))
		switch mode {
		case "add": // 用 Decimal 加法避开 int 算术中间结果溢出。
			quota = decimal.NewFromInt(int64(user.Quota)).Add(quota)
		case "subtract": // 输入仍为正面额，方向仅由已校验模式决定。
			quota = decimal.NewFromInt(int64(user.Quota)).Sub(quota)
		}
		after, err := common.WalletQuotaFromDecimalStrict(quota)
		if err != nil {
			return ErrWalletQuotaLimitExceeded
		}
		delta := int64(after) - int64(user.Quota)
		// 撤回与账务更正只动额度，不能自动冲现金或归还原授信。
		switch finance.Reason {
		case QuotaAdjustReasonGrant, QuotaAdjustReasonCompensation, QuotaAdjustReasonOnsiteCash, QuotaAdjustReasonOnAccount:
			if delta <= 0 { // 收款或发放原因要求实际新增权益。
				return ErrQuotaLedgerInvalid
			}
		case QuotaAdjustReasonRevokeGrant, QuotaAdjustReasonRevokeCompensation:
			if delta >= 0 { // 撤回原因不能用于正向增额。
				return ErrQuotaLedgerInvalid
			}
		}
		if finance.OriginalID > 0 { // 关联只作审计，不能据此自动释放原有债务。
			var original QuotaLedger // 原单不可变，不对它做增减或改分类。
			if err := tx.Where("id = ? AND user_id = ? AND source = ?", finance.OriginalID, userID, ManualSourceQuota).First(&original).Error; err != nil {
				return err
			}
			if finance.Reason == QuotaAdjustReasonRevokeGrant && original.Reason != QuotaAdjustReasonGrant {
				return ErrQuotaLedgerInvalid
			}
			if finance.Reason == QuotaAdjustReasonRevokeCompensation && original.Reason != QuotaAdjustReasonCompensation {
				return ErrQuotaLedgerInvalid
			}
			// 原记录不可修改，关联仅用于追溯这次额度纠错。
			if finance.Reason != QuotaAdjustReasonCorrection && delta >= 0 {
				return ErrQuotaLedgerInvalid
			}
		}
		beforeSnapshot, afterSnapshot := int64(user.Quota), int64(after)
		ledger = newManualFinanceEntry(&user, actor, ManualSourceQuota, hash, finance)
		ledger.Mode, ledger.QuotaBefore, ledger.QuotaAfter, ledger.Delta = mode, &beforeSnapshot, &afterSnapshot, delta
		// 先插入人工来源单，其唯一键冲突必须使整个事务回滚。
		attemptedInsert = true                           // 唯一键失败与前置校验失败采用不同恢复策略。
		if err := tx.Create(&ledger).Error; err != nil { // 没有可靠记账来源则禁止变更余额。
			return err
		}
		if finance.Reason == QuotaAdjustReasonOnAccount {
			if user.Status != common.UserStatusEnabled { // 停用用户不得新建挂帐债务。
				return ErrCreditUnavailable
			}
			entry, err := ChargeCreditTx(tx, userID, CreditModuleTopup, ledger.TradeNo, finance.ReceivableCents)
			if err != nil {
				return err
			}
			ledger.CreditEntryID = entry.ID // 结清/核销必须能从欠款反查该来源。
			if err := tx.Model(&ledger).Update("credit_entry_id", entry.ID).Error; err != nil {
				return err
			}
		}
		if after != user.Quota {
			result := tx.Model(&User{}).Where("id = ?", userID).Update("quota", after)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 { // 非零差额必须确实写中唯一用户。
				return gorm.ErrRecordNotFound
			}
		}
		completed = true // 只有即将正常提交的事务产生缓存副作用。
		return nil
	})
	if err != nil {
		if !attemptedInsert { // 权限或参数失败不能用历史成功记录掩盖。
			return nil, err
		}
		// PostgreSQL 唯一键冲突后必须先回滚，再读取并发已经提交的原结果。
		previous, lookupErr := findManualFinanceEntry(DB, actor, finance, hash)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if previous == nil { // 恢复不到原结果时保留真实失败，不返回空快照。
			return nil, err
		}
		ledger, completed = *previous, false // 并发胜方已同步余额，本请求只返回结果。
	}
	if ledger.QuotaBefore == nil || ledger.QuotaAfter == nil { // 钱包快照不能引用不适用的订阅记录。
		return nil, ErrQuotaLedgerInvalid
	}
	// 只对首次提交补缓存差额；保持模型请求尚未结算的预扣，不用库中总额覆盖缓存。
	if completed && ledger.Delta != 0 {
		if err := cacheIncrUserQuota(userID, ledger.Delta); err != nil {
			common.SysError(common.LogText("failed to sync manual quota adjustment for user %d: %s", userID, err.Error()))
		}
	}
	return &UserQuotaAdjustment{UserID: userID, Username: ledger.Username, Before: int(*ledger.QuotaBefore), After: int(*ledger.QuotaAfter), LedgerID: ledger.ID, Duplicate: !completed}, nil
}
