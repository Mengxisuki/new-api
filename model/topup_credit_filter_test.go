package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 个人挂帐历史必须在数据库查询阶段过滤，不能只裁剪当前分页结果。
func TestGetUserTopUpsFiltersCreditPaymentMethod(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	previousDB, previousType := DB, common.MainDatabaseType()
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() { DB = previousDB; common.SetMainDatabaseType(previousType) })
	require.NoError(t, db.AutoMigrate(&TopUp{}))
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&[]TopUp{
		{UserId: 7, TradeNo: "credit-order", PaymentMethod: PaymentProviderCredit, PaymentProvider: PaymentProviderCredit, Status: common.TopUpStatusSuccess, CreateTime: now},
		{UserId: 7, TradeNo: "wechat-order", PaymentMethod: "wxpay", PaymentProvider: PaymentProviderWechat, Status: common.TopUpStatusSuccess, CreateTime: now},
	}).Error)
	items, total, err := GetUserTopUps(7, &common.PageInfo{Page: 1, PageSize: 20}, PaymentProviderCredit)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, items, 1)
	require.Equal(t, "credit-order", items[0].TradeNo)
}
