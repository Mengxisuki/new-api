package controller

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 真实三库覆盖新建、升级及重复迁移；连接器为每项创建隔离库。
// 历史现金订单、钱包余额与唯一约束不能因新增挂帐表而变化。
func TestCreditDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		for _, upgrade := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/upgrade=%v", dialect, upgrade), func(t *testing.T) {
				dsn := os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN") // 仅使用显式测试连接，未配置不能假报跨库通过。
				if dialect != "sqlite" && dsn == "" {
					t.Skip("test database DSN is not configured")
				}
				db := modelManagementDB(t, dialect, dsn) // 真实方言初始化，测试之间不共用业务表。
				// 本轮没有改旧订单 schema，升级先创建现行发布结构与实际历史订单。
				if upgrade { // 有旧结构与空库安装分别验证。
					require.NoError(t, db.AutoMigrate(&model.AccountRechargeProduct{}, &model.AccountRechargeOrder{}, &model.AccountRechargeEvent{}))
					old := model.AccountRechargeOrder{UserID: 999, RequestID: "old", TradeNo: "RCHUSR-old", PaymentProvider: "wechat", PaymentStatus: "paid", MoneyCents: 15678, Version: 7}
					require.NoError(t, db.Create(&old).Error) // 历史记录先落库，再执行新增迁移。
				}
				tables := []any{&model.CreditAccount{}, &model.CreditEntry{}, &model.AccountRechargeProduct{}, &model.AccountRechargeOrder{}, &model.AccountRechargeEvent{}, &model.InvoiceRequest{}}
				for range 2 {
					require.NoError(t, db.AutoMigrate(tables...))
				}
				if upgrade {
					var old model.AccountRechargeOrder
					require.NoError(t, db.Where("trade_no = ?", "RCHUSR-old").First(&old).Error)
					assert.EqualValues(t, 15678, old.MoneyCents)   // 新授信表不能重算旧现金订单金额。
					assert.Equal(t, "wechat", old.PaymentProvider) // 升级不能把原微信付款转换为挂帐。
					assert.Equal(t, 7, old.Version)                // 原订单并发版本必须保持。
				}
				verifyCreditLifecycle(t, db) // 用公共模型方法验证实际资金行为。
				// 重复启动也必须保留结清与停用结果，不会重置授信或欠款。
				require.NoError(t, db.AutoMigrate(tables...))
				account, err := model.GetCreditAccount(1)
				require.NoError(t, err)
				require.NotNil(t, account)
				assert.EqualValues(t, 0, account.UsedCents) // 重启迁移不能恢复已经归还的占用。
			})
		}
	}
}

