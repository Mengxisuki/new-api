package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// hubBillingDB 在三种真实数据库运行同一业务验收，不替换为mock事务。
// 表前缀只属于本测试，结束删除这些表并恢复进程内句柄。
// 外部连接从环境注入，未配置必须明确跳过而不伪称通过。
// SQLite使用临时文件，模拟真实提交与重新读取的边界。
// 日志库故意仍指向原夹具，事件不能依赖日志库是否相同。
// 单连接让嵌套事务死锁立即暴露，而非依靠连接池掩盖问题。
// 不删除数据库或其他前缀表，避免污染并行验证任务。
// 用户余额在新增事件迁移前创建，升级后必须保持原值。
// 默认暂停投递并不关闭事件捕获，测试保留这个默认状态。
// 所有数据库版本从实际连接查询，不把驱动版本当作引擎版本。
// 新表与原业务表共用一条数据库连接，真实验证事务归属。
// 清理恢复日志开关和批量设置，不能依赖测试执行顺序。
// 没有真实商户调用，支付通知仅使用模型已验证的业务入口。
// 后续正式鉴权测试仍由控制器层负责，本文件只验证资金契约。
// 清理顺序先停止捕获，防止其它测试误写已经删除的事件表。
// 旧数据库句柄在恢复后保持原连接池，不重建或关闭用户环境。
// 迁移输入直接使用生产模型，不复制表定义自证正确。
// 同一测试不启动工作器定时循环，检测由测试明确触发确保确定性。
// 所有故障断言使用可识别的错误值，不用任意字符串匹配假装根因。
// 测试库不启用Redis，专注关系库事务与基线的跨方言合同。
func hubBillingDB(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	var driver gorm.Dialector
	switch dialect {
	case "sqlite":
		driver = sqlite.Open(filepath.Join(t.TempDir(), "hub.db"))
	case "mysql":
		dsn := os.Getenv("TEST_MYSQL_DSN")
		if dsn == "" {
			t.Skip("TEST_MYSQL_DSN is not configured")
		}
		driver = mysql.Open(dsn)
	case "postgres":
		dsn := os.Getenv("TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("TEST_POSTGRES_DSN is not configured")
		}
		driver = postgres.Open(dsn)
	}
	db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "hub_billing_"}})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	oldDB, oldLog := model.DB, model.LOG_DB
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldRedis, oldBatch, oldCapture, oldLogEnabled := common.RedisEnabled, common.BatchUpdateEnabled, model.HubCaptureEnabled(), common.LogConsumeEnabled
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseType(dialect), oldLogType)
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, false
	models := []any{&model.User{}, &model.Token{}, &model.Task{}, &model.Channel{}, &model.TopUp{}, &model.UserSubscription{}, &model.AccountRechargeOrder{}, &model.AccountRechargeEvent{}, &model.HubSettings{}, &model.HubEvent{}, &model.HubConsumer{}, &model.HubToken{}, &model.HubDelivery{}, &model.HubConnection{}, &model.HubChargeObservation{}}
	t.Cleanup(func() {
		model.SetHubCaptureEnabled(false)
		assert.NoError(t, db.Migrator().DropTable(models...))
		model.DB, model.LOG_DB = oldDB, oldLog
		common.SetDatabaseTypes(oldMainType, oldLogType)
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldRedis, oldBatch, oldLogEnabled
		model.SetHubCaptureEnabled(oldCapture)
		assert.NoError(t, sqlDB.Close())
	})
	// 先建立现有业务表，新增事件迁移不应改变旧订单金额或用户余额。
	// 连续两次迁移核对幂等，避免用新建表掩盖升级破坏。
	require.NoError(t, db.AutoMigrate(models[:8]...))
	require.NoError(t, db.Create(&model.User{Id: 9201, Username: "hub-billing-user", Status: common.UserStatusEnabled, Quota: 10000}).Error)
	require.NoError(t, model.MigrateEventHub(db))
	require.NoError(t, model.MigrateEventHub(db))
	model.SetHubCaptureEnabled(true)
	var user model.User
	require.NoError(t, db.First(&user, 9201).Error)
	assert.Equal(t, 10000, user.Quota)
	settings, err := model.GetHubSettings()
	require.NoError(t, err)
	settings.AnomalyMinQuota, settings.AnomalyColdQuota = 200, 1000
	settings.AnomalyMinSamples, settings.AnomalyMultiplier = 2, 3
	require.NoError(t, model.UpdateHubSettings(settings))
	var version string
	query := "SELECT version()"
	if dialect == "sqlite" {
		query = "SELECT sqlite_version()"
	}
	require.NoError(t, db.Raw(query).Scan(&version).Error)
	t.Logf("event billing database: %s %s", dialect, version)
	return db
}

