package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 上海 2026-11-01 零点；固定时段让分日断言不依赖运行测试的日期和机器时区。
const financeTestDay = int64(1793462400)

// 三库验证聚合口径：分组、取模分日、条件求和都必须在三种方言上得到相同数字。
// 兑换码字段沿用主表迁移，测试夹具显式创建该表以覆盖旧库升级后的查询契约。
func TestFinanceOverviewDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN") // 未配置外部库时显式跳过，不假报通过。
			if dialect != "sqlite" && dsn == "" {
				t.Skip("test database DSN is not configured")
			}
			db := modelManagementDB(t, dialect, dsn)
			require.NoError(t, db.AutoMigrate(&model.Redemption{}, &model.TopUp{}, &model.SubscriptionOrder{}, &model.AccountRechargeOrder{},
				&model.AccountRechargeEvent{}, &model.CreditAccount{}, &model.CreditEntry{}, &model.InvoiceRequest{}, &model.QuotaData{}, &model.QuotaLedger{}))
			exportEnabled, consumeEnabled := common.DataExportEnabled, common.LogConsumeEnabled
			common.DataExportEnabled, common.LogConsumeEnabled = true, true
			t.Cleanup(func() {
				common.DataExportEnabled, common.LogConsumeEnabled = exportEnabled, consumeEnabled
			})

			start, end := financeTestDay, financeTestDay+3*86400 // 查询 11 月 1 日至 3 日。
			day2, day3 := start+86400, start+2*86400
			now := common.GetTimestamp()

			require.NoError(t, db.Create(&[]model.User{
				{Id: 1, Username: "debtor-a", DisplayName: "Debtor A", AffCode: "fa", Status: common.UserStatusEnabled, Quota: 1000},
				{Id: 2, Username: "debtor-b", AffCode: "fb", Status: common.UserStatusEnabled, Quota: 2500},
			}).Error)

			require.NoError(t, db.Create(&[]model.TopUp{
				{UserId: 1, Money: 100, TradeNo: "t-wechat", PaymentProvider: model.PaymentProviderWechat, Status: common.TopUpStatusSuccess, CompleteTime: start + 10},
				// 上海当日最后一秒仍归当日，下一秒归次日，验证分日边界。
				{UserId: 1, Money: 50.5, TradeNo: "t-epay", PaymentProvider: model.PaymentProviderEpay, Status: common.TopUpStatusSuccess, CompleteTime: day2 - 1},
				{UserId: 1, Money: 9.99, TradeNo: "t-next-day", PaymentProvider: model.PaymentProviderWechat, Status: common.TopUpStatusSuccess, CompleteTime: day2},
				// 以下四笔都不得进入人民币实收：国际网关、未支付、时段之前、时段结束瞬间。
				{UserId: 1, Money: 20, TradeNo: "t-stripe", PaymentProvider: model.PaymentProviderStripe, Status: common.TopUpStatusSuccess, CompleteTime: start + 20},
				{UserId: 1, Money: 999, TradeNo: "t-pending", PaymentProvider: model.PaymentProviderWechat, Status: common.TopUpStatusPending},
				{UserId: 1, Money: 999, TradeNo: "t-before", PaymentProvider: model.PaymentProviderWechat, Status: common.TopUpStatusSuccess, CompleteTime: start - 1},
				{UserId: 1, Money: 999, TradeNo: "t-at-end", PaymentProvider: model.PaymentProviderWechat, Status: common.TopUpStatusSuccess, CompleteTime: end},
			}).Error)
			require.NoError(t, db.Create(&[]model.SubscriptionOrder{
				{UserId: 2, PlanId: 1, Money: 30, TradeNo: "s-wechat", PaymentProvider: model.PaymentProviderWechat, Status: common.TopUpStatusSuccess, CompleteTime: day3 + 5},
				{UserId: 2, PlanId: 1, Money: 30, TradeNo: "s-pending", PaymentProvider: model.PaymentProviderWechat, Status: common.TopUpStatusPending},
			}).Error)

			orders := []model.AccountRechargeOrder{
				{UserID: 1, RequestID: "r1", TradeNo: "r-wechat", PaymentProvider: model.PaymentProviderWechat, PaymentStatus: "paid", MoneyCents: 6000, PaidAt: start + 30, Version: 1},
				// 余额支付重复计收入、挂帐尚未收款，两者都不能算实收。
				{UserID: 1, RequestID: "r2", TradeNo: "r-wallet", PaymentProvider: "wallet", PaymentStatus: "paid", MoneyCents: 5000, PaidAt: start + 40, Version: 1},
				{UserID: 1, RequestID: "r3", TradeNo: "r-credit", PaymentProvider: "credit", PaymentStatus: "paid", MoneyCents: 8000, Version: 1},
				// 时段前收款、时段内退款：不计本期实收，但计本期退款。
				{UserID: 2, RequestID: "r4", TradeNo: "r-refunded", PaymentProvider: model.PaymentProviderWechat, PaymentStatus: "refunded", MoneyCents: 4000, PaidAt: start - 500, Version: 1},
				{UserID: 2, RequestID: "r5", TradeNo: "r-refunding", PaymentProvider: model.PaymentProviderEpay, PaymentStatus: "refunding", MoneyCents: 100, PaidAt: start - 500, Version: 1},
				{UserID: 2, RequestID: "r6", TradeNo: "r-requested", PaymentProvider: model.PaymentProviderEpay, PaymentStatus: "paid", RefundRequested: true, MoneyCents: 100, PaidAt: start - 500, Version: 1},
				// 余额支付订单的退款是退回钱包，不是现金退款。
				{UserID: 2, RequestID: "r7", TradeNo: "r-wallet-refund", PaymentProvider: "wallet", PaymentStatus: "refunded", MoneyCents: 700, PaidAt: start - 500, Version: 1},
			}
			require.NoError(t, db.Create(&orders).Error)
			require.NoError(t, db.Create(&[]model.AccountRechargeEvent{
				{OrderID: orders[3].ID, Version: 2, Action: "refund_approve", CreatedAt: day2 + 5}, // 批准不等于已退款。
				{OrderID: orders[3].ID, Version: 3, Action: "refund", CreatedAt: day2 + 10},
				{OrderID: orders[6].ID, Version: 2, Action: "refund", CreatedAt: day2 + 10},
			}).Error)

			require.NoError(t, db.Create(&[]model.CreditAccount{
				{UserID: 1, Enabled: true, LimitCents: 50000, UsedCents: 8000, Version: 1},
				{UserID: 2, Enabled: false, LimitCents: 20000, UsedCents: 3000, Version: 1}, // 停用账户不计授信总额，但占用仍在。
			}).Error)
			require.NoError(t, db.Create(&[]model.CreditEntry{
				{UserID: 1, Module: model.CreditModuleRecharge, TradeNo: "c-new", AmountCents: 8000, Status: "outstanding", CreatedAt: now - 5*86400},
				{UserID: 2, Module: model.CreditModuleRecharge, TradeNo: "c-45d", AmountCents: 2000, Status: "outstanding", CreatedAt: now - 45*86400},
				{UserID: 2, Module: model.CreditModuleRecharge, TradeNo: "c-90d", AmountCents: 1000, Status: "outstanding", CreatedAt: now - 90*86400},
				// 时段内结清计入实收；时段内新发生的挂帐只计入新增。
				{UserID: 1, Module: model.CreditModuleRecharge, TradeNo: "c-settled", AmountCents: 7000, Status: "settled", CreatedAt: start + 60, SettledAt: day2 + 100},
				// 时段前结清、时段内退款：只计退款。
				{UserID: 2, Module: model.CreditModuleRecharge, TradeNo: "c-refunded", AmountCents: 1500, Status: "refunded", CreatedAt: start - 900, SettledAt: start - 800, ClosedAt: day3 + 50},
				// 未收款就撤销的挂帐既不是收入也不是退款。
				{UserID: 2, Module: model.CreditModuleRecharge, TradeNo: "c-cancelled", AmountCents: 9000, Status: "cancelled", CreatedAt: start - 900, ClosedAt: day3 + 60},
			}).Error)

			invoice := func(trade, status string, fee float64, paidAt int64) model.InvoiceRequest {
				return model.InvoiceRequest{UserId: 1, TradeNo: trade, BusinessType: "topup", ProfileJSON: "{}", BaseAmount: 100, FeeRate: 6, FeeAmount: fee, PayAmount: 100 + fee, Currency: "CNY", Status: status, PaidAt: paidAt}
			}
			require.NoError(t, db.Create(&[]model.InvoiceRequest{
				invoice("i-pending", "pending", 6, start+10),
				invoice("i-processing", "processing", 1.23, day2+10),
				invoice("i-issued", "issued", 5, start-10),      // 时段外付款，已办结。
				invoice("i-unpaid", "pending_payment", 5, 0),    // 未付款不产生附加费收入，也不是待办。
				invoice("i-rejected", "rejected", 2, start-100), // 已办结。
			}).Error)

			require.NoError(t, db.Create(&[]model.QuotaData{
				{UserID: 1, CreatedAt: start + 3600, Quota: 700},
				{UserID: 2, CreatedAt: day3 + 3600, Quota: 300},
				{UserID: 2, CreatedAt: end + 3600, Quota: 5000}, // 时段之后。
			}).Error)

			// 售出码收入按兑换日确认；即使兑换后清理软删除，也不能从财务总览消失。
			redeemedCode := model.Redemption{
				UserId: 1, Key: "finance-redeemed-code", Name: "sold", Quota: 100,
				Status: common.RedemptionCodeStatusUsed, DistributionType: model.RedemptionDistributionSale,
				PriceCents: 1234, CreatedTime: start - 3600, RedeemedTime: start + 100, UsedUserId: 1,
			}
			require.NoError(t, db.Create(&redeemedCode).Error)
			require.NoError(t, db.Delete(&redeemedCode).Error)
			require.NoError(t, db.Create(&[]model.Redemption{
				{UserId: 1, Key: "finance-sale-pending", Name: "sale-pending", Quota: 100, Status: common.RedemptionCodeStatusEnabled, DistributionType: model.RedemptionDistributionSale, PriceCents: 5000},
				{UserId: 1, Key: "finance-gift-pending", Name: "gift-pending", Quota: 100, Status: common.RedemptionCodeStatusEnabled, DistributionType: model.RedemptionDistributionGift},
				{UserId: 1, Key: "finance-unknown-pending", Name: "unknown-pending", Quota: 100, Status: common.RedemptionCodeStatusEnabled},
			}).Error)

			overview, err := model.GetFinanceOverview(start, end)
			require.NoError(t, err)

			assert.EqualValues(t, 16049, overview.TopupCents) // 100 + 50.50 + 9.99，不含 Stripe。
			assert.EqualValues(t, 3000, overview.SubscriptionCents)
			assert.EqualValues(t, 6000, overview.RechargeCents)
			assert.EqualValues(t, 7000, overview.CreditSettledCents)
			assert.EqualValues(t, 1234, overview.RedemptionCents)
			assert.EqualValues(t, 33283, overview.ReceivedCents)
			assert.EqualValues(t, 5500, overview.RefundCents) // 代充值 4000 加已结清挂帐 1500。
			assert.EqualValues(t, 27783, overview.NetCents)
			assert.EqualValues(t, 723, overview.InvoiceFeeCents)

			assert.Equal(t, []model.FinanceDaily{
				{Day: start, TopupCents: 15050, RechargeCents: 6000, RedemptionCents: 1234},
				{Day: day2, TopupCents: 999, CreditSettledCents: 7000},
				{Day: day3, SubscriptionCents: 3000},
			}, overview.Daily)
			assert.Equal(t, []model.FinanceChannel{
				{Channel: model.PaymentProviderWechat, Cents: 19999, Count: 4},
				{Channel: model.FinanceChannelCreditSettlement, Cents: 7000, Count: 1},
				{Channel: model.PaymentProviderEpay, Cents: 5050, Count: 1},
				{Channel: model.FinanceChannelRedemptionSale, Cents: 1234, Count: 1},
			}, overview.Channels)
			assert.Equal(t, model.FinanceRedemption{
				UnredeemedSaleCents: 5000, UnredeemedSaleCount: 1,
				UnredeemedGiftCount: 1, UnredeemedUnknownCount: 1,
			}, overview.Redemption)
			assert.Equal(t, []model.FinanceOtherGateway{{Provider: model.PaymentProviderStripe, Money: 20, Count: 1}}, overview.OtherGateways)

			assert.Equal(t, model.FinanceCredit{
				OutstandingCents: 11000, OutstandingCount: 3,
				LimitCents: 50000, UsedCents: 11000, NewCents: 7000,
				Aging30Cents: 8000, Aging60Cents: 2000, AgingOverCents: 1000, OverdueCount: 2,
				TopDebtors: []model.FinanceDebtor{
					{UserID: 1, Username: "debtor-a", DisplayName: "Debtor A", Cents: 8000, Count: 1},
					{UserID: 2, Username: "debtor-b", Cents: 3000, Count: 2},
				},
			}, overview.Credit)

			assert.EqualValues(t, 3500, overview.WalletBalanceQuota)
			assert.EqualValues(t, 1000, overview.ConsumedQuota)
			assert.True(t, overview.ConsumptionAvailable)
			assert.Equal(t, model.FinanceTodo{PendingInvoices: 2, PendingRefunds: 2}, overview.Todo)

			// 看板统计关闭时明确标记消耗不可用，而不是返回一个看似真实的零。
			common.DataExportEnabled = false
			overview, err = model.GetFinanceOverview(start, end)
			require.NoError(t, err)
			assert.False(t, overview.ConsumptionAvailable)
			assert.Zero(t, overview.ConsumedQuota)
			// 消费日志关闭时也不会收集用量，不能仅凭看板导出开关宣称数据可用。
			common.DataExportEnabled, common.LogConsumeEnabled = true, false
			overview, err = model.GetFinanceOverview(start, end)
			require.NoError(t, err)
			assert.False(t, overview.ConsumptionAvailable)
			assert.Zero(t, overview.ConsumedQuota)
			common.LogConsumeEnabled = true

			// 没有任何收入的时段仍返回完整的逐日序列和空数组，前端不必处理 null。
			empty, err := model.GetFinanceOverview(start-10*86400, start-8*86400)
			require.NoError(t, err)
			assert.Zero(t, empty.ReceivedCents)
			assert.Len(t, empty.Daily, 2)
			assert.NotNil(t, empty.Channels)
			assert.NotNil(t, empty.OtherGateways)
			assert.EqualValues(t, 11000, empty.Credit.OutstandingCents) // 欠款是当前快照，不随时段变化。

			// 走实际订阅到账事务：它还会创建同交易号的 TopUp 账单镜像，不能再算一份收入。
			require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}, &model.UserSubscription{}, &model.Log{}))
			plan := model.SubscriptionPlan{Title: "Finance subscription", PriceAmount: 12.34, DurationUnit: "month", DurationValue: 1, TotalAmount: 1000, Enabled: true}
			require.NoError(t, db.Create(&plan).Error)
			model.InvalidateSubscriptionPlanCache(plan.Id)
			t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(plan.Id) })
			order := model.SubscriptionOrder{UserId: 1, PlanId: plan.Id, Money: 12.34, TradeNo: "s-real-payment", PaymentMethod: "wechat-finance", PaymentProvider: model.PaymentProviderWechat, Status: common.TopUpStatusPending}
			require.NoError(t, order.Insert())
			require.NoError(t, model.CompleteOfficialSubscriptionOrder(order.TradeNo, "", model.PaymentProviderWechat, order.PaymentMethod, decimal.RequireFromString("12.34")))
			actual, err := model.GetFinanceOverview(now-60, common.GetTimestamp()+60)
			require.NoError(t, err)
			assert.EqualValues(t, 1234, actual.SubscriptionCents)
			assert.EqualValues(t, 1234, actual.ReceivedCents)
			assert.Zero(t, actual.TopupCents)
			assert.Empty(t, actual.OtherGateways) // 镜像不应被误列为 provider 为空的其他网关。

			// 人工现金与额度采用明显不同数值，防止额度面值被当成人民币。
			cashInput := model.ManualFinanceInput{Reason: "offline_cash", CashCents: 3400, CashRef: "offline-received", Note: "verified receipt", RequestID: "00000000-0000-4000-8000-000000000001"}
			_, err = model.AdjustUserQuota(1, common.RoleRootUser, "add", 500000, 100, cashInput)
			require.NoError(t, err)
			_, err = model.AdjustUserQuota(1, common.RoleRootUser, "add", 500000, 100, cashInput)
			require.NoError(t, err) // 原请求重复不能多算一次线下实收。
			_, err = model.AdminBindSubscriptionWithFinance(1, plan.Id, 100, common.RoleRootUser, model.ManualFinanceInput{Reason: "offline_cash", CashCents: 600, CashRef: "subscription-received", Note: "manual subscription", RequestID: "00000000-0000-4000-8000-000000000002"})
			require.NoError(t, err)
			require.NoError(t, db.Model(&model.CreditAccount{}).Where("user_id = ?", 1).Update("topup_enabled", true).Error)
			adjustment, err := model.AdjustUserQuota(1, common.RoleRootUser, "add", 200000, 100, model.ManualFinanceInput{Reason: "on_account", ReceivableCents: 1700, Note: "deferred collection", RequestID: "00000000-0000-4000-8000-000000000003"})
			require.NoError(t, err)
			var ledger model.QuotaLedger
			require.NoError(t, db.First(&ledger, adjustment.LedgerID).Error)
			require.NoError(t, model.WriteOffCreditEntry(ledger.CreditEntryID, 100, "uncollectible"))
			actual, err = model.GetFinanceOverview(now-60, common.GetTimestamp()+60)
			require.NoError(t, err)
			assert.EqualValues(t, 3400, actual.TopupCents)
			assert.EqualValues(t, 1834, actual.SubscriptionCents)
			assert.EqualValues(t, 5234, actual.ReceivedCents)
			assert.EqualValues(t, 4000, actual.OfflineCashCents)
			assert.EqualValues(t, 1700, actual.WriteOffCents)
			assert.EqualValues(t, 1, actual.WriteOffCount)
			assert.EqualValues(t, 5234, actual.NetCents) // 坏账核销不是现金退款。
			assert.Len(t, actual.ManualAdjustments, 3)
			var dailyTotal int64
			for _, day := range actual.Daily {
				dailyTotal += day.TopupCents + day.SubscriptionCents + day.RechargeCents + day.CreditSettledCents
			}
			assert.Equal(t, actual.ReceivedCents, dailyTotal)
		})
	}
}