// 支付走真实订单事务；测试不复制额度计算作为被测实现。
// 退款分别覆盖欠款撤销和已结清现金返还，防止重复恢复额度。
func verifyCreditLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	buyer := model.User{Id: 1, Username: "credit-buyer", AffCode: "credit", Status: common.UserStatusEnabled, Quota: 12345}
	require.NoError(t, db.Create(&buyer).Error) // 钱包余额是验证挂帐资金隔离的基线。
	product := model.AccountRechargeProduct{Title: "Credit plan", Enabled: true, PriceCents: 6000}
	require.NoError(t, model.SaveAccountRechargeProduct(&product))                                    // 使用生产商品校验生成可信价格。
	require.ErrorIs(t, model.CheckCreditAvailable(nil, "recharge", 6000), model.ErrCreditUnavailable) // 未配置用户没有隐含额度。
	config := model.CreditAccount{UserID: buyer.Id, Enabled: true, LimitCents: 10000, RechargeEnabled: true}
	require.NoError(t, model.SaveCreditAccount(config, 100)) // 首次授信需要明确启用。
	// 首次配置竞争不能覆盖现有授信；两个新模块开关可独立保存。
	require.ErrorIs(t, model.SaveCreditAccount(config, 100), model.ErrCreditConflict) // 版本零不能覆盖已有账户。
	config.Version, config.TopupEnabled = 1, true
	require.NoError(t, model.SaveCreditAccount(config, 100)) // topup 开关与 recharge 共用授信总额。
	config.Version++
	config.TopupEnabled = false
	require.ErrorIs(t, model.CheckCreditAvailable(&config, "subscription", 6000), model.ErrCreditUnavailable) // 总开关不能替代逐模块授权。
	orders := make([]*model.AccountRechargeOrder, 2)                                                          // 两张真实订单共同竞争不足两单的授信。
	for i := range orders {
		order := &model.AccountRechargeOrder{UserID: buyer.Id, ProductID: product.ID, BaseCents: 6000, MoneyCents: 6000, TradeNo: fmt.Sprintf("RCHUSR-credit-%d", i), RequestID: fmt.Sprintf("credit-%d", i), RequestHash: "same", PaymentProvider: "credit", PaymentMethod: "credit", ExpiresAt: common.GetTimestamp() + 1800}
		var err error
		orders[i], err = model.CreateAccountRechargeOrder(order, product.Version, nil) // 创建只冻结订单，尚不占额。
		require.NoError(t, err)
	}
	// 两个不同订单竞争同一授信，最多一笔成功；SQLite锁冲突可显式重试。
	var wait sync.WaitGroup
	results := make([]error, 2)
	for i := range orders {
		wait.Go(func() { results[i] = model.PayAccountRechargeCredit(orders[i].ID, buyer.Id) })
	}
	wait.Wait() // 等待所有并发事务完成后核对账本。
	successes := 0
	var paid *model.AccountRechargeOrder
	for i, err := range results {
		if err == nil {
			successes++
			paid = orders[i]
		}
	}
	require.Equal(t, 1, successes, "%v", results)                         // 超额竞争只允许一笔订单获准履约。
	require.NoError(t, model.PayAccountRechargeCredit(paid.ID, buyer.Id)) // 同单重复确认恢复原结果。
	account, err := model.GetCreditAccount(buyer.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 6000, account.UsedCents) // 不能重复累计已成功订单。
	var entries []model.CreditEntry
	require.NoError(t, db.Find(&entries).Error)
	require.Len(t, entries, 1)                        // 账本不能多出失败或重复支付记录。
	assert.Equal(t, "outstanding", entries[0].Status) // 挂帐成功仍然是未收款。
	// 外部回调不能伪造挂帐成功；授信不能通过钱包产生额外资金。
	require.ErrorIs(t, model.CompleteAccountRechargePayment(paid.TradeNo, "credit", "credit", "forged", decimal.NewFromInt(60)), model.ErrRechargeInvalid)
	require.NoError(t, db.First(&buyer, buyer.Id).Error)
	assert.Equal(t, 12345, buyer.Quota)                                            // 平台钱包既不增加也不减少。
	config.LimitCents = 5000                                                       // 故意低于未结清欠款以验证修改下限。
	require.ErrorIs(t, model.SaveCreditAccount(config, 100), model.ErrCreditLimit) // 管理员不能制造负可用额度。
	config.LimitCents, config.Enabled = 10000, false                               // 停用允许保留现有欠款。
	require.NoError(t, model.SaveCreditAccount(config, 100))
	for _, order := range orders {
		if order.ID != paid.ID {
			require.Error(t, model.PayAccountRechargeCredit(order.ID, buyer.Id)) // 旧未付订单不能绕过停用。
		}
	}
	require.NoError(t, model.PayAccountRechargeCredit(paid.ID, buyer.Id))                                // 停用不阻止原已付款结果恢复。
	require.ErrorIs(t, model.SettleCreditEntry(entries[0].ID, 5, "", ""), model.ErrCreditInvalid)        // 线下收款凭证不可省略。
	require.NoError(t, model.SettleCreditEntry(entries[0].ID, 5, "offline-001", "full amount received")) // 实际结清入口归还占用。
	require.NoError(t, model.SettleCreditEntry(entries[0].ID, 5, "repeated", ""))                        // 重复结清不会再增加可用额度。
	require.NoError(t, db.First(&entries[0], entries[0].ID).Error)
	assert.Equal(t, "offline-001", entries[0].SettlementRef) // 首次收款证据不被重试覆盖。
	account, err = model.GetCreditAccount(buyer.Id)
	require.NoError(t, err)
	assert.Zero(t, account.UsedCents) // 整笔收款后恢复全部占用。
	// 已收款退款不可无凭证确认，也不能再次减去授信占用。
	paid, err = model.GetAccountRechargeOrder(paid.ID, buyer.Id)
	require.NoError(t, err)
	require.NoError(t, model.RefundAccountRecharge(paid.ID, 100, paid.Version, false, "cancel service", "")) // 批准先冻结业务办理。
	paid, err = model.GetAccountRechargeOrder(paid.ID, buyer.Id)
	require.NoError(t, err)
	require.ErrorIs(t, model.RefundAccountRecharge(paid.ID, 100, paid.Version, true, "cash returned", ""), model.ErrCreditInvalid) // 已收款必须先实际退款。
	require.NoError(t, model.RefundAccountRecharge(paid.ID, 100, paid.Version, true, "cash returned", "refund-001"))
	require.NoError(t, model.RefundAccountRecharge(paid.ID, 100, paid.Version, true, "cash returned", "refund-001"))
	// 重新开放代充值，使用另一张真实订单验证未收款撤销。
	account.Enabled = true                                     // 恢复授权不应丢失已结清历史。
	require.NoError(t, model.SaveCreditAccount(*account, 100)) // 保存使用当前真实配置版本。
	for _, order := range orders {
		if order.ID == paid.ID { // 另一张尚未付款的订单用于欠款撤销分支。
			continue
		}
		require.NoError(t, model.PayAccountRechargeCredit(order.ID, buyer.Id)) // 额度已恢复，应允许原第二单使用。
		current, err := model.GetAccountRechargeOrder(order.ID, buyer.Id)
		require.NoError(t, err)
		require.NoError(t, model.RefundAccountRecharge(order.ID, 100, current.Version, false, "cancel debt", "")) // 未结清退款也必须先停止履约。
		current, err = model.GetAccountRechargeOrder(order.ID, buyer.Id)
		require.NoError(t, err)
		var entry model.CreditEntry
		require.NoError(t, db.Where("trade_no = ?", order.TradeNo).First(&entry).Error)
		require.ErrorIs(t, model.SettleCreditEntry(entry.ID, 5, "cannot settle during refund", ""), model.ErrCreditConflict) // 退款中不能同时登记现金收款。
		require.NoError(t, model.RefundAccountRecharge(order.ID, 100, current.Version, true, "cancel debt", ""))             // 撤销欠款无需虚构网关凭证。
		require.ErrorIs(t, model.SettleCreditEntry(entry.ID, 5, "cannot settle cancelled", ""), model.ErrCreditConflict)     // 已撤销债务不能重新结清。
	}
	// 实际列表汇总验证状态筛选、用户名关联以及分页返回合同。
	rows, total, amount, err := model.ListCreditEntries(1, 20, buyer.Id, "cancelled", "credit-buyer")
	require.NoError(t, err)
	require.EqualValues(t, 1, total) // 只计算筛选范围内的一笔撤销记录。
	require.Len(t, rows, 1)
	assert.EqualValues(t, 6000, amount) // 合计来自整笔冻结金额。
	accountRows, total, err := model.ListCreditAccounts(1, 20, "credit-buyer")
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	assert.Zero(t, accountRows[0].UsedCents) // 只读用户摘要与资金事务结果一致。
	// 人为制造真实事件唯一键冲突，验证占额后失败会原子回滚。
	rollbackOrder := &model.AccountRechargeOrder{UserID: buyer.Id, ProductID: product.ID, BaseCents: 6000, MoneyCents: 6000, TradeNo: "RCHUSR-credit-rollback", RequestID: "credit-rollback", RequestHash: "same", PaymentProvider: "credit", PaymentMethod: "credit", ExpiresAt: common.GetTimestamp() + 1800}
	rollbackOrder, err = model.CreateAccountRechargeOrder(rollbackOrder, product.Version, nil)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.AccountRechargeEvent{OrderID: rollbackOrder.ID, Version: 2, Action: "credit_charge"}).Error)
	require.Error(t, model.PayAccountRechargeCredit(rollbackOrder.ID, buyer.Id)) // 事件唯一冲突必须作为失败可见。
	var rollbackEntries int64
	require.NoError(t, db.Model(&model.CreditEntry{}).Where("trade_no = ?", rollbackOrder.TradeNo).Count(&rollbackEntries).Error)
	assert.Zero(t, rollbackEntries) // 后续失败不能留下孤立债务。
	rollbackOrder, err = model.GetAccountRechargeOrder(rollbackOrder.ID, buyer.Id)
	require.NoError(t, err)
	assert.Equal(t, "pending", rollbackOrder.PaymentStatus) // 回滚后订单不能获得履约资格。
	account, err = model.GetCreditAccount(buyer.Id)
	require.NoError(t, err)
	assert.Zero(t, account.UsedCents)
	// 只关闭模块也必须拒绝旧单，不能仅检查用户授信总开关。
	account.RechargeEnabled = false
	require.NoError(t, model.SaveCreditAccount(*account, 100))
	require.ErrorIs(t, model.PayAccountRechargeCredit(rollbackOrder.ID, buyer.Id), model.ErrCreditUnavailable)
	account.Version++ // 下次编辑必须使用本次保存后的配置版本。
	account.RechargeEnabled = true
	require.NoError(t, model.SaveCreditAccount(*account, 100))
	// 所有者隔离与订单有效期都在实际支付事务中执行。
	require.ErrorIs(t, model.PayAccountRechargeCredit(rollbackOrder.ID, buyer.Id+999), gorm.ErrRecordNotFound)
	require.NoError(t, db.Model(rollbackOrder).Update("expires_at", common.GetTimestamp()-1).Error)
	require.ErrorIs(t, model.PayAccountRechargeCredit(rollbackOrder.ID, buyer.Id), model.ErrRechargeUnavailable)
	// 唯一约束仍在，直接重复业务键不能制造第二笔债务。
	duplicate := entries[0]
	duplicate.ID = 0                              // 保留业务键，仅改变数据库主键。
	require.Error(t, db.Create(&duplicate).Error) // 重复迁移没有破坏业务键唯一性。
}

