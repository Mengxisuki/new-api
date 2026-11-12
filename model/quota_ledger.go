package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// 人工权益操作独立于支付订单和日志库，所有行均与权益变更同事务提交。
// 同一张表保存调额与后台开订阅，不为订阅虚构钱包余额或网关付款单。
// 历史记录只追加；纠错新增关联记录，不覆盖原始凭证与发生时间。
const (
	QuotaAdjustReasonCompensation       = "compensation"
	QuotaAdjustReasonGrant              = "grant"
	QuotaAdjustReasonOnsiteCash         = "offline_cash"
	QuotaAdjustReasonOnAccount          = "on_account"
	QuotaAdjustReasonRevokeGrant        = "revoke_grant"
	QuotaAdjustReasonRevokeCompensation = "revoke_compensation"
	QuotaAdjustReasonCorrection         = "correction"
	QuotaAdjustReasonHistorical         = "unclassified"
	ManualSourceQuota                   = "quota_adjust"
	ManualSourceSubscription            = "subscription_bind"
)

var (
	ErrQuotaLedgerInvalid  = common.NewMessage("Invalid quota adjustment request")
	ErrQuotaLedgerReason   = common.NewMessage("A valid adjustment reason is required")
	ErrQuotaLedgerCash     = common.NewMessage("Offline collection requires the received amount and a reference")
	ErrQuotaLedgerConflict = common.NewMessage("This financial request was already used with different details")
)

// ManualFinanceInput 是两个人工权益入口共同的可写字段。
// 金额独立于内部额度，客户端必须明确填写人民币分，不读取当前汇率反推。
// 请求身份在一次提交及其重试之间保持不变；修改输入必须新建请求身份。
type ManualFinanceInput struct {
	Reason          string `json:"reason"`           // 仅对本期新账分类，历史不自动推断。           // 业务原因，不接受新操作伪装历史未分类。
	CashCents       int64  `json:"cash_cents"`       // 已收款分，已计入总览实收。       // 已实际收取人民币分，仅线下收款有效。
	ReceivableCents int64  `json:"receivable_cents"` // 本期发生的欠款，不代表当前未收余额。 // 尚未收到的应收人民币分，仅挂帐有效。
	CashRef         string `json:"cash_ref"`         // 收款凭证，不参与网关调用。
	Note            string `json:"note"`             // 每次人工变更都必须给出可审计说明。
	RequestID       string `json:"request_id"`       // UUIDv4，数据库唯一约束防止跨进程重复。
	OriginalID      int    `json:"original_id"`      // 可选更正来源，必须是本人的钱包调整。
}

