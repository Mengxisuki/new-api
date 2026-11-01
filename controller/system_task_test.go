package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSystemTaskListFiltersAndPaginationResponse(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}))
	require.NoError(t, db.Create(&[]model.SystemTask{
		{TaskID: "older", Type: model.SystemTaskTypeModelUpdate, Status: model.SystemTaskStatusFailed},
		{TaskID: "newer", Type: model.SystemTaskTypeModelUpdate, Status: model.SystemTaskStatusFailed},
		{TaskID: "other-status", Type: model.SystemTaskTypeModelUpdate, Status: model.SystemTaskStatusSucceeded},
		{TaskID: "other-type", Type: model.SystemTaskTypeChannelTest, Status: model.SystemTaskStatusFailed},
	}).Error)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/system-task/list?scope=history&type=model_update&status=failed&offset=1&limit=1", nil)
	ListSystemTasks(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool                       `json:"success"`
		Total   int64                      `json:"total"`
		Data    []model.SystemTaskResponse `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.EqualValues(t, 2, response.Total)
	require.Len(t, response.Data, 1)
	assert.Equal(t, "older", response.Data[0].TaskID)
}

func TestSystemTaskInvalidFiltersAreRejected(t *testing.T) {
	for _, tc := range []struct {
		query   string
		handler gin.HandlerFunc
	}{
		{"?scope=invalid", ListSystemTasks},
		{"?status=unknown", ListSystemTasks},
		{"?offset=-1", ListSystemTasks},
		{"?limit=invalid", ListSystemTasks},
		{"?status=unknown", DeleteSystemTaskHistory},
		{"?scope=invalid", DeleteSystemTaskHistory},
	} {
		t.Run(tc.query, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/"+tc.query, nil)
			tc.handler(c)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.JSONEq(t, `{"success":false,"message":"invalid system task filters"}`, recorder.Body.String())
		})
	}
}

// 升级夹具冻结本轮前的真实字段与索引，不能用新模型假装旧表。
type midjourneyBeforeResourcePolicy struct {
	Id          int    `json:"id"`
	Code        int    `json:"code"`
	UserId      int    `json:"user_id" gorm:"index"`
	Action      string `json:"action" gorm:"type:varchar(40);index"`
	MjId        string `json:"mj_id" gorm:"index"`
	Prompt      string `json:"prompt"`
	PromptEn    string `json:"prompt_en"`
	Description string `json:"description"`
	State       string `json:"state"`
	SubmitTime  int64  `json:"submit_time" gorm:"index"`
	StartTime   int64  `json:"start_time" gorm:"index"`
	FinishTime  int64  `json:"finish_time" gorm:"index"`
	ImageUrl    string `json:"image_url"`
	VideoUrl    string `json:"video_url"`
	VideoUrls   string `json:"video_urls"`
	Status      string `json:"status" gorm:"type:varchar(20);index"`
	Progress    string `json:"progress" gorm:"type:varchar(30);index"`
	FailReason  string `json:"fail_reason"`
	ChannelId   int    `json:"channel_id"`
	Quota       int    `json:"quota"`
	Buttons     string `json:"buttons"`
	Properties  string `json:"properties"`

	TokenId          int `json:"-" gorm:"default:0"`
	BillingChannelId int `json:"-" gorm:"default:0"`
}

func (midjourneyBeforeResourcePolicy) TableName() string { return "midjourneys" }

// 升级夹具冻结本轮前的真实字段与索引，不能用新模型假装旧表。
type subscriptionRecordBeforeCycle struct {
	Id                 int    `json:"id"`
	RequestId          string `json:"request_id" gorm:"type:varchar(64);uniqueIndex"`
	UserId             int    `json:"user_id" gorm:"index"`
	UserSubscriptionId int    `json:"user_subscription_id" gorm:"index"`
	PreConsumed        int64  `json:"pre_consumed" gorm:"type:bigint;not null;default:0"`
	Status             string `json:"status" gorm:"type:varchar(32);index"` // consumed/refunded
	CreatedAt          int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt          int64  `json:"updated_at" gorm:"bigint;index"`
}

func (subscriptionRecordBeforeCycle) TableName() string { return "subscription_pre_consume_records" }

// TestMidjourneyFailedFundingRecoveryDatabaseMatrix 验证系统轮询能恢复已结束任务的退款。
// 每个方言使用真实隔离数据库，旧表夹具来自本轮前的已发布模型。
// fresh 与 upgrade 都连续迁移两次，确认启动重跑不会损坏历史数据。
// 原订阅、钱包和 Key 的余额由真实事务更新，不用资金 mock。
// 任务没有有效渠道，证明已确定失败的本地退款不依赖原厂可达。
// 此测试覆盖系统任务的调度边界：进度完成不等于资金补偿已经完成。
// 不只调用退款函数，因为轮询筛选漏掉任务同样会造成用户损失。
// 首轮观测钱包、订阅、Key 三本账以及持久任务的退款标记。
// 次轮观测队列为空，避免只依赖单个 Go 对象已经被修改。
// 数据库设置由已有隔离工厂恢复，不连接或清理任何生产业务表。
// 外部数据库没有 DSN 时明确标成跳过，不能冒称执行过三库。
// 正式验收传入两条隔离 DSN，使六个迁移场景全部实际执行。
// MySQL 和 PostgreSQL 使用各自驱动，不能用 SQLite 模拟方言。
// 故障事务和同请求重放由资金入口测试负责，此处只测恢复调度。
// 历史任务没有新来源字段时继续原合同，不被当作待补偿新任务。
// 原有使用统计可由退款服务维护，测试不代替生产代码补写 marker。
// 新周期的金额与退款额故意不同，错误相减会得到可观察的三十。
// 旧表和新表场景使用不同数据库，避免升级测试从新建列中获益。
// 本轮新增周期字段允许 NULL，历史未知与新记录真实零必须能区分。
// 未知历史周期不被迁移自动填成今天，也不改旧单的资金归属。
func TestMidjourneyFailedFundingRecoveryDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct{ kind, env string }{{"sqlite", ""}, {"mysql", "TEST_MYSQL_DSN"}, {"postgres", "TEST_POSTGRES_DSN"}} {
		for _, upgrade := range []bool{false, true} {
			name := dialect.kind + "/fresh"
			if upgrade {
				name = dialect.kind + "/upgrade"
			}
			t.Run(name, func(t *testing.T) {
				if dialect.env != "" && os.Getenv(dialect.env) == "" {
					t.Skip("external database DSN not configured")
				}
				db := modelManagementDB(t, dialect.kind, os.Getenv(dialect.env))
				oldBatch := common.BatchUpdateEnabled
				common.BatchUpdateEnabled = false
				t.Cleanup(func() { common.BatchUpdateEnabled = oldBatch })
				if upgrade {
					// 旧行先写入旧 schema，后续新增字段只能取安全的无策略默认。
					// 明确保留非零历史金额，防止迁移错误清掉既有账目。
					// SUCCESS 任务不应因为新增退款查询而重入补偿队列。
					// 旧预扣记录的请求 ID 与金额必须跨迁移保留。
					// 新增周期字段默认零，但不得生成一个伪造的当前周期。
					require.NoError(t, db.AutoMigrate(&midjourneyBeforeResourcePolicy{}, &subscriptionRecordBeforeCycle{}))
					require.NoError(t, db.Create(&midjourneyBeforeResourcePolicy{Id: 10, MjId: "historical", Status: "SUCCESS", Progress: "100%", Quota: 7}).Error)
					require.NoError(t, db.Create(&subscriptionRecordBeforeCycle{RequestId: "historical", UserId: 1, UserSubscriptionId: 1, PreConsumed: 7, Status: "consumed"}).Error)
				}
				// 同时覆盖持久任务和订阅预扣记录的新增周期字段。
				// 测试必须真实执行迁移，不通过手动建新列替代启动行为。
				// 同一连接先升级再重跑，覆盖服务重启重复迁移。
				// 预扣表的唯一请求 ID 仍由旧 schema 夹具带入。
				// Token 和日志表只为真实退款服务提供必要运行边界。
				// 不启动模型厂商服务，也不伪造一次远端到账通知。
				for range 2 {
					require.NoError(t, db.AutoMigrate(&model.Midjourney{}, &model.SubscriptionPreConsumeRecord{}, &model.UserSubscription{}, &model.Token{}, &model.Log{}))
				}
				if upgrade {
					var historical model.Midjourney
					require.NoError(t, db.First(&historical, 10).Error)
					assert.Equal(t, "historical", historical.MjId)
					assert.Equal(t, 7, historical.Quota)
					assert.Empty(t, historical.BillingSource)
					var record model.SubscriptionPreConsumeRecord
					require.NoError(t, db.Where("request_id = ?", "historical").First(&record).Error)
					assert.EqualValues(t, 7, record.PreConsumed)
					assert.Zero(t, record.SubscriptionResetTime)
				}
				// 同周期任务退原订阅；跨周期任务不释放新周期其他请求的额度。
				// 三种来源各用独立用户与 Key，避免一笔退款掩盖另一笔错误。
				// 钱包初始八十，退十后只能到九十，不能同时修改订阅。
				// 订阅同周期已用四十，退十后只能到三十，钱包保持八十。
				// 跨周期仍已用四十，那是新周期其他调用的金额。
				// 三种情形都恢复 Key 的十单位预算，避免永久占用调用限额。
				// 用户推广码具有唯一索引，夹具提供不同值避免无关约束失败。
				for index, source := range []string{"wallet", "subscription", "subscription"} {
					user := model.User{Username: name + source + string(rune('a'+index)), Quota: 80, UsedQuota: 10, Status: common.UserStatusEnabled, AffCode: source + string(rune('a'+index))}
					require.NoError(t, db.Create(&user).Error)
					token := model.Token{UserId: user.Id, Key: name + source + string(rune('a'+index)), RemainQuota: 90, UsedQuota: 10}
					require.NoError(t, db.Create(&token).Error)
					sub := model.UserSubscription{UserId: user.Id, AmountTotal: 100, AmountUsed: 40, LastResetTime: 100}
					if index == 2 {
						sub.LastResetTime = 200
					}
					require.NoError(t, db.Create(&sub).Error)
					task := model.Midjourney{UserId: user.Id, TokenId: token.Id, MjId: source + string(rune('a'+index)), Status: "FAILURE", Progress: "100%", Quota: 10, BillingSource: source, SubscriptionId: sub.Id, SubscriptionResetTime: 100}
					require.NoError(t, db.Create(&task).Error)
					assert.True(t, model.HasUnfinishedMidjourneyTasks())
					// 直接触发真实轮询，既不伪造新上游失败，也不直接调用退款内部函数。
					// 渠道不存在仍应完成退款；正常上游状态不会阻挡补偿。
					// 本轮 channelsScanned 必须为零，证明没有误触模型供应商。
					// 重新读取全部模型，防止只检查服务修改的内存引用。
					// 已退款金额以任务行清零表达，重启后仍保留幂等事实。
					result := runMidjourneyTaskUpdateOnce(context.Background(), nil)
					assert.Equal(t, 0, result.ChannelsScanned)
					require.NoError(t, db.First(&user, user.Id).Error)
					require.NoError(t, db.First(&token, token.Id).Error)
					require.NoError(t, db.First(&sub, sub.Id).Error)
					require.NoError(t, db.First(&task, task.Id).Error)
					if source == "wallet" {
						assert.Equal(t, 90, user.Quota)
					} else {
						assert.Equal(t, 80, user.Quota)
					}
					expectedUsed := int64(40)
					if source == "subscription" && index != 2 {
						expectedUsed = 30
					}
					assert.Equal(t, expectedUsed, sub.AmountUsed)
					assert.Equal(t, 100, token.RemainQuota)
					assert.Zero(t, token.UsedQuota)
					assert.Zero(t, task.Quota)
					// 完成后持久 marker 已清除，下一轮不会再次加款或占用调度槽。
					// 幂等观察来自真实数据库，不只检查内存任务对象的 Quota。
					// 队列存在性与详情列表使用一致的退款未完成条件。
					// 空队列不会触发下一轮原厂请求，也不会再次恢复 Key。
					// 不插入固定等待，事务返回后即可观察确定的持久结果。
					assert.False(t, model.HasUnfinishedMidjourneyTasks())
					again := runMidjourneyTaskUpdateOnce(context.Background(), nil)
					assert.Zero(t, again.UnfinishedTasks)
					require.NoError(t, db.First(&token, token.Id).Error)
					assert.Equal(t, 100, token.RemainQuota)
				}
			})
		}
	}
}
