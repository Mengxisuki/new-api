package controller

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 事件模块复用现有安全测试的真实登录会话和隔离数据库。
// 每种方言运行同一组行为，不用 SQLite 冒充外部数据库。
// 管理员仅在本测试数据库升为 Root，不更改真实账号。
// 独立日志库仍由原 fixture 创建，事件始终保存主数据库。
// 捕获开关按原值恢复，防止污染未迁移事件表的旧测试。
// 测试凭据仅存在内存，不输出完整 Token 或单次证明。
// 开启设置只影响投递，不是启用捕获的替代条件。
// 产品默认关闭，测试必须主动开启才能触发实时消费。
// Root 角色变化后同步认证缓存，避免测试依赖过时角色。
// 有效期和 SID 由正式签发器生成，断流测试覆盖同一身份。
// 数据库清理由既有 fixture 接管，本层不删除开发环境表。
// 多库子测试串行执行，因项目数据库句柄是进程级状态。
// 断言失败后仍恢复捕获设置，避免后续测试出现连锁误报。
func hubTestFixture(t *testing.T, dialect string) (*model.User, service.AuthIdentity, string) {
	t.Helper()
	t.Setenv("TEST_SECURITY_DIALECT", dialect)
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleRootUser).Error)
	user.Role = common.RoleRootUser
	require.NoError(t, model.PublishUserAuthCache(user.Id))
	// 概览统计未决任务，测试也迁移真实任务表而非伪造零值。
	// 模型 Key 表支持任务关联，但不授予事件凭据模型执行权限。
	require.NoError(t, model.DB.AutoMigrate(&model.Task{}, &model.Token{}))
	previousCapture := model.HubCaptureEnabled()
	require.NoError(t, model.MigrateEventHub(model.DB))
	settings, err := model.GetHubSettings()
	require.NoError(t, err)
	settings.Enabled = true
	require.NoError(t, model.UpdateHubSettings(settings))
	model.SetHubCaptureEnabled(true)
	t.Cleanup(func() { model.SetHubCaptureEnabled(previousCapture) })
	token, _, err := service.IssueAccessToken(identity)
	require.NoError(t, err)
	return user, identity, token
}

// 与正式路由使用同名路径，确保 PAT 路由声明按真实模板匹配。
// 管理接口同时要求 Root 和网页登录，事件 Token 不能创建新凭据。
// 消费接口独立使用事件鉴权，避免误接入模型 Key 权限体系。
// 不人工设置身份上下文，让测试经过真实 JWT 与事件 Token 校验。
// 真正的网络 SSE 通过 httptest.Server 承接，不用睡眠猜测结果。
// 直接注册处理器避免导入 router 包形成 controller 测试循环依赖。
// 路由组仅隔离传输包装，业务校验继续执行生产实现。
// 证明与管理登录各自校验，不允许消费令牌继承 Root 会话。
// 连接确认走普通 POST，SSE 读取本身不得推进 ACK 状态。
// 路由模板中的消费者编号仍由后端与 Token 归属交叉验证。
// 此处不添加 gzip，正式路由是否排除压缩仍由主代理复核。
func hubTestRouter() *gin.Engine {
	router := gin.New()
	router.GET("/api/event-hub/stream", HubClientAuth(), HubStream)
	router.POST("/api/event-hub/ack", HubClientAuth(), HubAck)
	management := router.Group("/api/event-hub", middleware.RootAuth(), HubBrowserOnly)
	management.GET("", HubOverview)
	management.PUT("/settings", HubUpdateSettings)
	management.POST("/consumers", HubCreateConsumer)
	management.PUT("/consumers/:id", HubUpdateConsumer)
	management.GET("/consumers/:id", HubConsumerDetail)
	management.POST("/consumers/:id/tokens", HubCreateToken)
	management.DELETE("/consumers/:id/tokens/:token_id", HubRevokeToken)
	management.POST("/consumers/:id/replay", HubReplayEvent)
	management.GET("/events", HubListEvents)
	return router
}

