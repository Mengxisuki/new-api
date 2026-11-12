package controller

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 三种真实数据库使用同一交易场景，未配置的方言必须显式跳过。
// 新建与旧表升级分别执行，防止历史空摘要误触唯一约束。
// 迁移前后的历史微信金额、渠道和冻结额度都必须保持。
func TestCreditCheckoutDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		for _, upgrade := range []bool{false, true} { // 空库安装与旧结构升级必须各自完成交易。
			t.Run(fmt.Sprintf("%s/upgrade=%v", dialect, upgrade), func(t *testing.T) {
				dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
				if dialect != "sqlite" && dsn == "" {
					t.Skip("isolated database DSN is not configured") // 未提供真实引擎不能当作该方言已通过。
				}
				db := modelManagementDB(t, dialect, dsn) // 夹具创建隔离数据库，不复用开发业务表。
				tables := []any{&model.TopUp{}, &model.SubscriptionOrder{}, &model.SubscriptionPlan{}, &model.UserSubscription{}, &model.CreditAccount{}, &model.CreditEntry{}, &model.Log{}}
				if upgrade {
					// 从当前订单结构移除本期新增列，保留原有索引与历史列契约。
					require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.SubscriptionOrder{}))
					for _, col := range []string{"RequestHash", "ManualCompleted", "ManualActor", "ManualReceiptRef", "ManualNote"} {
						require.NoError(t, db.Migrator().DropColumn(&model.TopUp{}, col))
					}
					for _, col := range []string{"RequestHash", "PlanSnapshot"} {
						require.NoError(t, db.Migrator().DropColumn(&model.SubscriptionOrder{}, col))
					}
					// 多条历史无请求摘要数据不能在升级时互相冲突。
					for _, trade := range []string{"legacy-one", "legacy-two"} {
						require.NoError(t, db.Table("top_ups").Create(map[string]any{"user_id": 9, "amount": 1, "money": 1.25, "credited_quota": 123, "trade_no": trade, "payment_provider": model.PaymentProviderWechat, "status": common.TopUpStatusSuccess}).Error)
						require.NoError(t, db.Table("subscription_orders").Create(map[string]any{"user_id": 9, "plan_id": 9, "money": 2.50, "trade_no": trade, "payment_provider": model.PaymentProviderWechat, "status": common.TopUpStatusSuccess}).Error)
					}
				}
				// 连续两次启动式迁移不能改写旧金额，也不能重复添加唯一约束。
				for range 2 {
					require.NoError(t, db.AutoMigrate(tables...))
				}
				if upgrade {
					var old model.TopUp
					require.NoError(t, db.Where("trade_no = ?", "legacy-one").First(&old).Error)
					assert.Equal(t, 1.25, old.Money)                                  // 升级不能按现行汇率重算历史金额。
					assert.Equal(t, 123, old.CreditedQuota)                           // 原冻结额度不因新增授信字段发生变化。
					assert.Equal(t, model.PaymentProviderWechat, old.PaymentProvider) // 历史微信交易不能被改为挂帐。
				}
				configureCreditCheckoutTest(t, db)
				verifyCreditCheckoutTransactions(t, db)
			})
		}
	}
}

// 配置只作用于隔离测试，结束时恢复原内存值。
// 微信商户不参与挂帐报价，故测试不准备任何商户凭据。
// Hub 捕获开启以验证事件与钱包确实共同提交，而非仅检查函数返回。
func configureCreditCheckoutTest(t *testing.T, db *gorm.DB) {
	t.Helper()
	payment := operation_setting.GetPaymentSetting()
	oldPayment, oldDisplay := *payment, operation_setting.GetQuotaDisplayType() // 金额优惠和展示币种都属于可变测试状态。
	oldRate, oldMin, oldQuota := operation_setting.USDExchangeRate, operation_setting.MinTopUp, common.QuotaPerUnit
	oldCapture, oldGroup := model.HubCaptureEnabled(), common.TopupGroupRatio2JSONString()
	t.Cleanup(func() {
		*payment = oldPayment
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplay
		operation_setting.USDExchangeRate, operation_setting.MinTopUp, common.QuotaPerUnit = oldRate, oldMin, oldQuota
		model.SetHubCaptureEnabled(oldCapture) // 失败清理也必须还原捕获开关。
		assert.NoError(t, common.UpdateTopupGroupRatioByJSONString(oldGroup))
	})
	payment.ComplianceConfirmed, payment.ComplianceTermsVersion = true, operation_setting.CurrentComplianceTermsVersion
	payment.AmountDiscount = map[int]float64{}
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeCNY
	operation_setting.USDExchangeRate, operation_setting.MinTopUp, common.QuotaPerUnit = 10, 1, 500000
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, model.MigrateEventHub(db))
	model.SetHubCaptureEnabled(true) // 确保本次走真实事务事件写入而不是关闭后的空路径。
	// 真实用户状态和授信额度是测试前提，不能依赖其他测试留下的数据。
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "checkout-buyer", Group: "default", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, model.SaveCreditAccount(model.CreditAccount{UserID: 1, Enabled: true, LimitCents: 10000, TopupEnabled: true, SubscriptionEnabled: true}, 100))
}