// 使用真实会话认证与能力中间件，从目录、报价、创建进入挂帐付款。
// 请求体故意不能包含任意所有者；新用户默认没有授信入口。
func TestCreditCheckoutAndPermissions(t *testing.T) {
	modelManagementDB(t, "sqlite", "")
	buyer, identity := setupSecurityEnrollmentTest(t) // 真实会话验证代替伪造角色上下文。
	require.NoError(t, model.DB.AutoMigrate(&model.CreditAccount{}, &model.CreditEntry{}, &model.AccountRechargeProduct{}, &model.AccountRechargeOrder{}, &model.AccountRechargeEvent{}, &model.InvoiceRequest{}))
	t.Setenv("OAK_RECHARGE_DATA_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	previous := *operation_setting.GetPaymentSetting() // 支付配置在测试结束后完整恢复。
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() { *operation_setting.GetPaymentSetting() = previous })
	token, _, err := service.IssueAccessToken(identity)
	require.NoError(t, err)
	product := model.AccountRechargeProduct{Title: "Credit checkout", Enabled: true, PriceCents: 1000}
	require.NoError(t, model.SaveAccountRechargeProduct(&product))
	engine := gin.New() // 以最小路由串接生产鉴权与处理器。
	route := engine.Group("/api/account-recharge", middleware.UserAuth(), RechargeSessionOnly)
	route.GET("/products", GetRechargeProducts)
	route.POST("/quote", QuoteRechargeOrder)
	route.POST("/orders", CreateRechargeOrder)
	route.POST("/orders/:id/pay", PayRechargeOrder)
	route.GET("/orders/:id", GetRechargeOrder)
	// 权限真实执行，不能用页面按钮隐藏作为资金授权测试。
	engine.GET("/api/option/credit/accounts", middleware.RootAuth(), RechargeSessionOnly, GetCreditAccounts)
	engine.PUT("/api/option/credit/accounts", middleware.RootAuth(), RechargeSessionOnly, SaveCreditAccount)
	engine.POST("/api/finance/credit/entries/:id/settle", middleware.FinanceAuth(), RechargeSessionOnly, middleware.RequirePermission(authz.FinanceWrite), SettleCreditEntry)
	engine.GET("/api/finance/credit/entries", middleware.FinanceAuth(), RechargeSessionOnly, middleware.RequirePermission(authz.FinanceRead), GetCreditEntries)
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		data, err := common.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		req.Header.Set("Authorization", "Bearer "+token) // 会话与个人令牌均走实际身份分类。
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req) // 通过真实路由触发业务，不直接调用内部余额逻辑。
		return response
	}
	assert.NotContains(t, request("GET", "/api/account-recharge/products", nil).Body.String(), `"id":"credit"`)
	purchase := rechargePurchase{ProductID: product.ID, ProductVersion: product.Version, PaymentMethod: "credit", RequestID: "7cb148cb-7686-4d44-9349-1d8b9ce07121", QuotedCents: 1000}
	assert.Equal(t, 400, request("POST", "/api/account-recharge/quote", purchase).Code)
	require.NoError(t, model.SaveCreditAccount(model.CreditAccount{UserID: buyer.Id, LimitCents: 1000, Enabled: true, RechargeEnabled: true}, 100))
	assert.Contains(t, request("GET", "/api/account-recharge/products", nil).Body.String(), `"id":"credit"`)
	purchase.InvoiceRequired = true // 主动伪造挂帐开票请求必须被后端拒绝。
	assert.Equal(t, 400, request("POST", "/api/account-recharge/quote", purchase).Code)
	purchase.InvoiceRequired = false // 合法挂帐请求不创建付款开票申请。
	response := request("POST", "/api/account-recharge/orders", purchase)
	require.Equal(t, 200, response.Code, response.Body.String())
	var envelope struct {
		Success bool
		Data    model.AccountRechargeOrder
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
	require.True(t, envelope.Success, response.Body.String())
	path := fmt.Sprintf("/api/account-recharge/orders/%d", envelope.Data.ID)
	assert.Equal(t, 200, request("POST", path+"/pay", gin.H{}).Code)
	assert.Contains(t, request("GET", path, nil).Body.String(), `"credit_status":"outstanding"`)
	assert.Equal(t, 400, request("POST", "/api/account-recharge/quote", purchase).Code) // 原额度已占满。
	assert.NotEqual(t, 200, request("GET", "/api/option/credit/accounts", nil).Code)    // 买家不能枚举管理员授信列表。
	assert.NotEqual(t, 200, request("GET", "/api/finance/credit/entries", nil).Code)    // 买家不能借财务接口查看其他债务。
	// 从真实财务登录会话读取和结清；该身份仍不能修改授信配置。
	require.NoError(t, model.DB.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	oldMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = oldMaster })
	require.NoError(t, authz.Init(model.DB)) // 财务能力从正式权限目录加载。
	finance := model.User{Username: "credit-finance", AffCode: "credit-f", Role: common.RoleFinanceUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, model.DB.Create(&finance).Error)
	bundle, err := service.CreateLoginSession(finance.Id, "password", "127.0.0.1", "credit-test")
	require.NoError(t, err)
	token = bundle.AccessToken // 后续请求使用真正的财务会话。
	assert.Equal(t, 200, request("GET", "/api/finance/credit/entries", nil).Code)
	assert.NotEqual(t, 200, request("PUT", "/api/option/credit/accounts", gin.H{"user_id": buyer.Id, "limit_cents": 999999}).Code)
	var entry model.CreditEntry
	require.NoError(t, model.DB.Where("trade_no = ?", envelope.Data.TradeNo).First(&entry).Error)
	result := request("POST", fmt.Sprintf("/api/finance/credit/entries/%d/settle", entry.ID), gin.H{"reference": "offline-http"})
	assert.Equal(t, 200, result.Code, result.Body.String())
	require.NoError(t, model.DB.First(&entry, entry.ID).Error)
	assert.Equal(t, "settled", entry.Status)     // 控制器已经接入真实结清事务。
	assert.Equal(t, finance.Id, entry.SettledBy) // 登记人不能由请求体伪造。
	// 个人令牌即使有财务范围，也不能替代线下收款确认所需网页登录。
	raw := "pat_credit_test_only_not_a_real_credential"
	pat := &model.UserAccessToken{Name: "credit-fixture", TokenHash: model.AccessTokenFingerprint(raw), TokenHint: model.AccessTokenHint(raw)}
	require.NoError(t, pat.SetScopes([]string{"finance:read", "finance:write"})) // 有范围也不代表拥有会话专用入口。
	require.NoError(t, model.CreateUserAccessToken(finance.Id, pat, service.AccessTokenMaxPerUser))
	token = raw // 个人令牌请求复用同一实际鉴权链路。
	assert.NotEqual(t, 200, request("GET", "/api/finance/credit/entries", nil).Code)

}