// 普通请求复用既有请求器，凭据及证明只放在正式请求头。
// 响应体必须由公共 JSON 包解析，错误不能被空结构吞掉。
// output 可省略用于只检查 HTTP 状态，不构造伪业务响应。
// nil 请求体会序列化成合法 JSON，GET 处理器不能依赖正文。
// HTTP 错误与 success 字段分别判断，避免业务失败伪装成成功。
// 单次证明头由共用请求器传递，不写到 URL 或日志。
// 响应解析失败立即停止用例，不能继续读取零值作为授权结论。
func hubTestRequest(t *testing.T, router http.Handler, method, path, token, proof string, body any, output any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := common.Marshal(body)
	require.NoError(t, err)
	response := accessTokenRequest(router, method, path, token, proof, string(encoded))
	if output != nil {
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), output), response.Body.String())
	}
	return response
}

// 持久事件由业务同事务入口创建，测试不调用传输层伪造事件。
// payload 只使用明确的安全摘要，以真实序列化检查敏感字段边界。
// 所有调用都检查错误，失败不能被解释为暂时没有消息。
// 固定事件键模拟支付网关重投，相同键必须映射同一业务结果。
// 用户编号来自测试输入，用来覆盖持久事件的关注过滤字段。
// 未使用请求上下文，证明生产事件不依赖客户端保持在线。
// 事务内的追加失败会传播到外层，不能吞掉写库错误。
func hubTestEvent(t *testing.T, key, topic string, userID int) {
	t.Helper()
	require.NoError(t, model.DB.Transaction(func(tx *gorm.DB) error {
		return model.AppendHubEventTx(tx, key, topic, userID, map[string]any{"order_id": userID, "status": "paid"})
	}))
}

// 读取一条完整 SSE 帧，允许先经过 ready 或心跳帧。
// 单行 data 由服务端 JSON 序列化，不能按任意网络读块当作消息边界。
// 网络超时由请求 context 控制，遇到断流返回真实错误。
// 不在此 helper 里自动 ACK，测试可分别检查投递和确认语义。
// 心跳只能证明连接存活，不代表有需要确认的业务事件。
// 事件名称和 JSON 正文分别返回，支持协议字段独立演进。
// 首帧可能仅包含注释，不能将空正文解释为业务成功。
// 扫描错误保留原值，以区分正常 EOF 与网络超时。
// 只读取服务端真实文本帧，不根据数据库结果拼接预期正文。
func hubReadEvent(scanner *bufio.Scanner) (string, string, error) {
	var event, data string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if data != "" && event != "ready" && event != "heartbeat" {
				return event, data, nil
			}
			event, data = "", ""
			continue
		}
		if value, ok := strings.CutPrefix(line, "event:"); ok {
			event = strings.TrimSpace(value)
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			data = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", err
	}
	return "", "", io.EOF
}

// 每次消费建立真实 HTTP 连接，单次测试有独立取消与超时。
// 开启 Accept-Encoding 用于确认服务端不会把 SSE 变成缓冲压缩响应。
// Header 游标不能代替数据库 ACK，也不能用来跳过其他未确认事件。
// 只有调用者处理并确认后，重连才应停止重放该投递。
// 最大游标刻意超过真实编号，用于检验客户端不能跳过 ACK。
// 请求取消只是测试资源释放，不应当在服务端隐式确认消息。
// 每次调用有独立 context，上一连接退出不会取消下一连接。
// 响应体先交给实际扫描器，状态码不能代替事件交付验收。
// 请求总时限只是测试失败上限，不作为正常业务节奏断言。
func hubTestStream(t *testing.T, serverURL, token string, consumerID int) (*http.Response, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+"/api/event-hub/stream?consumer_id="+strconv.Itoa(consumerID), nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept-Encoding", "gzip")
	request.Header.Set("Last-Event-ID", "999999999")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	t.Cleanup(func() { cancel(); _ = response.Body.Close() })
	return response, cancel
}