// 直接经过 Gin 绑定和公共 handler，仅将网络边界替换为 recorder。
// 返回结构与前端合同一致，所有断言都基于实际数据库和响应。
// 身份来自中间件上下文，不把用户编号作为请求参数。
func creditCheckoutHTTP(t *testing.T, handler gin.HandlerFunc, method, path, body string) map[string]any {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 1) // 模拟已认证用户上下文，不从请求读取身份。
	c.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	handler(c) // 真实业务处理器负责解析、定价和落账。
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String()) // 错误响应也必须保持合法JSON，不能靠panic退出。
	return response
}

// 核心场景同时覆盖建单幂等、付款幂等、Hub零实收和关闭授权后的恢复。
// 后续独立失败单只占订单，不应产生额外债务或钱包余额。
func verifyCreditCheckoutTransactions(t *testing.T, db *gorm.DB) {
	t.Helper()
	quote := creditCheckoutHTTP(t, RequestAmount, http.MethodGet, "/api/user/amount?amount=1&payment_method=credit", "")
	assert.Equal(t, "1.00", quote["data"])            // 原报价字符串合同保持不变。
	assert.EqualValues(t, 100, quote["quoted_cents"]) // 下一步确认使用精确分值。
	// 报价不允许含税开票；开票失败也不能创建订单。
	badInvoice := creditCheckoutHTTP(t, RequestAmount, http.MethodPost, "/api/user/amount", `{"amount":1,"payment_method":"credit","invoice_required":true}`)
	assert.Equal(t, "error", badInvoice["message"]) // 挂帐报价阶段就拒绝开票。
	body := `{"amount":1,"request_id":"fb957380-baf1-4a78-9b48-0d27bc377871","quoted_cents":100}`
	result := creditCheckoutHTTP(t, RequestCreditTopUp, http.MethodPost, "/api/user/credit/pay", body)
	require.Equal(t, true, result["success"], "%v", result) // 接口成功必须对应完整付款事务。
	data := result["data"].(map[string]any)
	trade := data["trade_no"].(string)
	assert.Equal(t, "success", data["status"])      // 业务状态与公共ApiSuccess信封分开验证。
	assert.Equal(t, "credit", data["payment_kind"]) // 前端据此区分无现金付款方式。
	assert.LessOrEqual(t, len(trade), 32)           // 用户ID增长不会突破订单号长度合同。
	// 授信和合规都关闭，旧成功请求仍可恢复；新请求不能获得权益。
	require.NoError(t, db.Model(&model.CreditAccount{}).Where("user_id = ?", 1).Update("enabled", false).Error)
	operation_setting.GetPaymentSetting().ComplianceConfirmed = false
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{1: 0.5}
	retry := creditCheckoutHTTP(t, RequestCreditTopUp, http.MethodPost, "/api/user/credit/pay", body)
	assert.Equal(t, result, retry) // 关闭后重试应恢复同一交易号和结果。
	changed := creditCheckoutHTTP(t, RequestCreditTopUp, http.MethodPost, "/api/user/credit/pay", strings.Replace(body, `"amount":1`, `"amount":2`, 1))
	assert.Equal(t, false, changed["success"]) // 同键异参不能借成功原单绕过新意图校验。
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, 50000, user.Quota) // 只发放一次人民币面额对应的内部额度。
	var events []model.HubEvent
	require.NoError(t, db.Where("user_id = ?", 1).Find(&events).Error)
	require.Len(t, events, 1) // 重复HTTP付款不能生成第二条到账事件。
	var payload map[string]any
	require.NoError(t, common.UnmarshalJsonStr(events[0].Payload, &payload))
	assert.Equal(t, "0.00", payload["paid_amount"])        // 挂帐履约不得伪装成现金到账。
	assert.Equal(t, "1.00", payload["receivable_amount"])  // 应收和实收采用独立字段。
	assert.Equal(t, "credit", payload["payment_provider"]) // 事件沿用真实渠道而非业务模块名称。
	var entries int64
	require.NoError(t, db.Model(&model.CreditEntry{}).Count(&entries).Error)
	assert.EqualValues(t, 1, entries) // 幂等付款不能追加第二笔债务。
	var logs int64
	require.NoError(t, db.Model(&model.Log{}).Where("user_id = ?", 1).Count(&logs).Error)
	assert.EqualValues(t, 1, logs) // 重试不能重复记录挂帐充值成功日志。
	info := creditCheckoutHTTP(t, GetTopUpInfo, http.MethodGet, "/api/user/topup/info", "")
	require.Equal(t, true, info["success"])
	infoData := info["data"].(map[string]any)
	assert.EqualValues(t, 1, infoData["credit"].(map[string]any)["user_id"]) // 本人摘要不得混入其他用户账户。
	for _, method := range infoData["pay_methods"].([]any) {
		assert.NotEqual(t, "credit", method.(map[string]any)["type"]) // 不把授信路由到易支付。
	}
	// 原请求不因状态关闭而重算，但下一笔必须重新接受服务端定价。
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	require.NoError(t, db.Model(&model.CreditAccount{}).Where("user_id = ?", 1).Update("enabled", true).Error)
	stale := creditCheckoutHTTP(t, RequestCreditTopUp, http.MethodPost, "/api/user/credit/pay", `{"amount":1,"request_id":"fb957380-baf1-4a78-9b48-0d27bc377872","quoted_cents":99}`)
	assert.Equal(t, false, stale["success"]) // 新报价低一分也必须重新确认。
	// 重复插入实际触发数据库唯一约束；PostgreSQL必须在事务退出后恢复。
	original := model.GetTopUpByTradeNo(trade)
	require.NotNil(t, original)
	duplicate := *original
	duplicate.Id = 0                                                                                                    // 模拟另一实例使用同交易号插入，而非更新原记录。
	require.NoError(t, model.InsertCreditCheckout(&duplicate, 1, model.CreditModuleTopup, trade, original.RequestHash)) // 唯一键失败后恢复同请求，兼容PostgreSQL事务状态。
	duplicate.Id, duplicate.RequestHash = 0, "different"
	require.ErrorIs(t, model.InsertCreditCheckout(&duplicate, 1, model.CreditModuleTopup, trade, "different"), model.ErrCreditConflict) // 唯一键冲突不能吞掉异参。
	verifyCreditSubscriptionSnapshot(t, db)
	verifyCreditCheckoutFailures(t, db)
	verifyManualCashCompletion(t, db)
	verifyCreditCheckoutConcurrency(t, db) // 两笔真实付款竞争不足两单的共享授信。
}