// 通过真实登录会话、角色门槛和权限中间件验证总览只对被授予的账号开放，
// 并覆盖 Root 在用户编辑里给财务角色授予、保留和收回总览权限的完整流程。
func TestFinanceOverviewPermissions(t *testing.T) {
	root, rootIdentity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.Model(root).Update("role", common.RoleRootUser).Error)
	require.NoError(t, model.PublishUserAuthCache(root.Id))
	require.NoError(t, model.DB.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}, &model.Redemption{}, &model.TopUp{}, &model.SubscriptionOrder{},
		&model.AccountRechargeOrder{}, &model.AccountRechargeEvent{}, &model.CreditAccount{}, &model.CreditEntry{}, &model.InvoiceRequest{}, &model.QuotaData{}, &model.QuotaLedger{}))
	oldMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = oldMaster })
	require.NoError(t, authz.Init(model.DB))

	engine := gin.New()
	engine.GET("/api/finance/overview", middleware.FinanceAuth(), RechargeSessionOnly, middleware.RequirePermission(authz.FinanceOverviewRead), GetFinanceOverview)
	engine.GET("/api/finance/adjustments", middleware.FinanceAuth(), RechargeSessionOnly, middleware.RequirePermission(authz.FinanceOverviewRead), GetFinanceAdjustments)
	// 周期接口沿用同一会话和只读授权，写操作另经 Root 门槛。
	require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionPlan{}, &model.UserSubscription{}, &model.FinancePoolSubscription{}))
	engine.GET("/api/finance/subscriptions", middleware.FinanceAuth(), RechargeSessionOnly, middleware.RequirePermission(authz.FinanceOverviewRead), GetFinanceSubscriptions)
	engine.POST("/api/finance/pool-subscriptions", middleware.FinanceAuth(), RechargeSessionOnly, middleware.RootAuth(), SaveFinancePoolSubscription)
	engine.PUT("/api/finance/pool-subscriptions/:id", middleware.FinanceAuth(), RechargeSessionOnly, middleware.RootAuth(), SaveFinancePoolSubscription)
	engine.DELETE("/api/finance/pool-subscriptions/:id", middleware.FinanceAuth(), RechargeSessionOnly, middleware.RootAuth(), DeleteFinancePoolSubscription)
	path := fmt.Sprintf("/api/finance/overview?start=%d&end=%d", financeTestDay, financeTestDay+86400)
	get := func(token, target string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		return response
	}
	login := func(username string, role int) (*model.User, string) {
		user := &model.User{Username: username, AffCode: username, Role: role, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1}
		require.NoError(t, model.DB.Create(user).Error)
		bundle, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "finance-overview-test")
		require.NoError(t, err)
		return user, bundle.AccessToken
	}
	rootToken, _, err := service.IssueAccessToken(rootIdentity)
	require.NoError(t, err)
	finance, financeToken := login("overview-finance", common.RoleFinanceUser)
	admin, adminToken := login("overview-admin", common.RoleAdminUser)
	_, commonToken := login("overview-common", common.RoleCommonUser)

	// Root 始终可见；管理员和财务默认不可见，普通用户连角色门槛都过不了。
	response := get(rootToken, path)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), `"received_cents":0`)
	assert.Equal(t, http.StatusForbidden, get(financeToken, path).Code)
	assert.Equal(t, http.StatusForbidden, get(adminToken, path).Code)
	assert.NotEqual(t, http.StatusOK, get(commonToken, path).Code)

	// 时间范围缺失、倒置或超过上限都拒绝，不返回一个默认时段的数字。
	for _, query := range []string{
		"", "?start=abc&end=1", "?start=9223372036854775707&end=9223372036854775807",
		fmt.Sprintf("?start=%d&end=%d", financeTestDay, financeTestDay),
		fmt.Sprintf("?start=%d&end=%d", financeTestDay, financeTestDay+(model.FinanceOverviewMaxDays+1)*86400),
	} {
		assert.Contains(t, get(rootToken, "/api/finance/overview"+query).Body.String(), `"success":false`, query)
	}

	// Root 经单次验证修改用户权限；每次提交消耗一份证明。
	update := func(target *model.User, permissions string) {
		operation := service.VerificationOperation{Scope: service.VerificationScopeAdminUserUpdate, Context: []byte(fmt.Sprintf(`{"user_id":%d}`, target.Id))}
		proof := issueSecurityEnrollmentProof(t, rootIdentity, operation, service.VerificationMethodPassword)
		body := fmt.Sprintf(`{"id":%d,"username":%q,"display_name":"Overview","group":"default","admin_permissions":%s}`, target.Id, target.Username, permissions)
		result := adminUserRequest(http.MethodPut, "/api/user/", body, proof, rootIdentity, common.RoleRootUser, nil, UpdateUser)
		require.Contains(t, result.Body.String(), `"success":true`, result.Body.String())
	}

	// 给财务角色提交总览权限时夹带渠道权限：总览生效，渠道权限被丢弃。
	update(finance, `{"finance_overview":{"read":true},"channel":{"read":true,"sensitive_write":true}}`)
	assert.Equal(t, http.StatusOK, get(financeToken, path).Code)
	assert.False(t, authz.Can(finance.Id, common.RoleFinanceUser, authz.ChannelRead))
	assert.Equal(t, authz.PermissionsMap{authz.ResourceFinanceOverview: {authz.ActionRead: true}}, authz.ExplicitUserOverrides(finance.Id))
	// 普通资料编辑不携带权限矩阵，已授予的总览权限必须保留。
	plain := adminUserRequest(http.MethodPut, "/api/user/", fmt.Sprintf(`{"id":%d,"username":%q,"display_name":"Renamed","group":"default"}`, finance.Id, finance.Username), "", rootIdentity, common.RoleRootUser, nil, UpdateUser)
	require.Contains(t, plain.Body.String(), `"success":true`, plain.Body.String())
	assert.Equal(t, http.StatusOK, get(financeToken, path).Code)

	update(admin, `{"finance_overview":{"read":true}}`)
	assert.Equal(t, http.StatusOK, get(adminToken, path).Code)

	// 即使持有总览权限，个人访问令牌也不能读取全平台经营数据。
	raw := "pat_finance_overview_test_only_not_a_real_credential"
	pat := &model.UserAccessToken{Name: "overview-fixture", TokenHash: model.AccessTokenFingerprint(raw), TokenHint: model.AccessTokenHint(raw)}
	require.NoError(t, pat.SetScopes([]string{"finance_overview:read"}))
	require.NoError(t, model.CreateUserAccessToken(finance.Id, pat, service.AccessTokenMaxPerUser))
	assert.NotEqual(t, http.StatusOK, get(raw, path).Code)
	// 人工记录包含收款凭证，不因既有开票角色或 PAT 范围而顺带开放。
	adjustmentsPath := strings.Replace(path, "/overview?", "/adjustments?", 1)
	assert.Contains(t, get(financeToken, adjustmentsPath).Body.String(), `"items":[]`)
	for _, token := range []string{raw, commonToken, ""} {
		assert.NotEqual(t, http.StatusOK, get(token, adjustmentsPath).Code)
	}
	assert.Contains(t, get(rootToken, adjustmentsPath+"&user_id=invalid").Body.String(), `"success":false`)

	// 已获授权财务和管理员可以看周期，个人令牌与普通用户不能看。
	subPath := strings.Replace(path, "/overview?", "/subscriptions?", 1)
	for _, token := range []string{rootToken, financeToken, adminToken} {
		assert.Contains(t, get(token, subPath).Body.String(), `"items":[]`)
	}
	for _, token := range []string{raw, commonToken, ""} {
		assert.NotEqual(t, http.StatusOK, get(token, subPath).Code)
	}
	// 错误分页、类别和时间参数不被默默改为默认查询。
	for _, suffix := range []string{"&page=0", "&page=no", "&category=hosting", "&end=0"} {
		target := subPath + suffix
		if suffix == "&end=0" {
			target = "/api/finance/subscriptions?start=1&end=0"
		}
		assert.Contains(t, get(rootToken, target).Body.String(), `"success":false`)
	}
	writePeriod := func(token, method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, req)
		return recorder
	}
	body := fmt.Sprintf(`{"name":"HTTP pool","start_time":%d,"end_time":%d}`, financeTestDay, financeTestDay+86400)
	// 只读授权不包含写入，三个动作均验证，防止仅隐藏前端按钮。
	for _, token := range []string{financeToken, adminToken, commonToken, raw} {
		assert.NotEqual(t, http.StatusOK, writePeriod(token, "POST", "/api/finance/pool-subscriptions", body).Code)
		assert.NotEqual(t, http.StatusOK, writePeriod(token, "PUT", "/api/finance/pool-subscriptions/1", body).Code)
		assert.NotEqual(t, http.StatusOK, writePeriod(token, "DELETE", "/api/finance/pool-subscriptions/1", "").Code)
	}
	assert.Contains(t, writePeriod(rootToken, "POST", "/api/finance/pool-subscriptions", body).Body.String(), `"success":true`)
	assert.Contains(t, get(rootToken, subPath).Body.String(), `"name":"HTTP pool"`)
	assert.Contains(t, writePeriod(rootToken, "PUT", "/api/finance/pool-subscriptions/1", strings.Replace(body, "HTTP pool", "Edited", 1)).Body.String(), `"success":true`)
	assert.Contains(t, get(rootToken, subPath).Body.String(), `"name":"Edited"`)
	assert.Contains(t, writePeriod(rootToken, "POST", "/api/finance/pool-subscriptions", `{"name":"","start_time":1,"end_time":2}`).Body.String(), `"success":false`)
	assert.Contains(t, writePeriod(rootToken, "DELETE", "/api/finance/pool-subscriptions/1", "").Body.String(), `"success":true`)
	assert.Contains(t, get(rootToken, subPath).Body.String(), `"items":[]`)
	// Root 单独收回后立即失效，不需要等会话过期。
	update(finance, `{"finance_overview":{"read":false}}`)
	assert.Equal(t, http.StatusForbidden, get(financeToken, path).Code)
	assert.Empty(t, authz.ExplicitUserOverrides(finance.Id))

	// 总览授权不能恢复已经撤销的开票/挂帐办理能力，两种权限互不包含。
	require.NoError(t, authz.SetUserPermissions(finance.Id, authz.PermissionsMap{
		authz.ResourceFinance: {authz.ActionRead: false, authz.ActionWrite: false},
	}))
	update(finance, `{"finance_overview":{"read":true}}`)
	assert.True(t, authz.Can(finance.Id, common.RoleFinanceUser, authz.FinanceOverviewRead))
	assert.False(t, authz.Can(finance.Id, common.RoleFinanceUser, authz.FinanceRead))
	assert.False(t, authz.Can(finance.Id, common.RoleFinanceUser, authz.FinanceWrite))
	update(finance, `{"finance_overview":{"read":false}}`)
	assert.False(t, authz.Can(finance.Id, common.RoleFinanceUser, authz.FinanceRead))
	assert.False(t, authz.Can(finance.Id, common.RoleFinanceUser, authz.FinanceWrite))
}

