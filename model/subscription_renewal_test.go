package model

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// 测试真实付款入口，而不是仅调用续期辅助函数。
// 三种数据库使用独立前缀，避免触碰其他模块或部署数据。
// 外部数据库未配置时保留明确跳过；本轮验收会实际提供两个 DSN。
func TestSubscriptionRenewalDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} { // 同一公共行为分别跑三种真实数据库，不能以一个方言代替。
		t.Run(dialect, func(t *testing.T) {
			setupSubscriptionRenewalDatabase(t, dialect) // 每个数据库作用域独立维护表和全局句柄。
			for _, scenario := range []string{"paid-renewal", "callback", "admin", "expired", "cancelled", "last-window", "other-plan", "late-callback", "official"} {
				t.Run(scenario, func(t *testing.T) { checkSubscriptionRenewal(t, scenario) })
			}
		})
	}
}

// 连接专用测试库并恢复模型包全局句柄，测试不得并行切换数据库。
// 预扣之外的付款路径也会读取数据库时间，因此预留额外连接。
// 日志库与业务库共同隔离，使真实付款日志不会写向其他数据库。
func setupSubscriptionRenewalDatabase(t *testing.T, dialect string) {
	t.Helper()
	var driver gorm.Dialector // 复用项目已有驱动，不引入测试专用数据库实现。
	switch dialect {
	case "sqlite": // 使用临时文件，让事务中的第二个时间查询连接共享同一数据库。
		driver = sqlite.Open(filepath.Join(t.TempDir(), "renewal.db"))
	case "mysql": // 连接由本轮隔离容器提供，不猜测开发者已有数据库。
		if os.Getenv("TEST_MYSQL_DSN") == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		driver = mysql.Open(os.Getenv("TEST_MYSQL_DSN"))
	case "postgres": // 保留行锁及真实事务隔离行为，不能改用 SQLite 模拟。
		if os.Getenv("TEST_POSTGRES_DSN") == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		driver = postgres.Open(os.Getenv("TEST_POSTGRES_DSN"))
	}
	db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "subscription_renewal_"}})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)                                                   // 事务内已有时间查询使用全局 DB，不能将连接池限制为一。
	oldDB, oldLog := DB, LOG_DB                                                // 保存原句柄，防止之后的模型测试误用本轮专用库。
	oldMain, oldLogType := common.MainDatabaseType(), common.LogDatabaseType() // 原方言也必须一并恢复。
	DB, LOG_DB = db, db                                                        // 付款日志走真实实现，但只写本轮测试前缀的表。
	common.SetDatabaseTypes(common.DatabaseType(dialect), common.DatabaseType(dialect))
	initCol() // 切换方言后重新生成保留字列名和布尔值表示。
	models := []any{&User{}, &SubscriptionPlan{}, &UserSubscription{}, &SubscriptionOrder{}, &TopUp{}, &Log{}}
	t.Cleanup(func() {
		assert.NoError(t, db.Migrator().DropTable(models...)) // 只清理本测试前缀，不操作已有业务表。
		DB, LOG_DB = oldDB, oldLog                            // 在关闭临时连接前恢复业务句柄。
		common.SetDatabaseTypes(oldMain, oldLogType)
		initCol()                        // 切换方言后重新生成保留字列名和布尔值表示。
		assert.NoError(t, sqlDB.Close()) // 释放测试连接后，临时容器可由主流程统一回收。
	})
	require.NoError(t, db.AutoMigrate(models...))
	versionSQL := "SELECT version()" // 输出实际引擎版本，验证矩阵不能只声称已经配置。
	if dialect == "sqlite" {
		versionSQL = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
	t.Logf("database version: %s", version) // 日志保留可复核的数据库版本证据，不输出 DSN。
}