// 第二期授信回归覆盖三个模块共用占用、人工来源绑定和坏账核销。
// 失败来源校验必须在释放之前返回，重复核销不能覆盖首次原因。
func TestCreditSharedModulesAndWriteOff(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.SubscriptionOrder{}, &model.CreditAccount{}, &model.CreditEntry{}, &model.QuotaLedger{}))
	user := model.User{Id: 7001, Username: "credit-phase2", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, model.SaveCreditAccount(model.CreditAccount{
		UserID: user.Id, Enabled: true, LimitCents: 3000,
		RechargeEnabled: true, TopupEnabled: true, SubscriptionEnabled: true,
	}, 9))

	var manualEntry model.CreditEntry
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		ledger := model.QuotaLedger{
			UserID: user.Id, TradeNo: "MANUAL-TOPUP-CREDIT", Source: model.ManualSourceQuota,
			ReceivableCents: 1000, Delta: 1, CreatedAt: common.GetTimestamp(),
		}
		if err := tx.Create(&ledger).Error; err != nil {
			return err
		}
		entry, err := model.ChargeCreditTx(tx, user.Id, model.CreditModuleTopup, ledger.TradeNo, 1000)
		if err != nil {
			return err
		}
		ledger.CreditEntryID = entry.ID
		manualEntry = *entry
		return tx.Model(&ledger).Update("credit_entry_id", entry.ID).Error
	}))

	// 三个模块都从同一个授信账户原子占用，未知模块和非正金额不能创建债务。
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if _, err := model.ChargeCreditTx(tx, user.Id, model.CreditModuleRecharge, "RECHARGE-SHARED", 500); err != nil {
			return err
		}
		_, err := model.ChargeCreditTx(tx, user.Id, model.CreditModuleSubscription, "SUBSCRIPTION-SHARED", 500)
		return err
	}))
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := model.ChargeCreditTx(tx, user.Id, "invalid", "INVALID-MODULE", 1)
		return err
	}), model.ErrCreditInvalid)
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := model.ChargeCreditTx(tx, user.Id, model.CreditModuleTopup, "ZERO-CREDIT", 0)
		return err
	}), model.ErrCreditInvalid)
	account, err := model.GetCreditAccount(user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 2000, account.UsedCents)

	require.NoError(t, model.WriteOffCreditEntry(manualEntry.ID, 9, "customer insolvency"))
	require.NoError(t, model.WriteOffCreditEntry(manualEntry.ID, 10, "a later reason must not win"))
	var written model.CreditEntry
	require.NoError(t, db.First(&written, manualEntry.ID).Error)
	assert.Equal(t, "write_off", written.Status)
	assert.Equal(t, "customer insolvency", written.WriteOffNote)
	account, err = model.GetCreditAccount(user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1000, account.UsedCents)
}

