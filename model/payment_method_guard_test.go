package model

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func insertUserForPaymentGuardTest(t *testing.T, id int, quota int) *User {
	t.Helper()
	user := &User{
		Id:       id,
		Username: "payment_guard_user",
		Status:   common.UserStatusEnabled,
		Quota:    quota,
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

func insertSubscriptionPlanForPaymentGuardTest(t *testing.T, id int) *SubscriptionPlan {
	t.Helper()
	plan := &SubscriptionPlan{
		Id:            id,
		Title:         "Guard Plan",
		PriceAmount:   9.99,
		Currency:      "USD",
		DurationUnit:  SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, DB.Create(plan).Error)
	return plan
}

func insertSubscriptionOrderForPaymentGuardTest(t *testing.T, tradeNo string, userID int, planID int, paymentProvider string) {
	t.Helper()
	order := &SubscriptionOrder{
		UserId:          userID,
		PlanId:          planID,
		Money:           9.99,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentProvider,
		PaymentProvider: paymentProvider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, order.Insert())
}

func insertTopUpForPaymentGuardTest(t *testing.T, tradeNo string, userID int, paymentProvider string) {
	t.Helper()
	topUp := &TopUp{
		UserId:          userID,
		Amount:          2,
		Money:           9.99,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentProvider,
		PaymentProvider: paymentProvider,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())
}

func getTopUpStatusForPaymentGuardTest(t *testing.T, tradeNo string) string {
	t.Helper()
	topUp := GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, topUp)
	return topUp.Status
}

func countUserSubscriptionsForPaymentGuardTest(t *testing.T, userID int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", userID).Count(&count).Error)
	return count
}

func getUserQuotaForPaymentGuardTest(t *testing.T, userID int) int {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&user).Error)
	return user.Quota
}

func TestRechargeWaffoPancake_RejectsMismatchedPaymentMethod(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 101, 0)
	insertTopUpForPaymentGuardTest(t, "waffo-pancake-guard", 101, PaymentProviderStripe)

	err := RechargeWaffoPancake("waffo-pancake-guard")
	require.Error(t, err)

	topUp := GetTopUpByTradeNo("waffo-pancake-guard")
	require.NotNil(t, topUp)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, 101))
}

func TestUpdatePendingTopUpStatus_RejectsMismatchedPaymentProvider(t *testing.T) {
	testCases := []struct {
		name                    string
		tradeNo                 string
		storedPaymentProvider   string
		expectedPaymentProvider string
		targetStatus            string
	}{
		{
			name:                    "stripe expire",
			tradeNo:                 "stripe-expire-guard",
			storedPaymentProvider:   PaymentProviderCreem,
			expectedPaymentProvider: PaymentProviderStripe,
			targetStatus:            common.TopUpStatusExpired,
		},
		{
			name:                    "waffo failed",
			tradeNo:                 "waffo-failed-guard",
			storedPaymentProvider:   PaymentProviderStripe,
			expectedPaymentProvider: PaymentProviderWaffo,
			targetStatus:            common.TopUpStatusFailed,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			insertUserForPaymentGuardTest(t, 150, 0)
			insertTopUpForPaymentGuardTest(t, tc.tradeNo, 150, tc.storedPaymentProvider)

			err := UpdatePendingTopUpStatus(tc.tradeNo, tc.expectedPaymentProvider, tc.targetStatus)
			require.ErrorIs(t, err, ErrPaymentMethodMismatch)
			assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, tc.tradeNo))
		})
	}
}

func TestCompleteSubscriptionOrder_RejectsMismatchedPaymentProvider(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 202, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 301)
	insertSubscriptionOrderForPaymentGuardTest(t, "sub-guard-order", 202, plan.Id, PaymentProviderStripe)

	err := CompleteSubscriptionOrder("sub-guard-order", `{"provider":"epay"}`, PaymentProviderEpay, "alipay")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	order := GetSubscriptionOrderByTradeNo("sub-guard-order")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Zero(t, countUserSubscriptionsForPaymentGuardTest(t, 202))

	topUp := GetTopUpByTradeNo("sub-guard-order")
	assert.Nil(t, topUp)
}