// QuotaLedger 的钱包快照与订阅快照互斥，不适用的字段保留 NULL/空值。
// 订阅保存本次操作前后的完整权益快照，后续续期不会覆盖本次证据。
// Source 和 TradeNo 将人工记录与债务关联，结清可反查并核对原单金额。
type QuotaLedger struct {
	ID                 int    `json:"id"`
	UserID             int    `json:"user_id" gorm:"index:idx_quota_ledger_user_time,priority:1"`                  // 权益归属用户。
	Username           string `json:"username" gorm:"size:64"`                                                     // 操作时名称快照，改名不改历史。
	OperatorID         int    `json:"operator_id" gorm:"uniqueIndex:idx_manual_finance_request,priority:1"`        // 操作者来自会话。
	RequestID          string `json:"request_id" gorm:"size:36;uniqueIndex:idx_manual_finance_request,priority:2"` // 每个操作者的幂等键。
	RequestHash        string `json:"-" gorm:"size:64"`                                                            // 规范化请求摘要，拒绝同键异参。
	TradeNo            string `json:"trade_no" gorm:"size:64;uniqueIndex"`                                         // 人工操作单号，不冒充网关单。
	Source             string `json:"source" gorm:"size:32;index"`                                                 // 钱包调整或订阅开通。
	SourceID           int    `json:"source_id"`                                                                   // 订阅权益实例编号，钱包调整为零。
	Mode               string `json:"mode" gorm:"size:16"`                                                         // add/subtract/override，订阅为grant。
	Reason             string `json:"reason" gorm:"size:32;index"`                                                 // 发放、更正或收款分类。
	QuotaBefore        *int64 `json:"quota_before" gorm:"type:bigint"`                                             // 钱包变更前持久余额，不含在途预扣。
	QuotaAfter         *int64 `json:"quota_after" gorm:"type:bigint"`                                              // 钱包变更后持久余额。
	Delta              int64  `json:"delta" gorm:"type:bigint"`                                                    // 仅钱包净变化，订阅恒为零。
	SubscriptionBefore string `json:"subscription_before,omitempty" gorm:"type:text"`                              // 开通或续期前权益快照。
	SubscriptionAfter  string `json:"subscription_after,omitempty" gorm:"type:text"`                               // 这一次开通或续期后的快照。
	CashCents          int64  `json:"cash_cents"`                                                                  // 已收款，不能与应收重复保存。
	ReceivableCents    int64  `json:"receivable_cents"`                                                            // 挂帐金额，结清前不算现金。
	CashRef            string `json:"cash_ref" gorm:"size:128"`                                                    // 线下收款凭证。
	Note               string `json:"note" gorm:"size:500"`                                                        // 人工说明。
	OriginalID         int    `json:"original_id" gorm:"index"`                                                    // 纠错关联，不自动逆转原款项。
	CreditEntryID      int    `json:"credit_entry_id" gorm:"index"`                                                // 同事务创建的欠款编号。
	CreatedAt          int64  `json:"created_at" gorm:"index:idx_quota_ledger_user_time,priority:2"`               // 原始记账时间。
}

// Normalize 校验两种人工业务共用的字段，不负责决定权益方向。
// 规范化后的字段参与摘要，重试不能借空白差异创建另一种含义。
func (input *ManualFinanceInput) Normalize() error {
	input.Note, input.CashRef = strings.TrimSpace(input.Note), strings.TrimSpace(input.CashRef)
	id, err := uuid.Parse(input.RequestID)
	if err != nil || id.Version() != 4 || id.String() != input.RequestID || input.OriginalID < 0 {
		return ErrQuotaLedgerInvalid
	}
	if input.Note == "" || len([]rune(input.Note)) > 500 || len([]rune(input.CashRef)) > 128 {
		return ErrQuotaLedgerInvalid
	}
	if input.CashCents < 0 || input.ReceivableCents < 0 || input.CashCents > MaxCreditCents || input.ReceivableCents > MaxCreditCents {
		return ErrQuotaLedgerInvalid
	}
	switch input.Reason {
	case QuotaAdjustReasonOnsiteCash:
		if input.CashCents <= 0 || input.CashRef == "" || input.ReceivableCents != 0 {
			return ErrQuotaLedgerCash
		}
	case QuotaAdjustReasonOnAccount:
		if input.ReceivableCents <= 0 || input.CashCents != 0 || input.CashRef != "" {
			return ErrQuotaLedgerInvalid
		}
	case QuotaAdjustReasonGrant, QuotaAdjustReasonCompensation, QuotaAdjustReasonRevokeGrant, QuotaAdjustReasonRevokeCompensation, QuotaAdjustReasonCorrection:
		if input.CashCents != 0 || input.ReceivableCents != 0 || input.CashRef != "" {
			return ErrQuotaLedgerInvalid
		}
	default:
		return ErrQuotaLedgerReason
	}
	return nil
}

// manualFinanceHash 将已校验的业务参数加入同一个摘要。
// 额度、套餐和用户都是摘要的一部分；不能用别人的请求键发放不同权益。
func manualFinanceHash(source string, userID int, mode string, value int, input ManualFinanceInput) (string, error) {
	payload, err := common.Marshal(struct {
		Source string
		UserID int
		Mode   string
		Value  int
		Input  ManualFinanceInput
	}{source, userID, mode, value, input})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(payload)), nil
}