// 来源金额或债务编号不匹配时，结清和核销都不能先释放授信占用。
func TestCreditSourceMismatchDoesNotRelease(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.SubscriptionOrder{}, &model.CreditAccount{}, &model.CreditEntry{}, &model.QuotaLedger{}))
	user := model.User{Id: 7002, Username: "credit-source-mismatch", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.CreditAccount{UserID: user.Id, Enabled: true, LimitCents: 1000, TopupEnabled: true}).Error)
	entry := model.CreditEntry{UserID: user.Id, Module: model.CreditModuleTopup, TradeNo: "MANUAL-BAD-SOURCE", AmountCents: 1000, Status: "outstanding", CreatedAt: common.GetTimestamp()}
	require.NoError(t, db.Create(&entry).Error)
	require.NoError(t, db.Model(&model.CreditAccount{}).Where("user_id = ?", user.Id).Update("used_cents", 1000).Error)
	require.NoError(t, db.Create(&model.QuotaLedger{
		UserID: user.Id, TradeNo: entry.TradeNo, Source: model.ManualSourceQuota,
		ReceivableCents: 999, CreditEntryID: entry.ID, Delta: 1, CreatedAt: common.GetTimestamp(),
	}).Error)
	require.ErrorIs(t, model.WriteOffCreditEntry(entry.ID, 9, "should fail before release"), model.ErrCreditConflict)
	require.ErrorIs(t, model.SettleCreditEntry(entry.ID, 9, "offline-bad-source", ""), model.ErrCreditConflict)
	var account model.CreditAccount
	require.NoError(t, db.First(&account, user.Id).Error)
	assert.EqualValues(t, 1000, account.UsedCents)
	require.NoError(t, db.First(&entry, entry.ID).Error)
	assert.Equal(t, "outstanding", entry.Status)
}