func TestExpireSubscriptionOrder_RejectsMismatchedPaymentProvider(t *testing.T) {
	truncateTables(t)

	insertUserForPaymentGuardTest(t, 303, 0)
	plan := insertSubscriptionPlanForPaymentGuardTest(t, 401)
	insertSubscriptionOrderForPaymentGuardTest(t, "sub-expire-guard", 303, plan.Id, PaymentProviderStripe)

	err := ExpireSubscriptionOrder("sub-expire-guard", PaymentProviderCreem)
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	order := GetSubscriptionOrderByTradeNo("sub-expire-guard")
	require.NotNil(t, order)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
}

func createEpayTestOrder(t *testing.T, userId int, tradeNo string, provider string, status string) TopUp {
	t.Helper()
	topUp := TopUp{
		UserId:          userId,
		Amount:          2,
		Money:           10.0,
		TradeNo:         tradeNo,
		PaymentMethod:   "alipay",
		PaymentProvider: provider,
		CreateTime:      common.GetTimestamp(),
		Status:          status,
	}
	require.NoError(t, DB.Create(&topUp).Error)
	return topUp
}

func TestRechargeEpayCreditsQuotaExactlyOnce(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 501, 0)
	order := createEpayTestOrder(t, user.Id, "EPAYTESTONCE", PaymentProviderEpay, common.TopUpStatusPending)

	alreadyDone, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.Equal(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))

	reloaded := GetTopUpByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	assert.Equal(t, common.TopUpStatusSuccess, reloaded.Status)
	assert.NotZero(t, reloaded.CompleteTime)

	alreadyDone, err = RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, alreadyDone)
	assert.Equal(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))
}

func TestRechargeEpayKeepsRedisAndDatabaseCreditInSync(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 5
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 502, 7)
	require.NoError(t, populateUserCache(*user))
	order := createEpayTestOrder(t, user.Id, "EPAYTESTREDISSYNC", PaymentProviderEpay, common.TopUpStatusPending)

	alreadyDone, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.Equal(t, 17, getUserQuotaForPaymentGuardTest(t, user.Id))
	cached, err := cacheGetUserBase(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 17, cached.Quota)

	alreadyDone, err = RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, alreadyDone)
	cached, err = cacheGetUserBase(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 17, cached.Quota)
}

func TestRechargeEpayUpdatesPaymentMethodToActual(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 503, 0)
	order := createEpayTestOrder(t, user.Id, "EPAYTESTMETHOD", PaymentProviderEpay, common.TopUpStatusPending)

	alreadyDone, err := RechargeEpay(order.TradeNo, "wxpay", "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)

	reloaded := GetTopUpByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	assert.Equal(t, "wxpay", reloaded.PaymentMethod)
	assert.Equal(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))
}

func TestRechargeEpayRejectsForeignAndNonPendingOrders(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 504, 7)

	t.Run("order from another payment provider", func(t *testing.T) {
		order := createEpayTestOrder(t, user.Id, "EPAYTESTSTRIPE", PaymentProviderStripe, common.TopUpStatusPending)
		_, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
		assert.ErrorIs(t, err, ErrPaymentMethodMismatch)
		assert.Equal(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
	})

	t.Run("order that is not pending", func(t *testing.T) {
		order := createEpayTestOrder(t, user.Id, "EPAYTESTEXPIRED", PaymentProviderEpay, common.TopUpStatusExpired)
		_, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
		assert.ErrorIs(t, err, ErrTopUpStatusInvalid)
		assert.Equal(t, 7, getUserQuotaForPaymentGuardTest(t, user.Id))
	})

	t.Run("missing order", func(t *testing.T) {
		_, err := RechargeEpay("EPAYTESTMISSING", "alipay", "127.0.0.1")
		assert.ErrorIs(t, err, ErrTopUpNotFound)
	})
}