// 每个场景建立独立用户及套餐，足额钱包也不能用于购买订阅。
// 满额消耗后再买不能变相获得新额度；限购为一仍应允许活跃续期。
// 套餐缓存跨测试复用，因此每个场景明确失效，避免串入旧数据库记录。
func seedSubscriptionRenewal(t *testing.T, scenario string) (User, SubscriptionPlan, UserSubscription) {
	t.Helper()
	user := User{Username: "renewal-" + scenario, AffCode: scenario, Quota: int(20 * common.QuotaPerUnit), Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(&user).Error)
	plan := SubscriptionPlan{Title: "renewal-" + scenario, PriceAmount: 1, DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true, TotalAmount: 1000, QuotaResetPeriod: SubscriptionResetCustom, QuotaResetCustomSeconds: 7 * 86400, MaxPurchasePerUser: 1}
	if scenario == "expired" || scenario == "cancelled" { // 新开通场景不混入历史实例限购条件。
		plan.MaxPurchasePerUser = 0
	}
	require.NoError(t, DB.Create(&plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id) // 自增 ID 在不同引擎会重复，必须清除旧缓存。
	t.Cleanup(func() { InvalidateSubscriptionPlanCache(plan.Id) })
	now := GetDBTimestamp() // 与真实购买链路使用相同数据库时钟，避免客户端时间影响断言。
	sub := UserSubscription{UserId: user.Id, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 1000, StartTime: now - 86400, EndTime: time.Unix(now, 0).AddDate(0, 1, 0).Unix(), Status: "active", LastResetTime: now - 86400, NextResetTime: now + 6*86400, AllowWalletOverflow: false}
	if scenario == "late-callback" {
		// 付款订单创建在订阅有效时，回调到达时它已经自然到期。
		sub.EndTime, sub.Status = now-1, "expired"
	}
	if scenario == "expired" {
		sub.EndTime = now - 1 // 状态尚未被后台任务扫描时，时间到期也必须视为失效。
	}
	if scenario == "cancelled" {
		sub.Status = "cancelled" // 即使有效期仍在未来，已取消权益也不能被当成续期。
	}
	if scenario == "last-window" {
		// 原订阅的剩余有效期不足一个周期，旧到期限制已将下次重置截为零。
		sub.EndTime, sub.NextResetTime = now+3600, 0 // 续期后要恢复原周期边界，而非从付款时刻重新起算。
	}
	require.NoError(t, DB.Create(&sub).Error)
	return user, plan, sub // 返回购买前快照，由调用方独立核对权益变化。
}

// 外部支付回调和管理员授权共享续期语义，用户购买不走钱包。
// 回调重复投递只延长一次；两个独立订单仍各自购买一份时长。
// 过期、取消和不同套餐不复用旧权益，沿用首次购买的行为。
func checkSubscriptionRenewal(t *testing.T, scenario string) {
	t.Helper()
	user, plan, before := seedSubscriptionRenewal(t, scenario)
	if scenario == "other-plan" {
		plan.Id, plan.Title = 0, "another plan" // 新套餐不能误延长旧套餐，否则权限会串用。
		require.NoError(t, DB.Create(&plan).Error)
		InvalidateSubscriptionPlanCache(plan.Id) // 自增 ID 在不同引擎会重复，必须清除旧缓存。
		t.Cleanup(func() { InvalidateSubscriptionPlanCache(plan.Id) })
	}
	if scenario == "admin" {
		// 管理员授权沿用原入口；用户购买只能通过外部付款回调生效。
		_, err := AdminBindSubscription(user.Id, plan.Id, "renewal")
		require.NoError(t, err)
	} else {
		payments := 1 // 普通购买只提供一笔已确认的外部付款。
		if scenario == "callback" {
			payments = 2 // 独立订单各买一个月，和重复投递同一订单明确区分。
		}
		for i := range payments {
			tradeNo := fmt.Sprintf("renewal-%d-%d", user.Id, i)
			order := SubscriptionOrder{UserId: user.Id, PlanId: plan.Id, Money: 1, TradeNo: tradeNo, PaymentProvider: PaymentProviderEpay, PaymentMethod: "wxpay", Status: common.TopUpStatusPending}
			if scenario == "late-callback" {
				order.CreateTime = before.EndTime - 1 // 使用订单已保存的创建时间，不依赖等待跨秒。
			}
			if scenario == "official" {
				order.PaymentProvider, order.PaymentMethod = PaymentProviderWechat, "wechat-main" // 冻结到指定官方商户渠道。
			}
			require.NoError(t, order.Insert())
			// 待支付订单不提前延长时间，也不从余额中划扣购买金额。
			var unpaid UserSubscription // 先检查待支付状态，防止创建订单时就提前发放权益。
			require.NoError(t, DB.First(&unpaid, before.Id).Error)
			assert.Equal(t, time.Unix(before.EndTime, 0).AddDate(0, i, 0).Unix(), unpaid.EndTime)
			if scenario == "official" {
				// 官方入口必须同时匹配金额和商户渠道，错误通知不能发放权益。
				require.Error(t, CompleteOfficialSubscriptionOrder(tradeNo, "", PaymentProviderWechat, "other", decimal.NewFromInt(1)))
				require.Error(t, CompleteOfficialSubscriptionOrder(tradeNo, "", PaymentProviderWechat, "wechat-main", decimal.NewFromInt(2)))
				for range 2 {
					require.NoError(t, CompleteOfficialSubscriptionOrder(tradeNo, "", PaymentProviderWechat, "wechat-main", decimal.NewFromInt(1)))
				}
			} else {
				require.NoError(t, CompleteSubscriptionOrder(tradeNo, "", PaymentProviderEpay, "wxpay"))
				// 网关重复回调不得再次续期，订单自身是幂等边界。
				require.NoError(t, CompleteSubscriptionOrder(tradeNo, "", PaymentProviderEpay, "wxpay"))
			}
		}
	}
	var paidUser User // 用数据库读取余额，不依赖缓存或前端隐藏按钮证明未扣钱包。
	require.NoError(t, DB.First(&paidUser, user.Id).Error)
	// 无论账户余额是否足够，用户订阅付款都不会消耗钱包余额。
	assert.Equal(t, user.Quota, paidUser.Quota)
	var subs []UserSubscription // 读取全部实例，避免只检查到期时间而漏掉重复开通。
	require.NoError(t, DB.Where("user_id = ?", user.Id).Order("id").Find(&subs).Error)
	if scenario == "expired" || scenario == "cancelled" || scenario == "other-plan" || scenario == "late-callback" {
		require.Len(t, subs, 2)                          // 旧的失效或不同套餐记录保持原样，另建当前有效权益。
		assert.Equal(t, before.EndTime, subs[0].EndTime) // 不回写旧订单对应的订阅期限。
		assert.Zero(t, subs[1].AmountUsed)               // 新开通从空用量开始，避免继承历史周消耗。
		assert.Equal(t, plan.Id, subs[1].PlanId)
		return
	}
	require.Len(t, subs, 1) // 活跃同套餐只能延长现有实例，不能用多实例增加额度。
	after := subs[0]
	months := 1
	if scenario == "callback" {
		months = 2
	}
	assert.Equal(t, before.Id, after.Id) // 实例身份不变，已有密钥和账务引用可继续关联。
	assert.Equal(t, time.Unix(before.EndTime, 0).AddDate(0, months, 0).Unix(), after.EndTime)
	assert.Equal(t, before.StartTime, after.StartTime)                     // 起始时间是原周期锚点，续费不得重排。
	assert.Equal(t, before.AmountTotal, after.AmountTotal)                 // 追加时长不赠送一份并行周额度。
	assert.Equal(t, before.AmountUsed, after.AmountUsed)                   // 已耗尽的本周额度在续费后仍然耗尽。
	assert.Equal(t, before.LastResetTime, after.LastResetTime)             // 周期上界和用量归属不能因购买改变。
	assert.Equal(t, before.AllowWalletOverflow, after.AllowWalletOverflow) // 模型调用的余额策略与购买支付方式是两个独立概念。
	if scenario == "last-window" {
		assert.Equal(t, before.LastResetTime+7*86400, after.NextResetTime)
	} else {
		assert.Equal(t, before.NextResetTime, after.NextResetTime) // 正常续期保持页面已经公示的下次恢复时间。
	}
}

// 两笔独立支付都应成功入账，只串行延长同一实例。
// 真实 MySQL/PostgreSQL 验证行锁；SQLite 的单写者冲突不冒充行锁行为。
// 同时覆盖已有订阅续期及首次并发开通，后者不能重复建立权益。
func TestSubscriptionConcurrentPaymentDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"mysql", "postgres"} { // 两种行锁数据库分别验证，SQLite 冲突不能视为相同保证。
		t.Run(dialect, func(t *testing.T) {
			setupSubscriptionRenewalDatabase(t, dialect)  // 每个数据库作用域独立维护表和全局句柄。
			for _, initial := range []bool{false, true} { // 已有实例的累计续期与首次开通的去重都必须成立。
				t.Run(fmt.Sprintf("initial=%v", initial), func(t *testing.T) {
					user, plan, before := seedSubscriptionRenewal(t, fmt.Sprintf("concurrent-%v", initial))
					if initial {
						require.NoError(t, DB.Delete(&before).Error) // 只删除本场景种下的临时记录，模拟尚未开通过。
					}
					trades := []string{fmt.Sprintf("concurrent-%d-a", user.Id), fmt.Sprintf("concurrent-%d-b", user.Id)}
					for _, trade := range trades {
						require.NoError(t, (&SubscriptionOrder{UserId: user.Id, PlanId: plan.Id, Money: 1, TradeNo: trade, PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending}).Insert())
					}
					start, results := make(chan struct{}), make(chan error, len(trades))
					for _, trade := range trades {
						go func() {
							<-start
							results <- CompleteSubscriptionOrder(trade, "", PaymentProviderEpay, "wxpay") // 每个协程代表一笔独立付款通知。
						}()
					}
					close(start) // 使用同步屏障同时放行，不依赖 sleep 或机器执行速度。
					for range trades {
						require.NoError(t, <-results) // 不接受靠一笔付款失败来规避重复开通。
					}
					var subs []UserSubscription // 读取全部实例，避免只检查到期时间而漏掉重复开通。
					require.NoError(t, DB.Where("user_id = ?", user.Id).Find(&subs).Error)
					require.Len(t, subs, 1) // 活跃同套餐只能延长现有实例，不能用多实例增加额度。
					if initial {
						assert.Equal(t, time.Unix(subs[0].StartTime, 0).AddDate(0, 2, 0).Unix(), subs[0].EndTime)
						assert.Zero(t, subs[0].AmountUsed)
					} else {
						assert.Equal(t, time.Unix(before.EndTime, 0).AddDate(0, 2, 0).Unix(), subs[0].EndTime)
						assert.Equal(t, before.AmountUsed, subs[0].AmountUsed)
					}
				})
			}
		})
	}
}