// 三库矩阵同时覆盖新装及在已有账号、会话、日志库上的升级。
// 每次迁移连续执行两遍，确认默认设置不会重新启用已暂停服务。
// 幂等键、事务回滚和独立消费者是可观察契约，不能只断言建表成功。
// 缺少外部 DSN 时明确跳过，最终交付记录必须区分运行与未运行。
// 订单与事件原子性通过真实事务回滚验证，不使用 ORM mock。
// 重复迁移不得重置显式 false，守住运维暂停服务的选择。
// 自增主键与提交先后可能不同，查询不能永久依赖最大 ID。
// 低编号晚可见用显式插入模拟，断言最终有独立投递记录。
// 业务幂等只约束事件生成，不能让两个消费者共享一条投递。
// 未发送 ACK 必须失败，防止客户端跳过离线积压。
// 主库和日志库分离仍能工作，证明通知不靠扫描消费日志。
// 真实数据库版本沿用 fixture 输出，不能用驱动版本替代。
func TestEventHubDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			if dialect != "sqlite" && os.Getenv("TEST_"+strings.ToUpper(dialect)+"_DSN") == "" {
				t.Skip("external test database is not configured")
			}
			user, _, _ := hubTestFixture(t, dialect)
			db := model.DB
			// 升级后原账号、独立日志句柄仍保留，事件不搬进日志库。
			var persisted model.User
			require.NoError(t, db.First(&persisted, user.Id).Error)
			assert.Equal(t, user.Username, persisted.Username)
			assert.NotSame(t, db, model.LOG_DB)
			// 迁移后的账号可正常读取，排除新表创建破坏已有用户数据。
			// 重复调用正式迁移函数同时覆盖索引和配置初始化。
			require.NoError(t, model.MigrateEventHub(db))
			require.NoError(t, model.MigrateEventHub(db))
			// 从节点不执行迁移，也必须在验证完整表结构后开启事件捕获。
			// 显式关闭模拟新进程初始状态，不能依赖 fixture 已打开的全局值。
			// 恢复捕获后沿用后续真实业务写入，验证初始化与迁移彼此独立。
			model.SetHubCaptureEnabled(false)
			require.NoError(t, model.InitializeHubCapture())
			assert.True(t, model.HubCaptureEnabled())
			first, err := model.CreateHubConsumer(user.Id, "财务桌面", []string{model.HubTopicTopup}, nil)
			require.NoError(t, err)
			second, err := model.CreateHubConsumer(user.Id, "财务脚本", []string{model.HubTopicTopup}, nil)
			require.NoError(t, err)
			// 同一业务幂等重投只能持久一条，回滚事务不能留下通知。
			hubTestEvent(t, "wallet:one", model.HubTopicTopup, 7)
			hubTestEvent(t, "wallet:one", model.HubTopicTopup, 7)
			rollback := errors.New("intentional rollback")
			require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
				require.NoError(t, model.AppendHubEventTx(tx, "wallet:rollback", model.HubTopicTopup, 7, map[string]any{"order_id": 99}))
				return rollback
			}), rollback)
			var count int64
			require.NoError(t, db.Model(&model.HubEvent{}).Count(&count).Error)
			// 回滚和重投共用同一计数断言，任何多余事件都会暴露。
			// 后续按事件内容分发，不依赖此计数作为运行时游标。
			assert.EqualValues(t, 1, count)
			// 两个消费者各自获得一份，不能使用共享游标造成抢占。
			one, err := model.MaterializeHubDeliveries(first.ID, []string{model.HubTopicTopup})
			require.NoError(t, err)
			require.Len(t, one, 1)
			two, err := model.MaterializeHubDeliveries(second.ID, []string{model.HubTopicTopup})
			require.NoError(t, err)
			require.Len(t, two, 1)
			assert.Equal(t, one[0].EventID, two[0].EventID)
			assert.NotEqual(t, one[0].ID, two[0].ID)
			// 相同事件编号用于客户端幂等，投递编号隔离各消费者的 ACK。
			// 这同时排除“先到先得”的消费组语义被误接入广播需求。
			// ACK 不能跨消费者，也不能确认从未写入连接的事件。
			assert.Error(t, model.AckHubDelivery(second.ID, one[0].ID))
			assert.Error(t, model.AckHubDelivery(first.ID, one[0].ID))
			// 较早分配的事件后可见时仍要发现，不能以最大 ID 越过它。
			late := model.HubEvent{ID: one[0].EventID + 10, Key: "wallet:late", Topic: model.HubTopicTopup, UserID: 7, Payload: `{"order_id":8}`, CreatedAt: common.GetTimestamp()}
			ahead := model.HubEvent{ID: one[0].EventID + 20, Key: "wallet:ahead", Topic: model.HubTopicTopup, UserID: 7, Payload: `{"order_id":9}`, CreatedAt: common.GetTimestamp()}
			require.NoError(t, db.Create(&ahead).Error)
			_, err = model.MaterializeHubDeliveries(first.ID, []string{model.HubTopicTopup})
			require.NoError(t, err)
			require.NoError(t, db.Create(&late).Error)
			_, err = model.MaterializeHubDeliveries(first.ID, []string{model.HubTopicTopup})
			require.NoError(t, err)
			var discovered model.HubDelivery
			require.NoError(t, db.Where("consumer_id = ? AND event_id = ?", first.ID, late.ID).First(&discovered).Error)
			// 用户范围仅缩小可见事件，相同主题的其他用户不得进入待办。
			// 过滤保存到数据库，重建消费者对象后也必须保持相同行为。
			filtered, err := model.CreateHubConsumer(user.Id, "指定客户", []string{model.HubTopicTopup}, []int{42})
			require.NoError(t, err)
			hubTestEvent(t, "wallet:watched", model.HubTopicTopup, 42)
			hubTestEvent(t, "wallet:unwatched", model.HubTopicTopup, 43)
			watched, err := model.MaterializeHubDeliveries(filtered.ID, []string{model.HubTopicTopup})
			require.NoError(t, err)
			require.Len(t, watched, 1)
			var watchedEvent model.HubEvent
			require.NoError(t, db.First(&watchedEvent, watched[0].EventID).Error)
			assert.Equal(t, 42, watchedEvent.UserID)
			// 三种数据库都验证高熵凭据指纹存储及持久撤销条件。
			// 有效凭据只获得创建时的主题，不能任意扩大订阅。
			raw, credential, err := model.CreateHubToken(filtered.ID, "测试凭据", []string{model.HubTopicTopup}, common.GetTimestamp()+7200)
			require.NoError(t, err)
			assert.NotEqual(t, raw, credential.TokenHash)
			assert.Equal(t, model.AccessTokenFingerprint(raw), credential.TokenHash)
			_, _, authorized, err := model.AuthenticateHubToken(raw)
			require.NoError(t, err)
			assert.Equal(t, []string{model.HubTopicTopup}, authorized)
			require.NoError(t, model.RevokeHubToken(filtered.ID, credential.ID))
			_, _, _, err = model.AuthenticateHubToken(raw)
			assert.Error(t, err)
			// 连接上限统计有效租约，多节点同时接入也以主库为准。
			// 固定八次构造真实配额边界，不是随机压力或性能测试。
			// 过期租约可以腾出位置，但不能在新连接占位后重新复活。
			// 清理或进程停顿导致旧租约失效时，连接必须重新建立。
			connections := make([]string, 0, 8)
			for range 8 {
				connection, err := model.OpenHubConnection(filtered.ID, 0)
				require.NoError(t, err)
				connections = append(connections, connection)
			}
			_, err = model.OpenHubConnection(filtered.ID, 0)
			assert.ErrorIs(t, err, model.ErrHubConflict)
			require.NoError(t, db.Model(&model.HubConnection{}).Where("id = ?", connections[0]).Update("expires_at", common.GetTimestamp()-1).Error)
			_, err = model.OpenHubConnection(filtered.ID, 0)
			require.NoError(t, err)
			assert.Error(t, model.TouchHubConnection(connections[0], filtered.ID, 0))
			// 已被清理的连接不能仅凭一个成功零行UPDATE继续发送。
			// 此错误关闭流并释放资源，避免管理页错误显示在线数。
			assert.Error(t, model.TouchHubConnection("missing-lease", filtered.ID, 0))
			// 保留期不是无限消息存储；过期未确认必须留下明确对账缺口。
			// 清理重跑不能重复累计，原始事件和投递一起删除。
			// 时间直接写测试记录，避免等待真实七天或依赖系统时钟推进。
			// 尚未物化的关注事件同样应记录缺口，不能把离线误当作已确认。
			// 消费者先于过期事件存在，排除创建前历史不属于关注范围的情况。
			require.NoError(t, db.Model(filtered).Update("created_at", common.GetTimestamp()-9*86400).Error)
			require.NoError(t, db.Model(&watchedEvent).Update("created_at", common.GetTimestamp()-8*86400).Error)
			require.NoError(t, model.CleanupHubEvents())
			require.NoError(t, model.CleanupHubEvents())
			var cleaned model.HubConsumer
			require.NoError(t, db.First(&cleaned, filtered.ID).Error)
			assert.EqualValues(t, 1, cleaned.ExpiredUnackedCount)
			var remaining int64
			require.NoError(t, db.Model(&model.HubDelivery{}).Where("event_id = ?", watchedEvent.ID).Count(&remaining).Error)
			assert.Zero(t, remaining)
			// 多节点清理与分配同时发生，仍只有一份累计缺口。
			// 有界三个操作代表两台服务节点和一条消费者连接。
			// 同时启动不使用睡眠制造时序，最后直接检查持久业务不变量。
			// 过期事件删除后不允许留下无法关联摘要的孤立投递。
			hubTestEvent(t, "wallet:concurrent-expiry", model.HubTopicTopup, 42)
			require.NoError(t, db.Model(&model.HubEvent{}).Where("event_key = ?", "wallet:concurrent-expiry").Update("created_at", common.GetTimestamp()-8*86400).Error)
			start := make(chan struct{})
			results := make(chan error, 3)
			for range 2 {
				go func() { <-start; results <- model.CleanupHubEvents() }()
			}
			go func() {
				<-start
				_, err := model.MaterializeHubDeliveries(filtered.ID, []string{model.HubTopicTopup})
				results <- err
			}()
			close(start)
			for range 3 {
				require.NoError(t, <-results)
			}
			require.NoError(t, db.First(&cleaned, filtered.ID).Error)
			assert.EqualValues(t, 2, cleaned.ExpiredUnackedCount)
			var orphans int64
			require.NoError(t, db.Table("hub_deliveries").Joins("LEFT JOIN hub_events ON hub_events.id = hub_deliveries.event_id").Where("hub_events.id IS NULL").Count(&orphans).Error)
			assert.Zero(t, orphans)
			// 修改服务设置后迁移不得把 false 覆盖回 true。
			// 服务暂停仍保存事件，恢复时可以继续产生投递。
			// 设置是部署级状态，不跟随某个消费者的确认结果。
			require.NoError(t, db.Model(&model.HubSettings{}).Where("id = ?", 1).Update("enabled", false).Error)
			hubTestEvent(t, "wallet:paused", model.HubTopicTopup, 42)
			var paused model.HubEvent
			require.NoError(t, db.Where("event_key = ?", "wallet:paused").First(&paused).Error)
			require.NoError(t, model.MigrateEventHub(db))
			settings, err := model.GetHubSettings()
			require.NoError(t, err)
			assert.False(t, settings.Enabled)
		})
	}
}

