package model

import (
	"errors"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// 支出类型决定成本口径：订阅按覆盖日摊销，充值采购按余额消耗，其他支出按发生日计入。
const (
	FinanceExpenseSubscription = "subscription"
	FinanceExpenseRecharge     = "recharge"
	FinanceExpenseOther        = "other"
)

// FinanceExpense 是手工登记的采购或订阅支出主记录；金额均为人民币分。
// 充值的剩余余额只能通过余额接口减少，减少量会写入不可变消耗明细。
type FinanceExpense struct {
	ID                    int    `json:"id"`
	Kind                  string `json:"kind" gorm:"type:varchar(32);not null;index"`
	Title                 string `json:"title" gorm:"type:varchar(160);not null"`
	AmountCents           int64  `json:"amount_cents" gorm:"not null"`
	OccurredAt            int64  `json:"occurred_at" gorm:"not null;index"`
	CoverageStart         int64  `json:"coverage_start" gorm:"not null"`
	CoverageEnd           int64  `json:"coverage_end" gorm:"not null"`
	RemainingBalanceCents int64  `json:"remaining_balance_cents" gorm:"not null"`
	Note                  string `json:"note" gorm:"type:text"`
	CreatedAt             int64  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt             int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

// FinanceExpenseConsumption 记录充值采购余额每次减少的差额，保证成本视图可按发生日追溯。
type FinanceExpenseConsumption struct {
	ID                 int   `json:"id"`
	ExpenseID          int   `json:"expense_id" gorm:"not null;index"`
	AmountCents        int64 `json:"amount_cents" gorm:"not null"`
	ConsumedAt         int64 `json:"consumed_at" gorm:"not null;index"`
	BalanceBeforeCents int64 `json:"balance_before_cents" gorm:"not null"`
	BalanceAfterCents  int64 `json:"balance_after_cents" gorm:"not null"`
	CreatedAt          int64 `json:"created_at" gorm:"autoCreateTime"`
}

// FinanceExpenseSummary 同时返回现金支出和按成本口径确认的支出，避免前端自行推算。
type FinanceExpenseSummary struct {
	Start     int64            `json:"start"`
	End       int64            `json:"end"`
	CashCents int64            `json:"cash_cents"`
	CostCents int64            `json:"cost_cents"`
	Items     []FinanceExpense `json:"items"`
}

// ValidateFinanceExpense 校验所有跨类型约束，防止负金额或倒置覆盖期进入账本。
func ValidateFinanceExpense(kind, title string, amount, occurred, coverageStart, coverageEnd, remaining int64) error {
	if title = strings.TrimSpace(title); title == "" || len([]rune(title)) > 160 || amount <= 0 || occurred <= 0 {
		return errors.New("Invalid parameters")
	}
	if kind != FinanceExpenseSubscription && kind != FinanceExpenseRecharge && kind != FinanceExpenseOther {
		return errors.New("Invalid parameters")
	}
	if kind == FinanceExpenseSubscription && (coverageStart <= 0 || coverageEnd <= coverageStart) {
		return errors.New("Invalid parameters")
	}
	if kind != FinanceExpenseSubscription && (coverageStart != 0 || coverageEnd != 0) {
		return errors.New("Invalid parameters")
	}
	if kind == FinanceExpenseRecharge && (remaining < 0 || remaining > amount) {
		return errors.New("Invalid parameters")
	}
	if kind != FinanceExpenseRecharge && remaining != 0 {
		return errors.New("Invalid parameters")
	}
	return nil
}

// CreateFinanceExpense 创建支出主记录；充值采购可登记已知的初始剩余余额。
func CreateFinanceExpense(input FinanceExpense) (*FinanceExpense, error) {
	input.Title = strings.TrimSpace(input.Title)
	if len([]rune(input.Note)) > 5000 {
		return nil, errors.New("Invalid parameters")
	}
	if err := ValidateFinanceExpense(input.Kind, input.Title, input.AmountCents, input.OccurredAt, input.CoverageStart, input.CoverageEnd, input.RemainingBalanceCents); err != nil {
		return nil, err
	}
	if input.Kind == FinanceExpenseSubscription {
		input.RemainingBalanceCents = 0
	}
	if err := DB.Create(&input).Error; err != nil {
		return nil, err
	}
	return &input, nil
}

// ConsumeFinanceExpenseBalance 用余额差额确认充值采购成本，禁止回增或超过当前余额。
func ConsumeFinanceExpenseBalance(id int, remaining, consumedAt int64) (*FinanceExpense, error) {
	if id <= 0 || remaining < 0 || consumedAt <= 0 {
		return nil, errors.New("Invalid parameters")
	}
	var expense FinanceExpense
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&expense, id).Error; err != nil {
			return err
		}
		if expense.Kind != FinanceExpenseRecharge || remaining > expense.RemainingBalanceCents {
			return errors.New("Invalid parameters")
		}
		delta := expense.RemainingBalanceCents - remaining
		if delta == 0 {
			return nil
		}
		entry := FinanceExpenseConsumption{ExpenseID: id, AmountCents: delta, ConsumedAt: consumedAt, BalanceBeforeCents: expense.RemainingBalanceCents, BalanceAfterCents: remaining}
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}
		if err := tx.Model(&expense).Updates(map[string]any{"remaining_balance_cents": remaining, "updated_at": common.GetTimestamp()}).Error; err != nil {
			return err
		}
		expense.RemainingBalanceCents = remaining
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &expense, nil
}

// GetFinanceExpenseSummary 返回现金视图和成本视图；订阅成本按重叠天数线性摊销。
func GetFinanceExpenseSummary(start, end int64) (*FinanceExpenseSummary, error) {
	if start <= 0 || end <= start {
		return nil, errors.New("Invalid parameters")
	}
	var items []FinanceExpense
	if err := DB.Where("occurred_at < ? AND (kind <> ? OR coverage_end > ?)", end, FinanceExpenseSubscription, start).Order("occurred_at DESC, id DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	result := &FinanceExpenseSummary{Start: start, End: end, Items: items}
	for i := range items {
		item := &items[i]
		if item.OccurredAt >= start && item.OccurredAt < end {
			result.CashCents += item.AmountCents
		}
		switch item.Kind {
		case FinanceExpenseSubscription:
			overlapStart, overlapEnd := maxInt64(start, item.CoverageStart), minInt64(end, item.CoverageEnd)
			if overlapEnd > overlapStart {
				days := (item.CoverageEnd - item.CoverageStart) / 86400
				if days > 0 {
					result.CostCents += int64(math.Round(float64(item.AmountCents) * float64((overlapEnd-overlapStart)/86400) / float64(days)))
				}
			}
		case FinanceExpenseRecharge:
			var consumed int64
			if err := DB.Model(&FinanceExpenseConsumption{}).Where("expense_id = ? AND consumed_at >= ? AND consumed_at < ?", item.ID, start, end).Select("COALESCE(SUM(amount_cents), 0)").Scan(&consumed).Error; err != nil {
				return nil, err
			}
			result.CostCents += consumed
		case FinanceExpenseOther:
			if item.OccurredAt >= start && item.OccurredAt < end {
				result.CostCents += item.AmountCents
			}
		}
	}
	result.Items = items
	return result, nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