// TestHubBillingAtomicAndAnomaly 验证关闭日志仍有业务事件和最终费用样本。
// 当前样本排除自身；不同模型与来源不混入基线。
// 同步零差额、任务补扣、退款与幂等都走真实公共实现。
// 故障注入只破坏观察写入，断言真实资金和订单回滚。
// 服务暂停默认关闭，但捕获与检测仍需继续运行。
// 三种数据库使用相同断言，不能把仅SQLite成功解释为三库兼容。
// 中位数阈值使用可人工检查的固定值，不复制生产计算函数。
// 费用断言最终余额，不能只检查观察记录存在就声称资金正确。
// 重复结算覆盖同会话重投，生产唯一约束另保护跨实例认领。
// 显式关闭消费日志，避免将日志开启当成事件产生的隐含前提。
// 日志数据库与业务数据库分离，防止意外依赖同库事务成功。
// 先处理低额记录再处理高额记录，让基线代表过去真实消费。
// 新模型六百不会告警，用于区分冷启动与已有历史两个规则。
// 任务退款验证整笔费用归零，而不是仅新增一笔退款日志。
// 资料补交按订单版本提交，旧版本不能替代真实状态迁移。
// 本测试数据不含实际用户资料，secret字符串只是合成密文标记。
// 三库测试只读取外部环境提供的隔离测试库，凭据不写入源码。
func TestHubBillingAtomicAndAnomaly(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := hubBillingDB(t, dialect)
			ctx := context.Background()
			// 每笔收费独立请求身份，同一模型建立两笔一百额度的历史。
			// 第三笔六百触发相对阈值三百，不能把自身加入后抬高基线。
			for _, tc := range []struct {
				key   string
				quota int
			}{{"small-a", 100}, {"small-b", 100}, {"large", 600}} {
				// 本组没有预扣，最终结算一次提交完整费用与观察。
				// 重复调用应由已完成状态保护，不能再次执行钱包扣减。
				info := &relaycommon.RelayInfo{UserId: 9201, RequestId: tc.key, OriginModelName: "model-a", BillingSource: BillingSourceWallet, IsPlayground: true}
				session := &BillingSession{relayInfo: info, funding: &WalletFunding{userId: 9201}}
				require.NoError(t, session.Settle(tc.quota))
				require.NoError(t, session.Settle(tc.quota))
				require.NoError(t, model.ProcessHubChargeObservations(ctx))
			}
			// 检查公共事件类型和载荷，验证消费者真正能看到可解释的告警。
			// sample_count与阈值不以数据库记录数量代替业务判断。
			// 相同会话二次调用不能让钱包或事件增加第二份。
			var events []model.HubEvent
			require.NoError(t, db.Where("topic = ?", model.HubTopicCharge).Find(&events).Error)
			require.Len(t, events, 1)
			var payload map[string]any
			require.NoError(t, common.UnmarshalJsonStr(events[0].Payload, &payload))
			assert.Equal(t, float64(600), payload["actual_quota"])
			assert.Equal(t, float64(100), payload["baseline_quota"])
			assert.Equal(t, "historical_multiple", payload["reason"])
			// 告警主体名称随真实结算生成，不能取模型 Key 或管理员名称。
			assert.Equal(t, "hub-billing-user", payload["user_name"])
			// 资金查询使用数据库原值，不经过可能延迟的展示缓存。
			// 两笔一百和一笔六百的总费用应当是八百额度。
			var user model.User
			require.NoError(t, db.First(&user, 9201).Error)
			assert.Equal(t, 9200, user.Quota)
			// 相同费用与预扣匹配也保存最终事实，不允许早退漏样本。
			// 测试预扣已经真实写入，资金仍应只扣完整一笔。
			require.NoError(t, db.Model(&user).Update("quota", gorm.Expr("quota - ?", 80)).Error)
			info := &relaycommon.RelayInfo{UserId: 9201, RequestId: "exact", OriginModelName: "model-a", BillingSource: BillingSourceWallet, IsPlayground: true}
			session := &BillingSession{relayInfo: info, funding: &WalletFunding{userId: 9201}, preConsumedQuota: 80}
			require.NoError(t, session.Settle(80))
			var exact model.HubChargeObservation
			require.NoError(t, db.Where("request_key = ?", "request:exact").First(&exact).Error)
			assert.Equal(t, 80, exact.ActualQuota)
			// 高额新模型没有历史时走独立阈值，不能借另一个模型的便宜样本报警。
			// 新模型的冷启动金额低于一千，不应复用model-a的低价历史。
			// 该路径仍真实扣款，只是不满足告警条件。
			_, err := model.ApplyHubChargeSettlement(model.HubChargeObservation{RequestKey: "new-model", UserID: 9201, ModelName: "model-b", Funding: BillingSourceWallet, ActualQuota: 600, State: model.HubChargeFinal}, 600, 0, 0)
			require.NoError(t, err)
			require.NoError(t, model.ProcessHubChargeObservations(ctx))
			var count int64
			require.NoError(t, db.Model(&model.HubEvent{}).Where("topic = ?", model.HubTopicCharge).Count(&count).Error)
			assert.EqualValues(t, 1, count)
			// 观察写失败必须回滚已执行资金更新，而非只丢一条通知日志。
			// 针对观察Create回调注入真实失败，资金UPDATE已经发生在同事务内。
			// SQL回滚后余额不得残留七百额度的半笔扣款。
			// 故障移除后继续验证其它事件，避免测试在注入点就结束。
			injected := errors.New("observation write unavailable")
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register("hub-billing-fail", func(tx *gorm.DB) {
				if tx.Statement.Table == "hub_billing_hub_charge_observations" {
					tx.AddError(injected)
				}
			}))
			_, err = model.ApplyHubChargeSettlement(model.HubChargeObservation{RequestKey: "rollback", UserID: 9201, ModelName: "model-a", Funding: BillingSourceWallet, ActualQuota: 700, State: model.HubChargeFinal}, 700, 0, 0)
			require.ErrorIs(t, err, injected)
			require.NoError(t, db.Callback().Create().Remove("hub-billing-fail"))
			require.NoError(t, db.First(&user, 9201).Error)
			assert.Equal(t, 8520, user.Quota)
			// 充值成功由订单事务产生，重复真实支付通知只增加一次余额和事件。
			// 金额和冻结额度保留订单实际合同，不从日志内容反向解析。
			// 官方渠道、订单金额与冻结额度均明确，避免触发无关校验错误。
			// 两次相同完成调用代表真实回调重投，最终只生成一次到账事件。
			// 支付金额文本与内部到账额度不混为同一货币单位。
			order := model.TopUp{UserId: 9201, Money: 12, CreditedQuota: 120, TradeNo: "hub-topup", PaymentProvider: model.PaymentProviderWechat, PaymentMethod: "wechat-main", Status: common.TopUpStatusPending}
			require.NoError(t, db.Create(&order).Error)
			// 通知表故障必须回滚已保存的成功状态和刚增加的钱包余额。
			// 故障移除后仍用原订单重试，验证失败没有破坏后续幂等认领。
			// 检查订单与用户真实行，不能仅用事件数量推断资金已回滚。
			injectedEvent := errors.New("event write unavailable")
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register("hub-event-fail", func(tx *gorm.DB) {
				if tx.Statement.Table == "hub_billing_hub_events" {
					tx.AddError(injectedEvent)
				}
			}))
			err = model.CompleteOfficialTopUp(order.TradeNo, order.PaymentProvider, order.PaymentMethod, decimal.NewFromInt(12), "127.0.0.1")
			require.ErrorIs(t, err, injectedEvent)
			require.NoError(t, db.Callback().Create().Remove("hub-event-fail"))
			require.NoError(t, db.First(&order, order.Id).Error)
			assert.Equal(t, common.TopUpStatusPending, order.Status)
			require.NoError(t, db.First(&user, 9201).Error)
			assert.Equal(t, 8520, user.Quota)
			for range 2 {
				require.NoError(t, model.CompleteOfficialTopUp(order.TradeNo, order.PaymentProvider, order.PaymentMethod, decimal.NewFromInt(12), "127.0.0.1"))
			}
			require.NoError(t, db.Model(&model.HubEvent{}).Where("topic = ?", model.HubTopicTopup).Count(&count).Error)
			assert.EqualValues(t, 1, count)
			// 到账事件读取钱包所有者的账号名，与支付渠道字段独立。
			var topupEvent model.HubEvent
			require.NoError(t, db.Where("topic = ?", model.HubTopicTopup).First(&topupEvent).Error)
			var topupPayload map[string]any
			require.NoError(t, common.UnmarshalJsonStr(topupEvent.Payload, &topupPayload))
			// 充值载荷新增名称，原订单实付与额度字段仍由业务生产者决定。
			assert.Equal(t, "hub-billing-user", topupPayload["user_name"])
			// 资料事件的名称指买家，即使补交时已有业务员也不变。
			// 资料提交只传状态摘要，不能把 secret 或资料邮箱写入事件。
			// 第二次以新版本要求补交，通知仍保留同一处理人。
			// 初始状态必须为已付款且待资料，不绕过业务状态机。
			// 补交通过action_required恢复，明确保留此前领取的业务员。
			// 安全检查针对持久事件原文，不能仅检查DTO没有对应字段。
			recharge := model.AccountRechargeOrder{UserID: 9201, PaymentStatus: "paid", Status: "awaiting_info", Version: 1, TradeNo: "hub-recharge", RequestID: "request-info"}
			require.NoError(t, db.Create(&recharge).Error)
			// 资料事件故障也不能留下已提交资料却没有通知的半笔状态。
			// 同时核对版本与secret回滚，避免重试被旧版本拒绝。
			// 同一故障只隔离事件Create，不替换真实订单状态机实现。
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register("hub-event-fail", func(tx *gorm.DB) {
				if tx.Statement.Table == "hub_billing_hub_events" {
					tx.AddError(injectedEvent)
				}
			}))
			require.ErrorIs(t, model.SubmitAccountRechargeInfo(recharge.ID, 9201, 1, "failed-secret", "o***@example.com"), injectedEvent)
			require.NoError(t, db.Callback().Create().Remove("hub-event-fail"))
			require.NoError(t, db.First(&recharge, recharge.ID).Error)
			assert.Equal(t, "awaiting_info", recharge.Status)
			assert.Equal(t, 1, recharge.Version)
			assert.Empty(t, recharge.Secret)
			require.NoError(t, model.SubmitAccountRechargeInfo(recharge.ID, 9201, 1, "private-secret", "o***@example.com"))
			require.NoError(t, db.Model(&recharge).Updates(map[string]any{"status": "action_required", "assignee_id": 7}).Error)
			require.NoError(t, model.SubmitAccountRechargeInfo(recharge.ID, 9201, 2, "replacement-secret", "o***@example.com"))
			var infoEvents []model.HubEvent
			require.NoError(t, db.Where("topic = ?", model.HubTopicRecharge).Order("id ASC").Find(&infoEvents).Error)
			require.Len(t, infoEvents, 2)
			assert.Contains(t, infoEvents[0].Payload, `"submission":"initial"`)
			assert.Contains(t, infoEvents[1].Payload, `"submission":"resubmitted"`)
			for _, event := range infoEvents {
				var infoPayload map[string]any
				require.NoError(t, common.UnmarshalJsonStr(event.Payload, &infoPayload))
				assert.Equal(t, "hub-billing-user", infoPayload["user_name"])
			}
			assert.NotContains(t, infoEvents[1].Payload, "secret")
			assert.NotContains(t, infoEvents[1].Payload, "example.com")
			// 任务以最终总额比较，预扣五十后补到三百只产生一个样本。
			// 第二次同结果收尾不会再次扣费；失败退款将本样本移出基线。
			// 任务表先有五十额度费用，最终事务只补扣二百五十。
			// 同一最终总额调用两次，Key预算与费用统计都必须幂等。
			// 后续退款按最终三百退回，观察状态必须变成refunded。
			// 检查数据库持久观察，避免只改变内存task值而误称完成。
			task := &model.Task{TaskID: "hub-task", UserId: 9201, Quota: 50, Status: model.TaskStatusSuccess, Properties: model.Properties{OriginModelName: "model-task"}, PrivateData: model.TaskPrivateData{BillingSource: BillingSourceWallet}}
			require.NoError(t, db.Create(task).Error)
			RecalculateTaskQuota(ctx, task, 300, common.NewMessage("task final quota"))
			RecalculateTaskQuota(ctx, task, 300, common.NewMessage("task final quota"))
			var taskCharge model.HubChargeObservation
			require.NoError(t, db.Where("request_key = ?", "task:hub-task").First(&taskCharge).Error)
			assert.Equal(t, 300, taskCharge.ActualQuota)
			assert.Equal(t, model.HubChargeFinal, taskCharge.State)
			assert.True(t, RefundTaskQuota(ctx, task, "task failed"))
			assert.True(t, RefundTaskQuota(ctx, task, "task failed"))
			require.NoError(t, db.Where("request_key = ?", "task:hub-task").First(&taskCharge).Error)
			assert.Zero(t, taskCharge.ActualQuota)
			assert.Equal(t, model.HubChargeRefunded, taskCharge.State)
			// 没有产生差额的任务完成也落最终观察，不能只覆盖补扣分支。
			// 使用另一公开任务编号，避免复用上一笔已退款的计费身份。
			// 查询对象清零防止GORM自动带上上一条记录的主键。
			// 该例保护此前return前没有记录观察的零差额分支。
			task = &model.Task{TaskID: "hub-exact-task", UserId: 9201, Quota: 80, Status: model.TaskStatusSuccess, Properties: model.Properties{OriginModelName: "model-task"}, PrivateData: model.TaskPrivateData{BillingSource: BillingSourceWallet}}
			require.NoError(t, db.Create(task).Error)
			RecalculateTaskQuota(ctx, task, 80, common.NewMessage("task exact quota"))
			taskCharge = model.HubChargeObservation{}
			require.NoError(t, db.Where("request_key = ?", "task:hub-exact-task").First(&taskCharge).Error)
			assert.Equal(t, 80, taskCharge.ActualQuota)
			// 新会话重投同一个请求身份，资金幂等后Key预算也不得再扣一次。
			// 这覆盖进程内settled布尔无法保护的跨会话边界。
			// Token数据库最终余额是公共行为证据，不检查私有会话字段。
			// 令牌原文仅为测试合成值，不需要外部模型或真实用户凭据。
			// 第一次结算扣五十，第二个会话应识别已完成观察并整体跳过。
			// 观察唯一键的保护范围包含资金与Key预算，不能只做到资金不重扣。
			// 同时保留原令牌累计已用量，防止剩余额度与已用量不一致。
			token := model.Token{Id: 9411, UserId: 9201, Key: "hub-billing-test-key", RemainQuota: 1000}
			require.NoError(t, db.Create(&token).Error)
			for range 2 {
				info := &relaycommon.RelayInfo{UserId: 9201, TokenId: token.Id, TokenKey: token.Key, RequestId: "duplicate-session", OriginModelName: "model-a", BillingSource: BillingSourceWallet}
				session := &BillingSession{relayInfo: info, funding: &WalletFunding{userId: 9201}}
				require.NoError(t, session.Settle(50))
			}
			require.NoError(t, db.First(&token, token.Id).Error)
			assert.Equal(t, 950, token.RemainQuota)
			assert.Equal(t, 50, token.UsedQuota)
			// 订阅费用使用同一观察协议，资金只能进入原订阅的已用额度。
			// 钱包余额即使充足也不允许通知逻辑擅自选择另一个资金来源。
			// 订阅原有上限是权威，观察失败不能绕开它或者留下半笔快照。
			// 零费用不参与基线，因此订阅退款和钱包退款均有明确排除规则。
			sub := model.UserSubscription{Id: 9401, UserId: 9201, AmountTotal: 1000, AmountUsed: 100}
			require.NoError(t, db.Create(&sub).Error)
			subInfo := &relaycommon.RelayInfo{UserId: 9201, RequestId: "subscription-final", OriginModelName: "model-a", BillingSource: BillingSourceSubscription, SubscriptionId: sub.Id, IsPlayground: true}
			subSession := &BillingSession{relayInfo: subInfo, funding: &SubscriptionFunding{subscriptionId: sub.Id}, preConsumedQuota: 100}
			require.NoError(t, subSession.Settle(200))
			require.NoError(t, db.First(&sub, sub.Id).Error)
			assert.EqualValues(t, 200, sub.AmountUsed)
			_, err = model.ApplyHubChargeSettlement(model.HubChargeObservation{RequestKey: "subscription-too-large", UserID: 9201, ModelName: "model-a", Funding: BillingSourceSubscription, ActualQuota: 2000, State: model.HubChargeFinal}, 2000, sub.Id, 0)
			require.ErrorIs(t, err, model.ErrSubscriptionLimitExceeded)
			require.NoError(t, db.Model(&model.HubChargeObservation{}).Where("request_key = ?", "subscription-too-large").Count(&count).Error)
			assert.Zero(t, count)
			// 无会话旧入口仍须维护订阅日志差额，不能只更改数据库额度。
			// 零总额代表退还预扣一百，重复调用不得再次退还或累加上下文。
			// 通过公共SettleBilling入口验证，不直接调用私有观察构造函数。
			// 不发起真实提醒或网络请求，零总额按原合同跳过余额通知。
			fallback := &relaycommon.RelayInfo{UserId: 9201, RequestId: "subscription-fallback", OriginModelName: "model-a", BillingSource: BillingSourceSubscription, SubscriptionId: sub.Id, FinalPreConsumedQuota: 100, IsPlayground: true}
			require.NoError(t, SettleBilling(nil, fallback, 0))
			require.NoError(t, SettleBilling(nil, fallback, 0))
			require.NoError(t, db.First(&sub, sub.Id).Error)
			assert.EqualValues(t, 100, sub.AmountUsed)
			assert.EqualValues(t, -100, fallback.SubscriptionPostDelta)
			// 低样本时独立高额阈值的载荷明确标记absolute_limit。
			// 第一笔新模型六百未告警，第二笔一千五百仍不足两份历史样本。
			// 第二次处理工作器不可重复产生同一异常事件。
			_, err = model.ApplyHubChargeSettlement(model.HubChargeObservation{RequestKey: "cold-large", UserID: 9201, ModelName: "model-b", Funding: BillingSourceWallet, ActualQuota: 1500, State: model.HubChargeFinal}, 1500, 0, 0)
			require.NoError(t, err)
			require.NoError(t, model.ProcessHubChargeObservations(ctx))
			require.NoError(t, model.ProcessHubChargeObservations(ctx))
			var coldEvents []model.HubEvent
			require.NoError(t, db.Where("topic = ?", model.HubTopicCharge).Find(&coldEvents).Error)
			require.Len(t, coldEvents, 2)
			assert.Contains(t, coldEvents[1].Payload, `"reason":"absolute_limit"`)
		})
	}
}
