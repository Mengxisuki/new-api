package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 第四期核心回归：现金按购买日、订阅成本按覆盖日、充值成本按余额差额分别统计。
func TestFinanceExpenseSummaryAndBalanceConsumption(t *testing.T) {
	// 使用独立内存库，避免测试污染全局业务数据和其他财务用例。
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	previousDB, previousType := DB, common.MainDatabaseType()
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() { DB = previousDB; common.SetMainDatabaseType(previousType) })
	require.NoError(t, db.AutoMigrate(&FinanceExpense{}, &FinanceExpenseConsumption{}))
	const day = int64(1800000000)
	// 订阅覆盖 30 天，单日成本为 100 分；充值采购初始余额为 100 元。
	subscription, err := CreateFinanceExpense(FinanceExpense{Kind: FinanceExpenseSubscription, Title: "模型订阅", AmountCents: 3000, OccurredAt: day, CoverageStart: day, CoverageEnd: day + 30*86400})
	require.NoError(t, err)
	recharge, err := CreateFinanceExpense(FinanceExpense{Kind: FinanceExpenseRecharge, Title: "采购余额", AmountCents: 10000, OccurredAt: day, RemainingBalanceCents: 10000})
	require.NoError(t, err)
	_, err = ConsumeFinanceExpenseBalance(recharge.ID, 7000, day+86400)
	require.NoError(t, err)
	result, err := GetFinanceExpenseSummary(day+86400, day+2*86400)
	require.NoError(t, err)
	require.EqualValues(t, 0, result.CashCents)
	require.EqualValues(t, 100+3000, result.CostCents)
	_, err = ConsumeFinanceExpenseBalance(recharge.ID, 8000, day+2*86400)
	// 余额只能下降，回增必须拒绝且不能写入第二条反向流水。
	require.Error(t, err)
	require.Len(t, result.Items, 2)
	require.Contains(t, []int{result.Items[0].ID, result.Items[1].ID}, subscription.ID)
}
