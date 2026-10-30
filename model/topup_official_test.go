package model

import (
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 真实创建用户和官方充值单，再通过公开完成函数入账。
// 冻结额度故意不同于Amount按默认单位换算的结果。
// 以用户余额验证到账来源确为冻结额度，避免静默重算。
// 同一成功通知连续提交两次，最终只能增加一次余额。
// 生产数据库事务、订单状态和钱包更新均保持真实实现。
// 测试不模拟creditTopUpQuota或订单查询，能发现接线遗漏。
// 金额使用decimal固定值，与网关分精度一致。
// 第二次通知仍应成功确认，避免上游无休止重试。
// 幂等不能只通过返回值证明，还必须检查实际用户余额。
// 订单最终状态必须为成功，不能只是增额后仍待付款。
// 测试结束由现有fixture清理订单、日志和用户记录。
// 本用例不依赖微信网络，也不会产生真实资金流。
func TestCompleteOfficialTopUpIsIdempotent(t *testing.T) {
	truncateTables(t)
	insertUserForPaymentGuardTest(t, 701, 0)

	topUp := &TopUp{
		UserId:          701,
		Amount:          2,
		CreditedQuota:   714286,
		Money:           9.99,
		TradeNo:         "official-alipay-idempotent",
		PaymentMethod:   "alipay-main",
		PaymentProvider: PaymentProviderAlipay,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())

	paidAmount := decimal.RequireFromString("9.99") // 明确分精度，避免依赖二进制浮点计算期望。
	require.NoError(t, CompleteOfficialTopUp(
		topUp.TradeNo,
		PaymentProviderAlipay,
		topUp.PaymentMethod,
		paidAmount,
		"127.0.0.1",
	))
	require.NoError(t, CompleteOfficialTopUp(
		topUp.TradeNo,
		PaymentProviderAlipay,
		topUp.PaymentMethod,
		paidAmount,
		"127.0.0.1",
	))

	assert.Equal(t, topUp.CreditedQuota, getUserQuotaForPaymentGuardTest(t, 701))
	assert.Equal(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, topUp.TradeNo))
}

// 官方支付首次进入oak，没有需要猜测权益的历史官方订单。
// 冻结额度缺失代表本地数据不完整，必须可见失败。
// Amount和付款金额均正常，避免把其他校验失败误当成功覆盖。
// 调用公开结算函数，验证生产路径不会按当前单位补算额度。
// 用户余额保持零，不能因默认汇率或舍入获得意外赠送。
// 订单仍待付款，修复数据后可以让真实通知再次尝试。
// 错误使用共享额度校验哨兵，便于上层统一处理。
// 历史易支付行的零冻结额度由各自原结算链路处理。
// 本用例不要求给所有旧充值订单回填无根据的值。
// 测试明确防止将坏订单包装成兼容旧数据的隐藏回退。
func TestCompleteOfficialTopUpRejectsMissingFrozenQuota(t *testing.T) {
	truncateTables(t)
	insertUserForPaymentGuardTest(t, 703, 0)

	topUp := &TopUp{
		UserId:          703,
		Amount:          2,
		Money:           10,
		TradeNo:         "official-alipay-legacy",
		PaymentMethod:   "alipay-main",
		PaymentProvider: PaymentProviderAlipay,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())

	require.ErrorIs(t, CompleteOfficialTopUp(
		topUp.TradeNo,
		PaymentProviderAlipay,
		topUp.PaymentMethod,
		decimal.NewFromInt(10),
		"127.0.0.1",
	), ErrInvalidTopUpQuota)

	assert.Zero(t, getUserQuotaForPaymentGuardTest(t, 703))
	assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, topUp.TradeNo))
}