// 下单后修改套餐不改变已确认权益；下架则拒绝尚未成功的付款。
// 续期公共逻辑保持原实现，首次购买验证新快照的额度和周期。
func verifyCreditSubscriptionSnapshot(t *testing.T, db *gorm.DB) {
	t.Helper()
	plan := model.SubscriptionPlan{Title: "frozen", PriceAmount: 2.50, Enabled: true, DurationUnit: model.SubscriptionDurationDay, DurationValue: 2, TotalAmount: 1234}
	require.NoError(t, db.Create(&plan).Error)
	req := creditCheckoutRequest{PlanID: plan.Id, QuotedCents: 250} // 用户确认的是下单价格，不是修改后的价格。
	require.NoError(t, createCreditCheckout(1, model.CreditModuleSubscription, "credit-frozen-plan", "snapshot", req))
	require.NoError(t, db.Model(&plan).Updates(map[string]any{"price_amount": 9, "total_amount": 9000, "duration_value": 30}).Error)
	require.NoError(t, model.PaySubscriptionOnAccount("credit-frozen-plan", 1)) // 首次付款只兑现冻结权益。
	require.NoError(t, model.PaySubscriptionOnAccount("credit-frozen-plan", 1))
	var sub model.UserSubscription
	require.NoError(t, db.Where("user_id = ? AND plan_id = ?", 1, plan.Id).First(&sub).Error)
	assert.EqualValues(t, 1234, sub.AmountTotal)              // 不能拿到管理员改价后的九千额度。
	assert.EqualValues(t, 2*86400, sub.EndTime-sub.StartTime) // 不能按新三十天周期或重试次数延长。
	mirror := model.GetTopUpByTradeNo("credit-frozen-plan")
	require.NotNil(t, mirror)
	assert.Equal(t, "credit", mirror.PaymentProvider)                                                                   // 订阅镜像不能进入现金总览。
	assert.Zero(t, mirror.CreditedQuota)                                                                                // 订阅购买不顺带给钱包充值。
	require.ErrorIs(t, model.ManualCompleteTopUp(mirror.TradeNo, "", 1, "receipt", ""), model.ErrPaymentMethodMismatch) // 人工补单入口拒绝订阅挂帐镜像。
	// 现金通用回调即便省略expected provider也不能自动结清挂帐单。
	require.ErrorIs(t, model.CompleteSubscriptionOrder(mirror.TradeNo, "", "", ""), model.ErrPaymentMethodMismatch)
	req.QuotedCents = 900
	require.NoError(t, createCreditCheckout(1, model.CreditModuleSubscription, "credit-disabled-plan", "disabled", req))
	require.NoError(t, db.Model(&plan).Update("enabled", false).Error)
	require.ErrorIs(t, model.PaySubscriptionOnAccount("credit-disabled-plan", 1), model.ErrCreditUnavailable) // 下架后未付款原单不再取得新权益。
	// 原成功请求在下架后仍恢复，但不能重复延长有效期。
	require.NoError(t, model.PaySubscriptionOnAccount("credit-frozen-plan", 1))
	apiPlan := model.SubscriptionPlan{Title: "http-plan", PriceAmount: 1.50, Enabled: true, DurationUnit: model.SubscriptionDurationDay, DurationValue: 1, TotalAmount: 10}
	require.NoError(t, db.Create(&apiPlan).Error)
	body := fmt.Sprintf(`{"plan_id":%d,"request_id":"fb957380-baf1-4a78-9b48-0d27bc377874","quoted_cents":150}`, apiPlan.Id)
	paid := creditCheckoutHTTP(t, RequestCreditSubscription, http.MethodPost, "/api/subscription/credit/pay", body)
	require.Equal(t, true, paid["success"], "%v", paid) // 验证订阅真实JSON绑定和下单入口，不只测试模型。
	require.NoError(t, db.Model(&apiPlan).Updates(map[string]any{"enabled": false, "price_amount": 9}).Error)
	require.NoError(t, db.Model(&model.CreditAccount{}).Where("user_id = ?", 1).Update("enabled", false).Error)
	retried := creditCheckoutHTTP(t, RequestCreditSubscription, http.MethodPost, "/api/subscription/credit/pay", body)
	assert.Equal(t, paid, retried) // 下架、改价、授权关闭均不妨碍恢复旧成功请求。
	bad := creditCheckoutHTTP(t, RequestCreditSubscription, http.MethodPost, "/api/subscription/credit/pay", strings.Replace(body, `"quoted_cents":150`, `"quoted_cents":150,"invoice_required":true`, 1))
	assert.Equal(t, false, bad["success"]) // 即便交易号相同，也不能新增发票意图。
	bad = creditCheckoutHTTP(t, RequestCreditSubscription, http.MethodPost, "/api/subscription/credit/pay", strings.Replace(body, "-4a78-", "-1a78-", 1))
	assert.Equal(t, false, bad["success"]) // 只支持UUIDv4，不能接受时间型标识符。
	require.NoError(t, db.Model(&model.CreditAccount{}).Where("user_id = ?", 1).Update("enabled", true).Error)
}