// 事件 Token 只被消费入口接受，不能拿来访问管理接口或模型 API。
// 主题白名单与消费者范围交集决定实际投递，不能靠查询参数扩权。
// 每笔消息的确认属于消费者，凭据轮换继续同一消费进度。
// 真实 SSE 连接验证鉴权、编码、断线恢复和撤销后的停止发送。
// 多枚凭据映射同一消费者，轮换时继续使用已建立的进度。
// 跨消费者传入编号必须拒绝，即使凭据本身尚未过期。
// 查询字符串凭据明确不接受，避免代理访问日志泄露认证信息。
// 管理列表只暴露 Token 提示，不得把指纹或可用原文序列化。
// 一枚凭据撤销不应影响同消费者其他未撤销的独立凭据。
// 客户端确认调用重复发送应幂等，允许网络重试。
// 关注两种主题的消费者仍受窄 Token 交集限制。
// 不使用默认 EventSource，实际验证 Authorization 头接入方式。
// 传输元数据允许随重试更新，消息身份和业务摘要必须保持一致。
// 取消首个连接只清理在线租约，不删除待确认记录。
// 狭窄授权同样限制确认接口，防止客户端凭猜测编号完成他人事项。
// 网络帧错误与业务字段分开断言，便于区分鉴权和序列化回归。
func TestEventHubClientAuthorizationAndReplay(t *testing.T) {
	user, _, browser := hubTestFixture(t, "sqlite")
	consumer, err := model.CreateHubConsumer(user.Id, "运营客户端", []string{model.HubTopicTopup, model.HubTopicRecharge}, nil)
	require.NoError(t, err)
	other, err := model.CreateHubConsumer(user.Id, "另一个客户端", []string{model.HubTopicTopup}, nil)
	require.NoError(t, err)
	credential, token, err := model.CreateHubToken(consumer.ID, "充值只读", []string{model.HubTopicTopup}, time.Now().Add(2*time.Hour).Unix())
	require.NoError(t, err)
	secondCredential, _, err := model.CreateHubToken(consumer.ID, "轮换凭据", []string{model.HubTopicTopup}, time.Now().Add(2*time.Hour).Unix())
	require.NoError(t, err)
	otherCredential, _, err := model.CreateHubToken(other.ID, "独立客户端凭据", []string{model.HubTopicTopup}, time.Now().Add(2*time.Hour).Unix())
	require.NoError(t, err)
	router := hubTestRouter()
	for _, tc := range []struct {
		method, path, credential string
		status                   int
	}{
		{http.MethodGet, "/api/event-hub/stream?consumer_id=" + strconv.Itoa(consumer.ID), "", http.StatusUnauthorized},
		{http.MethodGet, "/api/event-hub/stream?consumer_id=" + strconv.Itoa(consumer.ID) + "&token=" + credential, "", http.StatusUnauthorized},
		{http.MethodGet, "/api/event-hub/stream?consumer_id=" + strconv.Itoa(other.ID), credential, http.StatusUnauthorized},
		{http.MethodGet, "/api/event-hub", credential, http.StatusUnauthorized},
	} {
		response := hubTestRequest(t, router, tc.method, tc.path, tc.credential, "", nil, nil)
		assert.Equal(t, tc.status, response.Code, tc.path)
	}
	// 敏感资料主题有持久事件，但只读充值 Token 不得看见或确认。
	hubTestEvent(t, "sse:topup", model.HubTopicTopup, 7)
	hubTestEvent(t, "sse:info", model.HubTopicRecharge, 7)
	server := httptest.NewServer(router)
	defer server.Close()
	response, cancel := hubTestStream(t, server.URL, credential, consumer.ID)
	require.Equal(t, http.StatusOK, response.StatusCode)
	assert.Contains(t, response.Header.Get("Content-Type"), "text/event-stream")
	assert.Empty(t, response.Header.Get("Content-Encoding"))
	_, payload, err := hubReadEvent(bufio.NewScanner(response.Body))
	require.NoError(t, err)
	assert.Contains(t, payload, model.HubTopicTopup)
	assert.NotContains(t, payload, model.HubTopicRecharge)
	cancel()
	require.NoError(t, response.Body.Close())
	var delivery model.HubDelivery
	require.NoError(t, model.DB.Where("consumer_id = ?", consumer.ID).First(&delivery).Error)
	assert.Positive(t, delivery.LastSentAt)
	// 重投间隔只用于限制频率，测试推进持久时间而不真实等待。
	// 该时间调整不修改 ACK，消息仍然属于未确认状态。
	require.NoError(t, model.DB.Model(&delivery).Update("last_sent_at", common.GetTimestamp()-31).Error)
	// 未确认事件断线后必须重放；不受客户端伪造的大游标影响。
	response, cancel = hubTestStream(t, server.URL, secondCredential, consumer.ID)
	_, repeated, err := hubReadEvent(bufio.NewScanner(response.Body))
	require.NoError(t, err)
	var firstFrame, replayedFrame model.HubDeliveryView
	require.NoError(t, common.UnmarshalJsonStr(payload, &firstFrame))
	require.NoError(t, common.UnmarshalJsonStr(repeated, &replayedFrame))
	assert.Equal(t, firstFrame.EventID, replayedFrame.EventID)
	assert.Equal(t, firstFrame.ID, replayedFrame.ID)
	assert.Equal(t, firstFrame.Payload, replayedFrame.Payload)
	cancel()
	require.NoError(t, response.Body.Close())
	// 另一消费者即使持有有效 Token，也不能确认当前消费者的投递。
	ack := map[string]any{"delivery_id": delivery.ID}
	assert.Equal(t, http.StatusNotFound, hubTestRequest(t, router, http.MethodPost, "/api/event-hub/ack", otherCredential, "", ack, nil).Code)
	assert.Equal(t, http.StatusOK, hubTestRequest(t, router, http.MethodPost, "/api/event-hub/ack", secondCredential, "", ack, nil).Code)
	assert.Equal(t, http.StatusOK, hubTestRequest(t, router, http.MethodPost, "/api/event-hub/ack", secondCredential, "", ack, nil).Code)
	// 管理详情不返回凭据明文或哈希，消费 Token 也没有管理权限。
	detail := hubTestRequest(t, router, http.MethodGet, fmt.Sprintf("/api/event-hub/consumers/%d", consumer.ID), browser, "", nil, nil)
	require.Equal(t, http.StatusOK, detail.Code)
	assert.NotContains(t, detail.Body.String(), credential)
	assert.NotContains(t, detail.Body.String(), token.TokenHash)
	// 已确认事件补发只重开投递，不改变原始事件或任何业务资金。
	// 确认状态回到待处理后，连接收到的仍是相同事件编号。
	require.NoError(t, model.ReplayHubEvent(consumer.ID, delivery.EventID))
	response, cancel = hubTestStream(t, server.URL, credential, consumer.ID)
	scanner := bufio.NewScanner(response.Body)
	_, _, err = hubReadEvent(scanner)
	require.NoError(t, err)
	// 撤销必须影响已在线连接，不能等客户端自行重新连接才生效。
	// 读取实际错误帧而非睡眠后查询数据库，验证持续鉴权接入。
	require.NoError(t, model.RevokeHubToken(consumer.ID, token.ID))
	eventName, revoked, err := hubReadEvent(scanner)
	require.NoError(t, err)
	assert.Equal(t, "error", eventName)
	assert.Contains(t, revoked, "authorization_revoked")
	assert.False(t, scanner.Scan())
	cancel()
	require.NoError(t, response.Body.Close())
	assert.Equal(t, http.StatusUnauthorized, hubTestRequest(t, router, http.MethodPost, "/api/event-hub/ack", credential, "", ack, nil).Code)
	assert.Equal(t, http.StatusOK, hubTestRequest(t, router, http.MethodPost, "/api/event-hub/ack", secondCredential, "", ack, nil).Code)
	// 宽订阅下物化的另一主题仍不能被窄授权 Token 确认。
	// 这验证 ACK 与发送应用同一权限交集，而非仅检查消费者编号。
	info, err := model.MaterializeHubDeliveries(consumer.ID, []string{model.HubTopicRecharge})
	require.NoError(t, err)
	require.Len(t, info, 1)
	require.NoError(t, model.MarkHubDeliverySent(consumer.ID, info[0].ID))
	assert.Equal(t, http.StatusForbidden, hubTestRequest(t, router, http.MethodPost, "/api/event-hub/ack", secondCredential, "", map[string]any{"delivery_id": info[0].ID}, nil).Code)
	// 到期丢失提醒是消费者级别的永久累计，不伪装成可重新ACK的消息。
	// 即使当前Token不订阅旧事件主题，也要知道自己的消费历史有缺口。
	// 每次新连接报告一次，提示先对账业务状态而非自动当作处理成功。
	// 日志清理与网页离线不应悄悄改变客户端对可靠性的理解。
	// 同步回推消费者创建时间，保留实际先订阅再产生消息的先后关系。
	require.NoError(t, model.DB.Model(consumer).Update("created_at", common.GetTimestamp()-9*86400).Error)
	require.NoError(t, model.DB.Model(&model.HubEvent{}).Where("id = ?", info[0].EventID).Update("created_at", common.GetTimestamp()-8*86400).Error)
	require.NoError(t, model.CleanupHubEvents())
	response, cancel = hubTestStream(t, server.URL, secondCredential, consumer.ID)
	eventName, gap, err := hubReadEvent(bufio.NewScanner(response.Body))
	require.NoError(t, err)
	assert.Equal(t, "retention_gap", eventName)
	assert.Contains(t, gap, `"expired_unacked_count":1`)
	assert.Contains(t, gap, "reconcile_business_state")
	cancel()
	require.NoError(t, response.Body.Close())
}