// 签名可信的通知仍不能绕过本地订单金额匹配。
// 订单金额9.99元，通知9.98元，只差一分也应失败。
// 网关和渠道保持一致，确保测试确实命中金额边界。
// 失败后用户余额保持零，避免先增额再检查金额。
// 订单状态保持待付款，不因错误通知不可逆地关闭订单。
// 测试走真实事务，使错误回滚行为具有证据。
// 比较对象为用户余额和订单状态，不断言内部SQL文本。
// 金额以decimal显式表示，避免float fixture引入无关误差。
// 后续正确通知仍应有机会到账，因此不将订单标为失败。
// 其他渠道不能通过恰好相同金额认领订单，矩阵另有独立断言。
func TestCompleteOfficialTopUpRejectsAmountMismatch(t *testing.T) {
	truncateTables(t)
	insertUserForPaymentGuardTest(t, 702, 0)

	topUp := &TopUp{
		UserId:          702,
		Amount:          2,
		Money:           9.99,
		TradeNo:         "official-wechat-amount-mismatch",
		PaymentMethod:   "wechat-main",
		PaymentProvider: PaymentProviderWechat,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())

	err := CompleteOfficialTopUp(
		topUp.TradeNo,
		PaymentProviderWechat,
		topUp.PaymentMethod,
		decimal.RequireFromString("9.98"),
		"127.0.0.1",
	)
	require.Error(t, err)
	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, 702))
	assert.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, topUp.TradeNo))
}