// 非整分、到期、跨用户和Hub故障都必须阻止整个事务提交。
// 通过真实坏事件表触发失败，确认占额和发放也一起回滚。
func verifyCreditCheckoutFailures(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, scenario := range []struct {
		name    string
		money   float64
		created int64
	}{
		{"fractional", 1.001, common.GetTimestamp()},
		{"expired", 1, common.GetTimestamp() - model.OfficialTopUpTTLSeconds},
	} {
		order := model.TopUp{UserId: 1, TradeNo: scenario.name, Money: scenario.money, CreditedQuota: 100, CreateTime: scenario.created, PaymentProvider: model.PaymentProviderCredit, Status: common.TopUpStatusPending}
		require.NoError(t, order.Insert())
		require.Error(t, model.PayTopUpOnAccount(order.TradeNo, 1))                               // 坏金额或超期订单不能生成债务。
		assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(order.TradeNo).Status) // 失败不得留下成功状态。
	}
	require.Error(t, model.PayTopUpOnAccount("fractional", 2)) // 其他用户不能认领原请求。
	before, err := model.GetCreditAccount(1)
	require.NoError(t, err)
	order := model.TopUp{UserId: 1, TradeNo: "hub-rollback", Money: 1, CreditedQuota: 100, CreateTime: common.GetTimestamp(), PaymentProvider: model.PaymentProviderCredit, Status: common.TopUpStatusPending}
	require.NoError(t, order.Insert())
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Update("status", common.UserStatusDisabled).Error)
	require.ErrorIs(t, model.PayTopUpOnAccount(order.TradeNo, 1), model.ErrCreditUnavailable) // 用户停用不能借仍开启的授信新购。
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Update("status", common.UserStatusEnabled).Error)
	require.NoError(t, db.Migrator().DropTable(&model.HubEvent{})) // 仅删除隔离测试表以触发持久事件写入故障。
	require.Error(t, model.PayTopUpOnAccount(order.TradeNo, 1))
	after, err := model.GetCreditAccount(1)
	require.NoError(t, err)
	assert.Equal(t, before.UsedCents, after.UsedCents) // 事件失败必须同时撤销授信占用。
	assert.Equal(t, common.TopUpStatusPending, model.GetTopUpByTradeNo(order.TradeNo).Status)
	require.NoError(t, model.MigrateEventHub(db))
	// 严格转换先验证整数分与上限，不能依赖 IntPart 截断。
	for _, invalid := range []float64{0, -1, 0.001, math.NaN(), math.Inf(1), float64(model.MaxCreditCents)/100 + 1} {
		_, err := model.CreditMoneyCents(invalid)
		require.Error(t, err)
	}
	cents, err := model.CreditMoneyCents(float64(model.MaxCreditCents) / 100)
	require.NoError(t, err)
	assert.Equal(t, model.MaxCreditCents, cents) // 合法上限应被完整接收而非多减一分。
}