// 新建事件凭据必须通过真实单次安全证明，不能只依靠前端确认框。
// 证明绑定消费者、主题和过期时间，换订阅目标不得复用。
// 创建成功后同一证明再次提交必须失败，不可发出第二份凭据。
// 错误权限在消耗证明前拒绝，避免恶意请求烧掉合法授权。
// 证明本身不作为消费凭证，生成后仍要兑换一次明确的 Token。
// 明文前缀与管理 PAT 不同，能够阻断通用令牌路径误识别。
// 有效期使用未来一天，避免测试时钟边界误判最短授权时长。
// 消费者由同一 Root 创建，测试只隔离授权证明这一失败条件。
// context 由服务器重建，主题顺序使用同一规范化合同。
// 凭据名称不属于授权范围，消费者和有效期必须精确绑定。
// 获取 proof 使用现有安全流程，不能手写已验证布尔上下文。
// 服务返回明文一次后，读取详情不会再次恢复同一秘密。
// 首次缺少证明的请求不能生成持久 Token，再提交才会真正创建。
// proof 重投失败检查 HTTP 与数据变化，避免错误状态伪装成功。
func TestEventHubTokenCreationRequiresBoundProof(t *testing.T) {
	user, identity, browser := hubTestFixture(t, "sqlite")
	consumer, err := model.CreateHubConsumer(user.Id, "安全授权客户端", []string{model.HubTopicTopup}, nil)
	require.NoError(t, err)
	router := hubTestRouter()
	expires := time.Now().Add(24 * time.Hour).Unix()
	body := map[string]any{"name": "桌面端", "topics": []string{model.HubTopicTopup}, "expires_at": expires}
	path := fmt.Sprintf("/api/event-hub/consumers/%d/tokens", consumer.ID)
	assert.NotEqual(t, http.StatusOK, hubTestRequest(t, router, http.MethodPost, path, browser, "", body, nil).Code)
	proofContext, err := common.Marshal(map[string]any{"consumer_id": consumer.ID, "topics": []string{model.HubTopicTopup}, "expires_at": expires})
	require.NoError(t, err)
	// 另一消费者的有效证明不能换成当前消费者的新凭据。
	// 错误上下文是真实签发，不是篡改签名造成的普通认证失败。
	wrongContext, err := common.Marshal(service.HubTokenVerificationContext{ConsumerID: consumer.ID + 1, Topics: []string{model.HubTopicTopup}, ExpiresAt: expires})
	require.NoError(t, err)
	wrongProof := issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: "event_hub.token", Context: wrongContext}, "password")
	assert.NotEqual(t, http.StatusOK, hubTestRequest(t, router, http.MethodPost, path, browser, wrongProof, body, nil).Code)
	proof := issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: "event_hub.token", Context: proofContext}, "password")
	var created struct {
		Success bool
		Data    struct{ Token string }
	}
	response := hubTestRequest(t, router, http.MethodPost, path, browser, proof, body, &created)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.True(t, created.Success)
	assert.True(t, strings.HasPrefix(created.Data.Token, "evt_"))
	assert.NotEqual(t, http.StatusOK, hubTestRequest(t, router, http.MethodPost, path, browser, proof, body, nil).Code)
}