// 本地二维码窗口结束不代表上游没有在有效期内收款。
// 模拟创建时间已经超过窗口，但成功通知才刚到达。
// 通知已经过控制器验签，本模型函数只核对订单和实际付款信息。
// 用户支付成功后应获得冻结额度，不能因队列延迟丢单。
// 测试不使用sleep，直接设置明确历史创建时间。
// 成功到账后订单必须转成功，重复投递由幂等逻辑处理。
// 该边界曾在旧迁移代码中错误拒绝，已有失败到通过的复现。
// 过期继续支付仍应禁止，与接受迟到回调是两个不同约束。
// 本用例保护真实资金与权益一致性，不更改上游支付窗口。
// 操作仅发生测试数据库，没有重新请求微信或创建新付款单。
func TestCompleteOfficialTopUpAcceptsDelayedVerifiedCallback(t *testing.T) {
	truncateTables(t)
	insertUserForPaymentGuardTest(t, 705, 0)

	topUp := &TopUp{
		UserId:          705,
		Amount:          10,
		CreditedQuota:   714286,
		Money:           10,
		TradeNo:         "official-alipay-expired",
		PaymentMethod:   "alipay-main",
		PaymentProvider: PaymentProviderAlipay,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix() - OfficialTopUpTTLSeconds - 1,
	}
	require.NoError(t, topUp.Insert())

	err := CompleteOfficialTopUp(topUp.TradeNo, PaymentProviderAlipay, topUp.PaymentMethod, decimal.NewFromInt(10), "127.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, topUp.TradeNo))
	assert.Equal(t, topUp.CreditedQuota, getUserQuotaForPaymentGuardTest(t, 705))
}

// 继续支付创建新交易号，但不得改变用户原本购买的权益。
// 旧订单先模拟仍处于待付款窗口，允许模型执行替换事务。
// 新订单必须保留原用户、金额、冻结额度、渠道和厂商。
// 创建时间按本次重试更新，用于生成新的付款窗口。
// 原单转失败防止重复继续支付，新单仍待付款不能提前入账。
// 该模型函数只验证本地事务，控制器另行保证先关闭上游旧单。
// 测试通过公开替换方法观察返回对象和原单状态。
// 不以当前渠道价格重算新订单，保护管理员改价期间的重试。
// 上游关单失败时不应调用本函数，不能靠此事务替代官方确认。
// 明确断言到期时间以秒计算，避免前端倒计时单位不一致。
// 所有值来自显式原订单fixture，测试不复制生产组装逻辑。
// 该用例不修改系统支付设置，也不读取真实商户凭据。
func TestReplacePendingOfficialTopUpForRetryFreezesOrderValues(t *testing.T) {
	truncateTables(t)
	insertUserForPaymentGuardTest(t, 706, 0)

	topUp := &TopUp{
		UserId:          706,
		Amount:          20,
		CreditedQuota:   1428571,
		Money:           20,
		TradeNo:         "official-wechat-retry-old",
		PaymentMethod:   "wechat-main",
		PaymentProvider: PaymentProviderWechat,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())
	createTime := time.Now().Unix() // 重试只建立新付款窗口，不刷新用户余额。

	replacement, err := ReplacePendingOfficialTopUpForRetry(706, topUp.TradeNo, "official-wechat-retry-new", createTime)
	require.NoError(t, err)
	assert.Equal(t, common.TopUpStatusFailed, getTopUpStatusForPaymentGuardTest(t, topUp.TradeNo))
	assert.Equal(t, topUp.Amount, replacement.Amount)
	assert.Equal(t, topUp.CreditedQuota, replacement.CreditedQuota)
	assert.Equal(t, topUp.Money, replacement.Money)
	assert.Equal(t, topUp.PaymentMethod, replacement.PaymentMethod)
	assert.Equal(t, topUp.PaymentProvider, replacement.PaymentProvider)
	assert.Equal(t, createTime+OfficialTopUpTTLSeconds, replacement.ExpiresAt)
}

// 旧版充值表没有冻结额度列，升级不能重算已有订单或清空账单。
// 新增列默认零代表历史记录，后续官方订单则必须保存下单时额度。
// 该结构仅用于迁移验收，不会进入生产模型注册或创建兼容接口。
type officialTopUpBeforeQuota struct {
	Id              int `gorm:"primaryKey"`
	UserId          int
	Amount          int64
	Money           float64
	TradeNo         string `gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string `gorm:"type:varchar(50)"`
	PaymentProvider string `gorm:"type:varchar(50);default:''"`
	CreateTime      int64
	CompleteTime    int64
	Status          string
}

// 同一组真实行为覆盖三种数据库，避免仅靠SQLite推断迁移与锁兼容。
// fresh验证新安装；upgrade验证旧表数据保留及连续迁移的幂等性。
// 所有测试表使用专用前缀，不读取或修改部署中的支付配置。
// 迁移和到账必须在真实SQLite、MySQL、PostgreSQL上分别验证。
// 数据库方言不同，行锁和默认值行为不能用单一引擎代替。
// 每种方言分别执行新安装和旧表升级两条路径。
// 旧表升级保留已有易支付历史行，新增冻结额度默认为零。
// 两次AutoMigrate验证重复启动不会破坏已有订单。
// 测试命名包含方言与模式，失败日志能定位具体数据库路径。
// 外部连接未配置时显式跳过，验收运行会提供专用测试DSN。
// 不把跳过记为兼容性通过，最终以真实运行结果报告。
// 矩阵串行切换模型全局数据库句柄，避免并行污染其他测试。
func TestOfficialPaymentDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, mode := range []string{"fresh", "upgrade"} {
				t.Run(mode, func(t *testing.T) { verifyOfficialPaymentDatabase(t, dialect, mode) })
			}
		})
	}
}

// 数据库全局句柄只在单个串行场景内替换，结束后还原原测试库。
// 同时隔离日志表，使到账后的审计记录也走真实、独立的数据库。
// 外部数据库必须通过专用测试DSN提供，未配置会明确跳过。
// 测试库使用固定专用表前缀，与订阅续期矩阵的表隔离。
// 连接句柄和日志句柄一并替换，到账审计也写到测试范围。
// 运行结束恢复原数据库类型与列名设置，避免影响其他模型测试。
// 清理只删除本次模型表，不删除数据库、容器或其他前缀表。
// 查询真实引擎版本并记录，供三数据库兼容性验收追溯。
// 升级模式先创建缺少冻结额度列的旧TopUp结构。
// 旧历史订单选易支付成功单，升级不得改变其金额与状态。
// 新增列通过现有AutoMigrate生成，不执行方言特有ALTER语句。
// 第二次迁移在新官方订单创建后运行，检验已保存冻结额度保留。
// 错误厂商和错误渠道分别调用真实完成函数，余额不能提前变更。
// 正确通知调用两次，最终余额只等于一份冻结额度。
// 模型成功记录必须保留原冻结值，不能只断言HTTP响应成功。
// 测试使用真实订单、用户、日志模型，不以mock事务替代行锁行为。
// MySQL与PostgreSQL连接通过专用环境变量提供，不在源码写凭据。
// SQLite文件位于临时目录，结束自动清理，不触碰项目业务数据库。
// 测试数据库最大连接数受控，防止空闲连接残留影响后续场景。
// 模型使用的数据库时钟与当前句柄一致，不由测试复制生产时间公式。
// 迁移异常立即终止当前场景，不能继续以不完整结构测试结算。
// 只有完整行为断言通过，当前方言才算本次验证成功。
func verifyOfficialPaymentDatabase(t *testing.T, dialect, mode string) {
	t.Helper()
	var driver gorm.Dialector
	switch dialect {
	case "sqlite":
		driver = sqlite.Open(filepath.Join(t.TempDir(), "payment.db"))
	case "mysql":
		if os.Getenv("TEST_MYSQL_DSN") == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		driver = mysql.Open(os.Getenv("TEST_MYSQL_DSN"))
	case "postgres":
		if os.Getenv("TEST_POSTGRES_DSN") == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		driver = postgres.Open(os.Getenv("TEST_POSTGRES_DSN"))
	}
	db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "official_payment_"}})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1) // 此矩阵验证串行事务，不用连接并发制造随机失败。
	oldDB, oldLog := DB, LOG_DB
	oldMain, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	DB, LOG_DB = db, db // 生产到账后的审计记录也写入测试前缀表。
	common.SetDatabaseTypes(common.DatabaseType(dialect), common.DatabaseType(dialect))
	initCol()
	models := []any{&TopUp{}, &User{}, &Log{}} // 业务与审计表同时隔离。
	t.Cleanup(func() {
		assert.NoError(t, db.Migrator().DropTable(models...))
		DB, LOG_DB = oldDB, oldLog
		common.SetDatabaseTypes(oldMain, oldLogType)
		initCol()
		assert.NoError(t, sqlDB.Close())
	})
	var version string
	versionSQL := "SELECT version()"
	if dialect == "sqlite" {
		versionSQL = "SELECT sqlite_version()"
	}
	require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
	t.Logf("database version: %s, mode: %s", version, mode)
	// 升级路径的旧记录必须真实存在，不能只验证新表能创建。
	if mode == "upgrade" {
		legacy := officialTopUpBeforeQuota{UserId: 1, Amount: 2, Money: 9.99, TradeNo: "legacy-preserved", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusSuccess}
		table := db.Table("official_payment_top_ups")
		require.NoError(t, table.AutoMigrate(&officialTopUpBeforeQuota{}))
		require.NoError(t, table.Create(&legacy).Error)
	}
	// 连续两次启动迁移不能丢失数据，也不能重置已保存的冻结额度。
	require.NoError(t, db.AutoMigrate(models...))
	require.True(t, db.Migrator().HasColumn(&TopUp{}, "credited_quota")) // 正式ORM字段必须真正迁移。
	// 升级路径的旧记录必须真实存在，不能只验证新表能创建。
	if mode == "upgrade" {
		var legacy TopUp
		require.NoError(t, db.Where("trade_no = ?", "legacy-preserved").First(&legacy).Error)
		assert.EqualValues(t, 2, legacy.Amount)
		assert.Zero(t, legacy.CreditedQuota)
	}
	user := User{Username: "official-payment-matrix", Quota: 0, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	topUp := TopUp{UserId: user.Id, Amount: 2, CreditedQuota: 1234567, Money: 9.99, TradeNo: "official-wechat-matrix", PaymentMethod: "wechat-main", PaymentProvider: PaymentProviderWechat, CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending}
	require.NoError(t, db.Create(&topUp).Error)
	require.NoError(t, db.AutoMigrate(models...))
	// 错误网关或渠道不能认领订单，即便订单号和金额都正确。
	err = CompleteOfficialTopUp(topUp.TradeNo, PaymentProviderAlipay, topUp.PaymentMethod, decimal.RequireFromString("9.99"), "127.0.0.1")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)
	err = CompleteOfficialTopUp(topUp.TradeNo, PaymentProviderWechat, "other-channel", decimal.RequireFromString("9.99"), "127.0.0.1")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)
	// 同一通知重投不能重复发放，真实状态和余额一起验证。
	for range 2 {
		require.NoError(t, CompleteOfficialTopUp(topUp.TradeNo, PaymentProviderWechat, topUp.PaymentMethod, decimal.RequireFromString("9.99"), "127.0.0.1"))
	}
	// 冻结额度与Amount并非等价，使用公开到账余额核对真实入账实现。
	var credited User
	require.NoError(t, db.First(&credited, user.Id).Error)
	assert.Equal(t, topUp.CreditedQuota, credited.Quota)
	var completed TopUp
	require.NoError(t, db.Where("trade_no = ?", topUp.TradeNo).First(&completed).Error)
	assert.Equal(t, common.TopUpStatusSuccess, completed.Status)
	assert.Equal(t, topUp.CreditedQuota, completed.CreditedQuota) // 连续迁移和结算都不得覆盖冻结权益。
}