// 补单留存原现金渠道且只发放一次；随后网关通知也必须幂等。
// 原微信事件不添加挂帐应收字段，避免破坏现有消费者合同。
func verifyManualCashCompletion(t *testing.T, db *gorm.DB) {
	t.Helper()
	order := model.TopUp{UserId: 1, TradeNo: "manual-wechat", Money: 1.23, CreditedQuota: 456, PaymentProvider: model.PaymentProviderWechat, PaymentMethod: "wx-fixture", Status: common.TopUpStatusPending}
	require.NoError(t, order.Insert())
	require.Error(t, model.ManualCompleteTopUp(order.TradeNo, "", 9, "", ""))                         // 无收款凭证不能进行人工到账。
	require.NoError(t, model.ManualCompleteTopUp(order.TradeNo, "", 9, "verified-first", "已核实原渠道收款")) // 原付款事实只补发一次权益。
	require.NoError(t, model.ManualCompleteTopUp(order.TradeNo, "", 10, "second", ""))                // 另一操作者重试也不能覆盖首次审计。
	require.NoError(t, model.CompleteOfficialTopUp(order.TradeNo, model.PaymentProviderWechat, "wx-fixture", decimal.NewFromFloat(1.23), ""))
	saved := model.GetTopUpByTradeNo(order.TradeNo)
	require.NotNil(t, saved)
	assert.True(t, saved.ManualCompleted)                               // 人工性质必须有持久标记。
	assert.Equal(t, 9, saved.ManualActor)                               // 首次核实收款者不能被后续请求改写。
	assert.Equal(t, "verified-first", saved.ManualReceiptRef)           // 保留首次凭证便于财务对账。
	assert.Equal(t, model.PaymentProviderWechat, saved.PaymentProvider) // 补单不能把真实渠道改成admin。
	assert.Equal(t, 1.23, saved.Money)                                  // 人工入口无权重写原款项金额。
	assert.Equal(t, 456, saved.CreditedQuota)                           // 补单同样沿用原冻结额度。
	var event model.HubEvent
	require.NoError(t, db.Where("event_key = ?", fmt.Sprintf("topup:%d", order.Id)).First(&event).Error)
	var payload map[string]any
	require.NoError(t, common.UnmarshalJsonStr(event.Payload, &payload))
	assert.Equal(t, "1.23", payload["paid_amount"])     // 原微信事件继续表示原渠道实际收款。
	assert.NotContains(t, payload, "receivable_amount") // 只给挂帐事件新增应收字段。
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, 50456, user.Quota) // 人工重试与回调合计仍只发一次额度。
	mirror := model.TopUp{UserId: 1, TradeNo: "cash-subscription-mirror", Money: 1, CreditedQuota: 100, PaymentProvider: model.PaymentProviderWechat, Status: common.TopUpStatusPending}
	require.NoError(t, mirror.Insert())
	subOrder := model.SubscriptionOrder{UserId: 1, PlanId: 1, TradeNo: mirror.TradeNo, Money: 1, PaymentProvider: model.PaymentProviderWechat, Status: common.TopUpStatusSuccess}
	require.NoError(t, subOrder.Insert())
	require.ErrorIs(t, model.ManualCompleteTopUp(mirror.TradeNo, "", 9, "verified", ""), model.ErrPaymentMethodMismatch) // 真实现金订阅的异常镜像也不能补发钱包。
	international := model.TopUp{UserId: 1, TradeNo: "unknown-currency", Money: 1, CreditedQuota: 100, PaymentProvider: model.PaymentProviderStripe, Status: common.TopUpStatusPending}
	require.NoError(t, international.Insert())
	require.ErrorIs(t, model.ManualCompleteTopUp(international.TradeNo, "", 9, "verified", ""), model.ErrPaymentMethodMismatch) // 不猜测国际记录金额就是人民币实收。
}