// findManualFinanceEntry 只恢复已提交的同参操作；缺失不是失败，坏摘要必须报冲突。
// 调用方在用户锁之后再查一次，串行化同用户的并发请求。
func findManualFinanceEntry(tx *gorm.DB, actor int, input ManualFinanceInput, hash string) (*QuotaLedger, error) {
	var record QuotaLedger
	err := tx.Where("operator_id = ? AND request_id = ?", actor, input.RequestID).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if record.RequestHash != hash {
		return nil, ErrQuotaLedgerConflict
	}
	return &record, nil
}

// newManualFinanceEntry 统一创建可追溯的人工单头，不执行数据库写入。
// 单号是请求身份的确定性摘要，重试不会出现无关联的随机欠款单。
func newManualFinanceEntry(user *User, actor int, source, hash string, input ManualFinanceInput) QuotaLedger {
	key := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", actor, input.RequestID)))
	return QuotaLedger{UserID: user.Id, Username: user.Username, OperatorID: actor,
		RequestID: input.RequestID, RequestHash: hash, TradeNo: fmt.Sprintf("MAN%x", key[:16]), Source: source,
		Reason: input.Reason, CashCents: input.CashCents, ReceivableCents: input.ReceivableCents,
		CashRef: input.CashRef, Note: input.Note, OriginalID: input.OriginalID, CreatedAt: common.GetTimestamp()}
}

// ListQuotaLedgers 按用户和原因分页查询，日志清理不会删除主库记录。
// 合计与分页使用独立 GORM 会话，避免联表状态被终结操作反复累积。
func ListQuotaLedgers(page, size, userID int, reason, keyword string, start, end int64) ([]QuotaLedger, int64, int64, error) {
	query := DB.Model(&QuotaLedger{}).Where("created_at >= ? AND created_at < ?", start, end)
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	if reason != "" {
		query = query.Where("reason = ?", reason)
	}
	if keyword != "" {
		query = query.Where("username LIKE ? OR trade_no LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	var total, delta int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}
	if err := query.Session(&gorm.Session{}).Select("COALESCE(SUM(delta), 0)").Scan(&delta).Error; err != nil {
		return nil, 0, 0, err
	}
	rows := []QuotaLedger{}
	err := query.Session(&gorm.Session{}).Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error
	return rows, total, delta, err
}

// 分类统计保留来源维度，订阅笔数和钱包额度不可相加为同一权益量。
// cash_cents 是实际线下收款，receivable_cents 只是发生额，并非当前未结清余额。
type QuotaLedgerTotals struct {
	Source          string `json:"source"`           // 原业务来源，订阅与钱包不混成一个权益单位。
	Reason          string `json:"reason"`           // 仅对本期新账分类，历史不自动推断。
	Delta           int64  `json:"delta"`            // 内部额度净变化，不是人民币。
	CashCents       int64  `json:"cash_cents"`       // 已收款分，已计入总览实收。
	ReceivableCents int64  `json:"receivable_cents"` // 本期发生的欠款，不代表当前未收余额。
	Count           int64  `json:"count"`            // 操作笔数，订阅权益不能由 Delta 表达。
}

func SumQuotaLedgers(start, end int64) ([]QuotaLedgerTotals, error) {
	rows := []QuotaLedgerTotals{}
	err := DB.Model(&QuotaLedger{}).Where("created_at >= ? AND created_at < ?", start, end).
		Select("source, reason, COALESCE(SUM(delta), 0) AS delta, COALESCE(SUM(cash_cents), 0) AS cash_cents, COALESCE(SUM(receivable_cents), 0) AS receivable_cents, COUNT(*) AS count").
		Group("source, reason").Order("source, reason").Scan(&rows).Error
	return rows, err
}
