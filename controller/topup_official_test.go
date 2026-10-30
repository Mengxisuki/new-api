package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 验签算法由 service 真实加密回归覆盖，本用例验证通知进入正确账务路径。
// 充值和订阅共用官方回调，不能因为共享接口就将订阅款记入钱包。
// 只隔离数据库句柄；模型订单完成、续期和幂等均调用真实实现。
func TestOfficialPaymentCallbackSeparatesSubscriptionFromWallet(t *testing.T) {
	gin.SetMode(gin.TestMode) // 真实请求上下文沿用 Gin 测试模式，不启动 HTTP 服务。
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false // 隔离未启动的 Redis，不模拟充值或日志业务本身。
	t.Cleanup(func() { common.RedisEnabled = oldRedis })
	db, err := gorm.Open(sqlite.Open("file:official_callback?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)                // 付款事务内查询数据库时间需要共享内存库的第二个连接。
	oldDB, oldLog := model.DB, model.LOG_DB // 保存宿主句柄，避免测试退出后污染其他控制器。
	oldMain, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB, model.LOG_DB = db, db // 日志也使用隔离数据库，不能产生真实业务日志。
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLog // 先还原包状态，再关闭临时连接。
		common.SetDatabaseTypes(oldMain, oldLogType)
		assert.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionOrder{}, &model.TopUp{}, &model.Log{}))
	user := model.User{Username: "official-callback", AffCode: "callback", Quota: 700, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	plan := model.SubscriptionPlan{Title: "Monthly", DurationUnit: "month", DurationValue: 1, TotalAmount: 1000, Enabled: true}
	require.NoError(t, db.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id) // 不允许另一个数据库的同 ID 套餐缓存改变本次权益。
	t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(plan.Id) })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/api/user/wechat-pay/notify/wechat-main", nil)
	channel := operation_setting.OfficialPaymentChannel{ID: "wechat-main", ChannelType: "wechat"}
	// 一笔待支付订阅和一笔充值金额相同，必须按业务前缀及实际订单区分。
	order := model.SubscriptionOrder{UserId: user.Id, PlanId: plan.Id, Money: 10, TradeNo: "SUBUSR-test", PaymentProvider: model.PaymentProviderWechat, PaymentMethod: channel.ID, Status: common.TopUpStatusPending}
	require.NoError(t, order.Insert()) // 本地待支付记录存在仍不代表已获得订阅权益。

	topup := model.TopUp{UserId: user.Id, Amount: 1, CreditedQuota: 1234, Money: 10, TradeNo: "USR-test", PaymentProvider: model.PaymentProviderWechat, PaymentMethod: channel.ID, Status: common.TopUpStatusPending, CreateTime: common.GetTimestamp()}
	require.NoError(t, topup.Insert()) // 使用冻结额度而非重算当前汇率，守住充值计费契约。

	callback := service.OfficialPaymentCallback{TradeNo: order.TradeNo, Amount: decimal.NewFromInt(10)}
	// 签名有效也不能给不存在的订阅号充值，业务之间禁止失败回退。
	callback.TradeNo = "SUBUSR-missing"
	require.Error(t, completeOfficialPaymentCallback(ctx, channel, &callback, model.PaymentProviderWechat))
	callback.TradeNo = order.TradeNo        // 切回存在的订阅单，使少付金额断言不受订单缺失影响。
	callback.Amount = decimal.NewFromInt(9) // 金额由订单核对，少付不可开通。
	require.Error(t, completeOfficialPaymentCallback(ctx, channel, &callback, model.PaymentProviderWechat))
	callback.Amount = decimal.NewFromInt(10) // 恢复正确金额后再单独验证商户渠道隔离。
	wrongChannel := operation_setting.OfficialPaymentChannel{ID: "another-merchant"}
	require.ErrorIs(t, completeOfficialPaymentCallback(ctx, wrongChannel, &callback, model.PaymentProviderWechat), model.ErrPaymentMethodMismatch)
	for range 2 { // 同一订阅付款重复回调只创建一次权益。
		require.NoError(t, completeOfficialPaymentCallback(ctx, channel, &callback, model.PaymentProviderWechat))
	}
	// 通知重复处理后的余额必须原样保留，不能把订阅价赠送成可消费余额。
	var paidUser model.User
	require.NoError(t, db.First(&paidUser, user.Id).Error)
	assert.Equal(t, 700, paidUser.Quota) // 订阅支付不赠送钱包额度，也不消费已有余额。
	// 查询实际权益表，避免只检查回调返回成功而漏掉重复开通。
	var subscriptions []model.UserSubscription
	require.NoError(t, db.Where("user_id = ?", user.Id).Find(&subscriptions).Error)
	require.Len(t, subscriptions, 1)
	assert.EqualValues(t, 1000, subscriptions[0].AmountTotal) // 权益额度来自套餐，和付款金额分别保存。

	// 同一个回调入口换用另一笔订单号后，应准确切换到账业务。
	callback.TradeNo = topup.TradeNo // 切换到真实充值订单，只更新钱包。
	for range 2 {
		require.NoError(t, completeOfficialPaymentCallback(ctx, channel, &callback, model.PaymentProviderWechat))
	}
	require.NoError(t, db.First(&paidUser, user.Id).Error)
	assert.Equal(t, 1934, paidUser.Quota) // 冻结额度只加一次，不能按重复通知翻倍。
	// 充值通知不得借相同金额命中已有订阅，实例数量仍应保持一份。
	var count int64
	require.NoError(t, db.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count) // 钱包充值不能新建或延长订阅。
}