// 两个不同订单争抢同一份授信，必须由真实数据库锁保证最多成功一笔。
// 测试不依赖进程内LockOrder，覆盖跨实例付款所需的模型约束。
// SQLite写竞争可报告锁错误，但不能让成功单丢失债务或超额发放。
func verifyCreditCheckoutConcurrency(t *testing.T, db *gorm.DB) {
	t.Helper()
	account, err := model.GetCreditAccount(1)
	require.NoError(t, err)
	// 精确只剩一单可用额度，两个事务同时启动。
	require.NoError(t, db.Model(&model.CreditAccount{}).Where("user_id = ?", 1).Update("limit_cents", account.UsedCents+100).Error)
	orders := []model.TopUp{
		{UserId: 1, TradeNo: "compete-one", Money: 1, CreditedQuota: 100, CreateTime: common.GetTimestamp(), PaymentProvider: model.PaymentProviderCredit, Status: common.TopUpStatusPending},
		{UserId: 1, TradeNo: "compete-two", Money: 1, CreditedQuota: 100, CreateTime: common.GetTimestamp(), PaymentProvider: model.PaymentProviderCredit, Status: common.TopUpStatusPending},
	}
	for i := range orders {
		require.NoError(t, orders[i].Insert())
	} // 订单先完整落库，不将建单错误混入付款竞争。
	var wait sync.WaitGroup
	results := make([]error, len(orders))
	for i := range orders {
		wait.Go(func() { results[i] = model.PayTopUpOnAccount(orders[i].TradeNo, 1) })
	}
	wait.Wait() // 两个事务都结束后检查持久结果，不用sleep猜测时序。
	successes, paid := 0, -1
	for i, result := range results {
		if result == nil {
			successes++
			paid = i
		}
	}
	require.Equal(t, 1, successes, "%v", results)                                                // 超额竞争不能两笔都发放，也不能伪造成功。
	require.NoError(t, model.PayTopUpOnAccount(orders[paid].TradeNo, 1))                         // 成功重试在余额已满时仍必须恢复。
	require.ErrorIs(t, model.PayTopUpOnAccount(orders[1-paid].TradeNo, 1), model.ErrCreditLimit) // 未成功原单不能借重试突破授信。
	current, err := model.GetCreditAccount(1)
	require.NoError(t, err)
	assert.Equal(t, account.UsedCents+100, current.UsedCents) // 同用户不同单共用总额度，不能分别预检后重复占额。
	var count int64
	require.NoError(t, db.Model(&model.CreditEntry{}).Where("trade_no IN ?", []string{"compete-one", "compete-two"}).Count(&count).Error)
	assert.EqualValues(t, 1, count) // 被拒订单不得留下欠款记录。
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, 50556, user.Quota) // 钱包只增加获准一单的冻结额度。
}
