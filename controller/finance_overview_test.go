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
// 本功能不新增表或字段，因此只验证只读查询，不涉及迁移升级。
func TestFinanceOverviewDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN") // 未配置外部库时显式跳过，不假报通过。
			if dialect != "sqlite" && dsn == "" {
				t.Skip("test database DSN is not configured")
			}
			db := modelManagementDB(t, dialect, dsn)
			require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.SubscriptionOrder{}, &model.AccountRechargeOrder{},
				&model.AccountRechargeEvent{}, &model.CreditAccount{}, &model.CreditEntry{}, &model.InvoiceRequest{}, &model.QuotaData{}))
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

			overview, err := model.GetFinanceOverview(start, end)
			require.NoError(t, err)

			assert.EqualValues(t, 16049, overview.TopupCents) // 100 + 50.50 + 9.99，不含 Stripe。
			assert.EqualValues(t, 3000, overview.SubscriptionCents)
			assert.EqualValues(t, 6000, overview.RechargeCents)
			assert.EqualValues(t, 7000, overview.CreditSettledCents)
			assert.EqualValues(t, 32049, overview.ReceivedCents)
			assert.EqualValues(t, 5500, overview.RefundCents) // 代充值 4000 加已结清挂帐 1500。
			assert.EqualValues(t, 26549, overview.NetCents)
			assert.EqualValues(t, 723, overview.InvoiceFeeCents)

			assert.Equal(t, []model.FinanceDaily{
				{Day: start, TopupCents: 15050, RechargeCents: 6000},
				{Day: day2, TopupCents: 999, CreditSettledCents: 7000},
				{Day: day3, SubscriptionCents: 3000},
			}, overview.Daily)
			assert.Equal(t, []model.FinanceChannel{
				{Channel: model.PaymentProviderWechat, Cents: 19999, Count: 4},
				{Channel: model.FinanceChannelCreditSettlement, Cents: 7000, Count: 1},
				{Channel: model.PaymentProviderEpay, Cents: 5050, Count: 1},
			}, overview.Channels)
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
		})
	}
}

// 通过真实登录会话、角色门槛和权限中间件验证总览只对被授予的账号开放，
// 并覆盖 Root 在用户编辑里给财务角色授予、保留和收回总览权限的完整流程。
func TestFinanceOverviewPermissions(t *testing.T) {
	root, rootIdentity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.Model(root).Update("role", common.RoleRootUser).Error)
	require.NoError(t, model.PublishUserAuthCache(root.Id))
	require.NoError(t, model.DB.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}, &model.TopUp{}, &model.SubscriptionOrder{},
		&model.AccountRechargeOrder{}, &model.AccountRechargeEvent{}, &model.CreditAccount{}, &model.CreditEntry{}, &model.InvoiceRequest{}, &model.QuotaData{}))
	oldMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = oldMaster })
	require.NoError(t, authz.Init(model.DB))

	engine := gin.New()
	engine.GET("/api/finance/overview", middleware.FinanceAuth(), RechargeSessionOnly, middleware.RequirePermission(authz.FinanceOverviewRead), GetFinanceOverview)
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