func TestRechargeEpayRejectsQuotaOverflowBeforeCompletingOrder(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = float64(common.MaxWalletQuota + 1)
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 505, 3)
	order := createEpayTestOrder(t, user.Id, "EPAYTESTOVERFLOW", PaymentProviderEpay, common.TopUpStatusPending)

	_, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
	require.Error(t, err)
	assert.Equal(t, 3, getUserQuotaForPaymentGuardTest(t, user.Id))
	assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
}

func TestRechargeEpayEnforcesFinalWalletQuotaLimit(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	testCases := []struct {
		name         string
		currentQuota int
		wantErr      bool
		wantQuota    int
		wantStatus   string
	}{
		{
			name:         "allows exact highest representable wallet balance",
			currentQuota: common.MaxWalletQuota - 1_000_000,
			wantQuota:    common.MaxWalletQuota,
			wantStatus:   common.TopUpStatusSuccess,
		},
		{
			name:         "rejects balance above wallet quota domain",
			currentQuota: common.MaxWalletQuota - 999_999,
			wantErr:      true,
			wantQuota:    common.MaxWalletQuota - 999_999,
			wantStatus:   common.TopUpStatusPending,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			user := insertUserForPaymentGuardTest(t, 506, tc.currentQuota)
			order := createEpayTestOrder(t, user.Id, "EPAYTESTWALLETLIMIT", PaymentProviderEpay, common.TopUpStatusPending)

			_, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
			if tc.wantErr {
				require.ErrorIs(t, err, ErrTopUpQuotaLimitExceeded)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantQuota, getUserQuotaForPaymentGuardTest(t, user.Id))
			assert.Equal(t, tc.wantStatus, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
		})
	}
}

// 新易支付单按下单冻结值到账，不能再把人民币输入解释为美元数量。
// 回调与人工补单必须一致；真实历史零冻结值订单保留原美元数量契约。
// 三种数据库执行真实订单锁、余额写入和幂等判断，不用 mock 自证公式。
func TestEpayFrozenQuotaDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			setupEpayFrozenQuotaDatabase(t, dialect)     // 切换句柄后只调用真实模型公共入口。
			for _, manual := range []bool{false, true} { // 两种到账入口不能各保留一份不同的换算规则。
				for _, frozen := range []int{68493, 0, -1, common.MaxWalletQuota} { // 1元冻结值、真实旧单、坏数据和钱包溢出。
					name := fmt.Sprintf("manual=%v/frozen=%d", manual, frozen)
					t.Run(name, func(t *testing.T) { checkEpayFrozenQuota(t, manual, frozen) })
				}
			}
		})
	}
}

// 每个引擎使用专用前缀与日志表，不能向用户现有数据库写入测试记录。
// 外部数据库未配置时明确跳过，主流程验收会提供隔离测试实例。
// 模型包全局句柄只能串行替换，测试结束后恢复原方言和句柄。
func setupEpayFrozenQuotaDatabase(t *testing.T, dialect string) {
	t.Helper()
	var driver gorm.Dialector // 保留三种真实驱动，不用 SQLite 代替行锁数据库。
	switch dialect {
	case "sqlite":
		driver = sqlite.Open(filepath.Join(t.TempDir(), "epay.db")) // 临时文件在结束后自动清理。
	case "mysql":
		if os.Getenv("TEST_MYSQL_DSN") == "" { // 缺库不能伪装成已经完成该方言验证。
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		driver = mysql.Open(os.Getenv("TEST_MYSQL_DSN"))
	case "postgres":
		if os.Getenv("TEST_POSTGRES_DSN") == "" { // 明确告知外部数据库未运行，不能默默替换驱动。
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		driver = postgres.Open(os.Getenv("TEST_POSTGRES_DSN"))
	}
	db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "epay_frozen_"}})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)    // 本测试验证串行到账，连接数固定以免测试资源互相争抢。
	oldDB, oldLog := DB, LOG_DB // 日志与主库同时隔离，真实日志也不得泄漏到业务库。
	oldMain, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	DB, LOG_DB = db, db // 支付后的日志也写入当前隔离前缀表。
	common.SetDatabaseTypes(common.DatabaseType(dialect), common.DatabaseType(dialect))
	initCol()                                                        // 方言切换后列名转义必须同步，尤其 PostgreSQL 保留字。
	models := []any{&User{}, &TopUp{}, &SubscriptionOrder{}, &Log{}} // 只建立本次回调与补单实际使用的业务表。
	t.Cleanup(func() {
		assert.NoError(t, db.Migrator().DropTable(models...)) // 只删除本次专用前缀表。
		DB, LOG_DB = oldDB, oldLog                            // 在关闭连接前恢复其他测试的模型环境。
		common.SetDatabaseTypes(oldMain, oldLogType)
		initCol()
		assert.NoError(t, sqlDB.Close()) // 连接关闭后主流程可安全回收临时容器。
	})
	require.NoError(t, db.AutoMigrate(models...))
	versionSQL := "SELECT version()"
	if dialect == "sqlite" { // SQLite 使用独立版本查询语法，版本来自真实引擎。
		versionSQL = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
	t.Logf("database version: %s", version) // 保留真实引擎证据，不输出包含凭据的 DSN。
}