// 周期功能在同一文件集中验证三库、迁移及真实接口权限，避免重复测试夹具。
// 旧版本已经具有用户/套餐/订阅表；升级只新增人工号池表。
// 验证相交边界与分页的同时保留原订阅编号和取消状态。
// 用例在真实方言连接上执行，不用 SQLite 模拟 MySQL 的零受影响行语义。
// 各子用例通过既有 helper 创建并清理自己的数据库，不触及开发数据。
// 外部库未配置时明确跳过；交付验证必须同时提供两份回环 DSN。
// 先建立旧版订阅表，再增加人工周期表，代表本次新增表的升级路径。
// 自动迁移重复执行两次，确保新表不会在重启时损失记录。
// 开始日期索引在重复迁移后仍须存在，不能只验证表名存在。
// 原用户订阅总数不变，证明人工表迁移没有重建用户权益数据。
// 时间常量采用固定上海零点，数据库服务器本地时区不参与计算。
// 结束于窗口左端的合同不相交，不能以闭区间查询重复展示边界项。
// 开始于窗口右端的合同不相交，下一窗口才应出现。
// 覆盖整个查询窗口的订阅必须保留完整原日期。
// 同一个用户拥有两份相交订阅，合并不能按用户编号去重。
// 已取消订阅仍有历史展示价值，不使用 active 状态过滤。
// 用户名与套餐名连接到正确来源，号池主键不会被当作用户编号。
// 号池名字首尾空白在入口统一清理，读取时不再额外规范化。
// 保存后直接重查，避免只断言传入对象中的值就宣称持久化成功。
// 相同字段重复保存不能被 MySQL 的零变更行数误判为不存在。
// 空白名称与超长中文名称都必须在服务端拒绝。
// 反转起止时间不能生成负长度条形，也不能被自动调换为合法值。
// 更新不存在的编号不能隐式创建新记录，否则编辑会产生重复合同。
// 超过二十条才可验证真实分页路径，不能只验证固定第一页。
// 全部类别计数包含两个来源，第二页长度必须与总数一致。
// 稳定排序包含来源与主键，使相同开始时间的记录顺序可预测。
// 删除人工记录后再查用户来源，证明没有复用用户订阅删除函数。
// 重复删除返回错误，防止过期界面显示不存在的成功结果。
// 删除套餐定义后仍保留购买记录，左连接不能被改成内连接。
// 名称缺失只是历史关联缺失，不能把订阅整条记录藏掉。
// 测试不构造订单付款，不涉及余额、收入或真实商户回调。
// 本次新增表没有金额字段，不能被其他账务路径误作支出凭证。
// 具体权限另在上方真实登录会话用例验证，避免复制登录夹具。
func TestFinanceSubscriptionsDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dialect != "sqlite" && dsn == "" {
				t.Skip("test database DSN is not configured")
			}
			db := modelManagementDB(t, dialect, dsn)
			// 先建旧版核心表并留下数据，再执行新增表迁移，模拟升级。
			require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}, &model.UserSubscription{}))
			user := model.User{Username: "gantt-user", AffCode: "gantt"}
			require.NoError(t, db.Create(&user).Error)
			plan := model.SubscriptionPlan{Title: "Monthly plan"}
			require.NoError(t, db.Create(&plan).Error)
			start, end := financeTestDay, financeTestDay+30*86400
			// 区间恰好结束于查询开始、开始于查询结束的两条均不可出现。
			subscriptions := []model.UserSubscription{
				{UserId: user.Id, PlanId: plan.Id, StartTime: start - 86400, EndTime: start, Status: "expired"},
				{UserId: user.Id, PlanId: plan.Id, StartTime: start - 86400, EndTime: end + 86400, Status: "active"},
				{UserId: user.Id, PlanId: plan.Id, StartTime: end, EndTime: end + 86400, Status: "active"},
				{UserId: user.Id, PlanId: plan.Id, StartTime: start + 86400, EndTime: end, Status: "cancelled"},
			}
			require.NoError(t, db.Create(&subscriptions).Error)
			// 新建空表及连续迁移都需要成功；用户订阅不能被迁移覆盖。
			require.NoError(t, db.AutoMigrate(&model.FinancePoolSubscription{}))
			pool, err := model.SaveFinancePoolSubscription(0, "  Codex pool  ", start, end)
			require.NoError(t, err)
			for range 2 {
				require.NoError(t, db.AutoMigrate(&model.FinancePoolSubscription{}))
			}
			require.True(t, db.Migrator().HasIndex(&model.FinancePoolSubscription{}, "StartTime"))
			var stored model.FinancePoolSubscription
			require.NoError(t, db.First(&stored, pool.ID).Error)
			assert.Equal(t, "Codex pool", stored.Name)
			assert.Equal(t, end, stored.EndTime)
			var existing int64
			require.NoError(t, db.Model(&model.UserSubscription{}).Count(&existing).Error)
			assert.EqualValues(t, 4, existing)
			// 两类合并保留真实归属及状态；只按时间相交筛选，不只取当前 active。
			result, err := model.GetFinanceSubscriptions(start, end, "all", 1)
			require.NoError(t, err)
			require.EqualValues(t, 3, result.Total)
			require.Len(t, result.Items, 3)
			assert.Equal(t, "user", result.Items[0].Category)
			assert.Equal(t, "gantt-user", result.Items[0].Username)
			assert.Equal(t, "Monthly plan", result.Items[0].Name)
			assert.Equal(t, "cancelled", result.Items[2].Status)
			result, err = model.GetFinanceSubscriptions(start, end, "pool", 1)
			require.NoError(t, err)
			require.Len(t, result.Items, 1)
			assert.Equal(t, pool.ID, result.Items[0].ID)
			// 修改后重新读取必须回显；重复保存相同值也应成功。
			for range 2 {
				_, err = model.SaveFinancePoolSubscription(pool.ID, "Renewed pool", start+86400, end+86400)
				require.NoError(t, err)
			}
			result, err = model.GetFinanceSubscriptions(start, end, "pool", 1)
			require.NoError(t, err)
			assert.Equal(t, "Renewed pool", result.Items[0].Name)
			assert.Equal(t, start+86400, result.Items[0].StartTime)
			// 所有写入校验都在后端，绕过表单仍不能存空名称或倒置时间。
			for _, name := range []string{" ", strings.Repeat("中", 129)} {
				_, err = model.SaveFinancePoolSubscription(0, name, start, end)
				require.Error(t, err)
			}
			_, err = model.SaveFinancePoolSubscription(0, "invalid", end, start)
			require.Error(t, err)
			_, err = model.SaveFinancePoolSubscription(999999, "missing", start, end)
			require.Error(t, err)
			// 超过一页使用稳定主键排序，合并来源不能导致跨页重复或漏项。
			for range 21 {
				_, err = model.SaveFinancePoolSubscription(0, "another pool", start, end)
				require.NoError(t, err)
			}
			first, err := model.GetFinanceSubscriptions(start, end, "all", 1)
			require.NoError(t, err)
			second, err := model.GetFinanceSubscriptions(start, end, "all", 2)
			require.NoError(t, err)
			assert.Len(t, first.Items, 20)
			assert.Len(t, second.Items, 4)
			assert.EqualValues(t, 24, second.Total)
			// 删除记录后同号用户订阅仍存在，类别不是可由客户端改变的写参数。
			require.NoError(t, model.DeleteFinancePoolSubscription(pool.ID))
			require.Error(t, model.DeleteFinancePoolSubscription(pool.ID))
			result, err = model.GetFinanceSubscriptions(start, end, "user", 1)
			require.NoError(t, err)
			assert.EqualValues(t, 2, result.Total)
			// 关联记录丢失时左连接保留周期与编号，不用空列表隐藏历史。
			require.NoError(t, db.Delete(&plan).Error)
			result, err = model.GetFinanceSubscriptions(start, end, "user", 1)
			require.NoError(t, err)
			assert.EqualValues(t, 2, result.Total)
			assert.Empty(t, result.Items[0].Name)
		})
	}
}