// 旧库可能已保存允许余额购买的配置；移除功能不应破坏旧表其他字段。
// 该结构仅还原旧schema，生产中不保留可重新启用余额购的开关。
type subscriptionPlanBeforeExternalPayment struct {
	SubscriptionPlan
	AllowBalancePay *bool `gorm:"column:allow_balance_pay"` // 只在迁移测试使用历史列。
}

// 三库同时验证：旧列即使为true，更新模型也不暴力删除历史列或套餐数据。
// 真实余额购买路由已由router测试保护，不依赖旧列是否存在决定行为。
func TestSubscriptionExternalPaymentMigrationDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			setupSubscriptionRenewalDatabase(t, dialect)                     // 先检查当前模型能在空库正常创建。
			require.NoError(t, DB.Migrator().DropTable(&SubscriptionPlan{})) // 仅重建本测试前缀中的套餐表。
			table := DB.Table("subscription_renewal_subscription_plans")
			require.NoError(t, table.AutoMigrate(&subscriptionPlanBeforeExternalPayment{})) // 还原功能移除前的旧表。
			legacy := subscriptionPlanBeforeExternalPayment{
				SubscriptionPlan: SubscriptionPlan{Title: "legacy", DurationUnit: "month", DurationValue: 1, TotalAmount: 1500},
				AllowBalancePay:  common.GetPointer(true), // 主动覆盖旧站点曾经开启余额兑换的情形。
			}
			require.NoError(t, table.Create(&legacy).Error)
			for range 2 { // 重复启动不得删除历史套餐，也不因旧列造成迁移失败。
				require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}))
			}
			var migrated SubscriptionPlan
			require.NoError(t, DB.First(&migrated, legacy.Id).Error)
			assert.Equal(t, "legacy", migrated.Title) // 套餐名称与额度属于仍需保留的用户数据。
			assert.EqualValues(t, 1500, migrated.TotalAmount)
			var previousFlag bool
			require.NoError(t, table.Select("allow_balance_pay").Where("id = ?", legacy.Id).Scan(&previousFlag).Error)
			assert.True(t, previousFlag) // 保留旧列，不等于为用户留下可调用的购买后门。
		})
	}
}