// 相同金额的冻结值、历史零值和坏负值分别检验，不能默默回退坏订单。
// 故意在下单后修改展示汇率，到账仍必须遵从已经持久化的购买权益。
// 重复调用真实完成入口，不允许用重复加余额来通过支付回调重试。
func checkEpayFrozenQuota(t *testing.T, manual bool, frozen int) {
	t.Helper()
	oldQuotaPerUnit, oldRate := common.QuotaPerUnit, operation_setting.USDExchangeRate
	common.QuotaPerUnit = 500000 // 固定测试中的内部美元额度单位。
	operation_setting.USDExchangeRate = 7.3
	t.Cleanup(func() { common.QuotaPerUnit, operation_setting.USDExchangeRate = oldQuotaPerUnit, oldRate })
	name := fmt.Sprintf("epay-%v-%d", manual, frozen) // 订单与用户使用不同标识，避免场景互相复用旧状态。
	user := User{Username: name, AffCode: name, Quota: 7, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	order := TopUp{UserId: user.Id, Amount: 1, CreditedQuota: frozen, Money: 1, TradeNo: name, PaymentProvider: PaymentProviderEpay, PaymentMethod: "wxpay", Status: common.TopUpStatusPending}
	require.NoError(t, order.Insert())     // 冻结值必须先持久化，配置变化发生在订单创建之后。
	operation_setting.USDExchangeRate = 20 // 后来的配置不能追溯改变已付订单的额度。
	for range 2 {
		var err error
		if manual {
			err = ManualCompleteTopUp(order.TradeNo, "127.0.0.1", 1, "verified-epay-receipt", "已核实渠道收款")
		} else {
			_, err = RechargeEpay(order.TradeNo, "wxpay", "127.0.0.1")
		}
		if frozen == common.MaxWalletQuota {
			require.ErrorIs(t, err, ErrTopUpQuotaLimitExceeded) // 冻结值有效也不得突破用户钱包上限。
		} else if frozen < 0 {
			require.ErrorIs(t, err, ErrInvalidTopUpQuota) // 坏冻结值不可当作历史无冻结订单。
		} else {
			require.NoError(t, err) // 第二次完成必须幂等，而非再次增加充值额度。
		}
	}
	wantQuota := 7 // 非法订单的余额和状态都须原样保留。
	wantStatus := common.TopUpStatusPending
	if frozen >= 0 && frozen < common.MaxWalletQuota { // 只有合法且未超钱包上限的订单可以完成。
		wantStatus = common.TopUpStatusSuccess
		if frozen == 0 {
			wantQuota += 500000 // 旧Amount=1仍表示1美元，不能被新的人民币设置重新解释。
		} else {
			wantQuota += frozen // 新订单只兑现下单已保存的内部额度，与后续汇率无关。
		}
	}
	assert.Equal(t, wantQuota, getUserQuotaForPaymentGuardTest(t, user.Id))
	assert.Equal(t, wantStatus, getTopUpStatusForPaymentGuardTest(t, order.TradeNo))
}
