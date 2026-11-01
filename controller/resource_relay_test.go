package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/hosting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// resourceFixture 运行真实鉴权、策略、控制面 client 与数据面转发。
// 执行层控制面与原厂执行由本地 HTTP 服务替代，钱包与令牌使用真实数据库。
type resourceFixture struct {
	user     *model.User             // 数据库所有者，用于归属和真实钱包验证。
	token    *model.Token            // 统一模型 Key，不承载互斥的收费类型。
	accounts []*model.HostingAccount // 账号顺序由策略明确授权，不能由余额自动重排。
	engine   *gin.Engine
	calls    []string // 仅记录实际执行凭据，不把控制面观察计为模型调用。
	status   int      // 故障发生在外部执行之后，用于检查不可重放边界。
	stream   bool     // 让同一真实转发器处理增量输出，避免另造模拟实现。
}

// newResourceFixture 给每个测试独立数据库与两个账号。
// 账号内部 Key 与管理 Key 不同，假执行端断言权限头没有串用。
// 数据库由既有隔离 helper 创建，外部方言必须显式提供测试连接。
// 使用真实模型 API Key，测试请求不能跳过 TokenAuth 直接注入用户身份。
// 管理 Key 与各账号内部 Key 故意不同，便于观察权限头是否串用。
// 统一定价只为同一夹具中的付费渠道提供可计算预算。
// 本地服务器分别处理控制面目录、观察缓存与模型响应。
// 控制面观察读取夹具快照，测试可以构造未知、新鲜和过期状态。
// 数据面调用只记录 Authorization，不把浏览器 Cookie 或伪造归属头视为许可。
// 批量写库在本夹具关闭，余额断言直接观察已持久化结果。
// 平台渠道和两个私有账号指向同一测试服务，但使用不同权限凭据。
// 旧链路返回专用状态码，便于发现统一来源请求意外落回旧处理器。
// 响应输出由真实转发器处理，夹具不替代调度、预扣、退款或模型过滤。
// 服务器关闭和全局批量写配置都在测试清理中恢复。
func newResourceFixture(t *testing.T) *resourceFixture {
	t.Helper()
	kind := os.Getenv("TEST_RESOURCE_DB_DIALECT")
	if kind == "" {
		kind = "sqlite"
	}
	dsn := ""
	if kind == "mysql" {
		dsn = os.Getenv("TEST_MYSQL_DSN")
	}
	if kind == "postgres" {
		dsn = os.Getenv("TEST_POSTGRES_DSN")
	}
	if kind != "sqlite" && dsn == "" {
		t.Fatal("resource database DSN is required")
	}
	modelManagementDB(t, kind, dsn)
	require.NoError(t, i18n.Init())
	service.InitHttpClient()
	previousBatch := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = false
	t.Cleanup(func() { common.BatchUpdateEnabled = previousBatch })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"gpt-4":0.01}`))
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}, &model.ResourcePolicy{}, &model.HostingAccount{}, &model.Log{}, &model.Channel{}, &model.Ability{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}, &model.SubscriptionPlan{}))
	user := &model.User{Username: "resource-user", Status: common.UserStatusEnabled, Group: "default", Quota: 1000, AuthVersion: 1}
	require.NoError(t, model.DB.Create(user).Error)
	token := &model.Token{UserId: user.Id, Key: "resourcetesttoken", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100}
	require.NoError(t, model.DB.Create(token).Error)
	t.Setenv("CRYPTO_SECRET", strings.Repeat("k", 32))
	f := &resourceFixture{user: user, token: token, status: 200}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/hosting/v1/") {
			require.Equal(t, hosting.OwnerRef(user.Id), hostingTestControlAuthorization(t, r))
			require.Equal(t, hosting.OwnerRef(user.Id), r.URL.Query().Get("owner_ref"))
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(r.URL.Path, "/models") {
				io.WriteString(w, `{"code":0,"data":{"models":[{"id":"gpt-4"}]}}`)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/observation") {
				observation := `{}`
				for _, account := range f.accounts {
					if strings.Contains(r.URL.Path, account.RemoteRef) && account.ObservationJSON != "" {
						observation = account.ObservationJSON
					}
				}
				fmt.Fprintf(w, `{"code":0,"data":%s}`, observation)
				return
			}
			t.Errorf("unexpected control route %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		require.Empty(t, r.Header.Get("Cookie"))      // 浏览器登录材料不属于执行层模型请求。
		require.Empty(t, r.Header.Get("X-Oak-Owner")) // 客户端伪造归属不能越过服务端认证边界。
		f.calls = append(f.calls, r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), `"model":"gpt-4"`)
		if f.stream {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.status)
		if f.status >= 400 {
			io.WriteString(w, `{"error":{"code":"rate_limit_exceeded"}}`)
			return
		}
		io.WriteString(w, `{"id":"response-test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("OAK_GPT_LOAD_URL", server.URL)
	for index := range 2 {
		account := &model.HostingAccount{UserID: user.Id, Provider: "codex", Alias: fmt.Sprint(index), RemoteRef: fmt.Sprintf("account-%d", index), Status: "active"}
		key, err := hosting.EncryptKey(account.RemoteRef, fmt.Sprintf("internal-%d", index))
		require.NoError(t, err)
		account.EncryptedKey = key
		require.NoError(t, model.DB.Create(account).Error)
		f.accounts = append(f.accounts, account)
	}
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Key: "platform-only", Name: "platform", Status: common.ChannelStatusEnabled, Models: "gpt-4", Group: "default", BaseURL: &server.URL}
	require.NoError(t, channel.Insert())
	f.engine = gin.New()
	f.engine.Use(middleware.BodyStorageCleanup())
	f.engine.Use(middleware.TokenAuth())
	f.engine.POST("/v1/chat/completions", ResourceRelay(), func(c *gin.Context) { c.JSON(418, gin.H{"legacy": true}) })
	f.engine.POST("/v1/responses", ResourceRelay())
	f.engine.POST("/v1/images/generations", func(c *gin.Context) { c.Status(418) })
	f.engine.GET("/v1/models", func(c *gin.Context) { ListModels(c, constant.ChannelTypeOpenAI) })
	f.engine.GET("/v1/models/:model", func(c *gin.Context) { RetrieveModel(c, constant.ChannelTypeOpenAI) })
	return f
}

// setPolicy 通过真实模型校验保存顺序，避免测试绕过归属检查。
// 每次读取该 Key 实际版本后再更新，版本冲突不会被测试夹具掩盖。
// 每次保存读取实际 revision，测试不能直接改 JSON 跳过契约校验。
// 只配置当前模型 Key，不能影响同一用户的其他客户端。
// 来源列表的顺序就是明确回退授权，helper 不自动补钱包。
// 空参数保存显式空数组，保持禁用全部与没有策略的区别。
// 保存失败会立刻终止用例，避免错误策略让后续断言失去意义。
// 本 helper 不伪造额度或账号可用性，候选仍由执行时的真实状态检查。
// 同一个测试可以多次替换当前 Key 策略，验证用户撤销来源立即生效。
func (f *resourceFixture) setPolicy(t *testing.T, sources ...model.ResourceSource) {
	t.Helper()
	var row model.ResourcePolicy
	err := model.DB.Where("user_id = ? AND token_id = ?", f.user.Id, f.token.Id).First(&row).Error
	// 只允许首次没有记录，其他数据库故障不能被零版本掩盖。
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		require.NoError(t, err)
	}
	_, err = model.SaveResourcePolicy(f.user.Id, f.token.Id, row.Revision, sources)
	require.NoError(t, err)
}

// request 从统一 Key 入口发请求，恶意身份头必须被新转发器丢弃。
// 返回完整响应，测试断言行为而非内部调用顺序实现。
// 请求使用真实模型 Key，而不是控制台会话或管理凭据。
// 额外放入伪造归属头，确保转发器只信任服务端验证的用户身份。
// Cookie 代表浏览器遗留状态，不能透传给执行层模型接口。
// 处理器通过 Gin 引擎进入，实际经历正文存储、鉴权和来源调度。
// 此 helper 不自动重试，调用次数只能来自被测实现。
// 测试可以检查响应状态、来源标记和 SSE 刷新行为。
// 返回记录器后读取数据库，验证完成响应后的余额结论。
func (f *resourceFixture) request(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer sk-"+f.token.Key)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Cookie", "session=do-not-forward")
	request.Header.Set("X-Oak-Owner", "another-user")
	response := httptest.NewRecorder()
	f.engine.ServeHTTP(response, request)
	return response
}

// 免费身份与付费预算分离：耗尽 Key 可用本人账号，但禁用/过期仍失败。
// 同时验证免费成功后钱包和令牌不变，响应头标识实际来源。
// 用户钱包和 Key 金额预算同时清零，以复现旧鉴权拒绝免费资源的条件。
// 明确授权账号在前、钱包在后，成功不能来自平台渠道。
// 实际受限 Key 证明请求确实发给本人挂靠账号。
// 免费成功后重新读取数据库，而不只检查内存中的余额字段。
// 令牌的金额耗尽状态不能等同于禁用身份。
// 随后把同一个 Key 显式禁用，验证身份限制仍然优先执行。
// 禁用后的请求不能增加上游调用次数，防止免费模式成为鉴权绕过。
func TestResourceRelayFreeWithExhaustedBudget(t *testing.T) {
	f := newResourceFixture(t)
	require.NoError(t, model.DB.Model(f.user).Update("quota", 0).Error)
	require.NoError(t, model.DB.Model(f.token).Updates(map[string]any{"remain_quota": 0, "status": common.TokenStatusExhausted}).Error)
	f.setPolicy(t, model.ResourceSource{Kind: "hosting", AccountID: f.accounts[0].ID, Enabled: true}, model.ResourceSource{Kind: "wallet", Enabled: true})
	response := f.request(t, "POST", "/v1/chat/completions", `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, []string{"Bearer internal-0"}, f.calls)
	require.Equal(t, "hosting", response.Header().Get("X-Oak-Resource-Source"))
	var token model.Token
	require.NoError(t, model.DB.First(&token, f.token.Id).Error)
	require.Zero(t, token.RemainQuota)
	require.Zero(t, token.UsedQuota) // 免费来源不能污染平台金额消费统计。
	quota, err := model.GetUserQuota(f.user.Id, false)
	require.NoError(t, err)
	require.Zero(t, quota)
	// 账户自身限制不会因免费来源而放宽。
	require.NoError(t, model.DB.Model(f.token).Update("status", common.TokenStatusDisabled).Error)
	require.Equal(t, 401, f.request(t, "POST", "/v1/chat/completions", `{"model":"gpt-4","messages":[]}`).Code)
	require.Len(t, f.calls, 1)
}

// 新鲜明确耗尽时跳过账号 A，未知或过期观测则尝试一次真实请求。
// 上游 429 不证明没有执行，必须停止，不能向账号 B 或钱包重放。
// 第一次请求给账号 A 一个新鲜、覆盖整个账号且尚未重置的耗尽窗口。
// 只有这种明确的调用前依据才允许选择账号 B。
// 钱包同时存在，检查实际权限头可以识别任何意外的付费回退。
// 第二次移除观察依据，账号 A 应重新成为第一个执行候选。
// 执行层返回 429 只说明请求失败，不能证明原厂没有处理输入。
// 因此第二次请求必须结束并报告失败，而不能复制输入到其他账号。
// 两个阶段使用同一策略与正文，隔离耗尽证据和执行后失败的差别。
func TestResourceRelayExhaustionAndNoReplay(t *testing.T) {
	f := newResourceFixture(t)
	observed, reset := time.Now().UnixMilli(), time.Now().Add(time.Hour).UnixMilli()
	observation := hosting.Observation{State: "fresh", ObservedAtMS: &observed, Snapshot: &hosting.ObservationSnapshot{QuotaWindows: []hosting.QuotaWindow{{Scope: "account", State: "exhausted", ResetAtMS: &reset}}}}
	raw, err := common.Marshal(observation)
	require.NoError(t, err)
	f.accounts[0].ObservationJSON = string(raw)
	require.NoError(t, model.DB.Save(f.accounts[0]).Error)
	f.setPolicy(t, model.ResourceSource{Kind: "hosting", AccountID: f.accounts[0].ID, Enabled: true}, model.ResourceSource{Kind: "hosting", AccountID: f.accounts[1].ID, Enabled: true}, model.ResourceSource{Kind: "wallet", Enabled: true})
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`
	require.Equal(t, 200, f.request(t, "POST", "/v1/chat/completions", body).Code)
	require.Equal(t, []string{"Bearer internal-1"}, f.calls)
	// 移除明确耗尽依据，第一次请求被拒绝也不再尝试第二账号。
	f.accounts[0].ObservationJSON = ""
	require.NoError(t, model.DB.Save(f.accounts[0]).Error)
	f.calls = nil
	f.status = 429
	response := f.request(t, "POST", "/v1/chat/completions", body)
	require.Equal(t, 502, response.Code, response.Body.String())
	require.Equal(t, []string{"Bearer internal-0"}, f.calls)
}

// Key 自己的来源顺序决定调度，空数组表示禁用全部来源。
// 模型限制、未支持协议、重复 model 与异步请求都在上游调用前拒绝。
// 先保存可用来源，再保存空数组，证明旧来源不会被保留。
// 重新显式保存来源后继续校验输入，不通过删除触发隐式收费。
// 不支持的图片协议必须在统一鉴权入口阻断，不能落回旧付费通道。
// 后台异步请求不属于当前同步调度契约，应在发送前明确拒绝。
// 重复 model 字段存在解析歧义，不能让不同阶段使用不同模型名。
// Key 模型白名单同时约束推理入口、目录列表和单模型详情。
// 最后要求执行调用为空，证明所有输入拒绝都发生在外部执行之前。
func TestResourceRelayPolicyAndInputBoundaries(t *testing.T) {
	f := newResourceFixture(t)
	f.setPolicy(t, model.ResourceSource{Kind: "hosting", AccountID: f.accounts[0].ID, Enabled: true})
	f.setPolicy(t)
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`
	require.Equal(t, 503, f.request(t, "POST", "/v1/chat/completions", body).Code)
	f.setPolicy(t, model.ResourceSource{Kind: "hosting", AccountID: f.accounts[0].ID, Enabled: true})
	require.Equal(t, 400, f.request(t, "POST", "/v1/images/generations", body).Code)
	require.Equal(t, 400, f.request(t, "POST", "/v1/responses", `{"model":"gpt-4","input":"hi","background":true}`).Code)
	require.Equal(t, 400, f.request(t, "POST", "/v1/chat/completions", `{"model":"gpt-4","model":"forbidden","messages":[]}`).Code)
	require.NoError(t, model.DB.Model(f.token).Updates(map[string]any{"model_limits_enabled": true, "model_limits": "another-model"}).Error)
	require.Equal(t, 403, f.request(t, "POST", "/v1/chat/completions", body).Code)
	models := f.request(t, "GET", "/v1/models", "")
	require.Equal(t, 200, models.Code)
	require.NotContains(t, models.Body.String(), "gpt-4")
	require.Equal(t, 404, f.request(t, "GET", "/v1/models/gpt-4", "").Code)
	require.Empty(t, f.calls)
}

// SSE 转发保持单个来源，及时输出且不因结束后错误尝试下一个账号。
// 通过实际响应输出和调用次数确认流式请求不跨来源重复执行。
// Responses 流式正文由真实转发器写出，不能只把 JSON 成功当流式支持。
// 刷新标记证明响应具备及时输出行为，而不是全部缓冲后一次返回。
// 终止事件需要原样到达调用者，避免客户端等待永不结束。
// 第一次完成后更改数据库中的账号归属，模拟策略保存之后发生的变化。
// 执行时必须重新核对归属，不能把旧策略当作永久访问授权。
// 第二次请求即使仍引用原账号，也不能再次调用其内部 Key。
// 通过累计调用次数确认归属失败没有误转到任何其他来源。
func TestResourceRelayStreamAndOwnership(t *testing.T) {
	f := newResourceFixture(t)
	f.stream = true
	f.setPolicy(t, model.ResourceSource{Kind: "hosting", AccountID: f.accounts[0].ID, Enabled: true})
	response := f.request(t, "POST", "/v1/responses", `{"model":"gpt-4","input":"hi","stream":true}`)
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), "[DONE]")
	require.True(t, response.Flushed) // 验证实际输出行为，不能只依靠成功状态码。
	// 策略保存后所有权变化仍需在执行时重新检查，不能凭旧策略越权。
	require.NoError(t, model.DB.Model(f.accounts[0]).Update("user_id", f.user.Id+1).Error)
	require.Equal(t, 503, f.request(t, "POST", "/v1/responses", `{"model":"gpt-4","input":"hi"}`).Code)
	require.Len(t, f.calls, 1)
}

// 付费来源按用户顺序共存：无订阅时使用钱包，钱包禁用则保持余额。
// 验证实际 HTTP 适配器、预扣退款和消费日志，不以模拟计费替代真实链路。
// 真实余额与 Key 预算都足够，排除金额不足对来源选择的干扰。
// 用户明确排列订阅后钱包，因此无订阅时可以使用平台余额。
// 实际平台 Key 和来源响应头共同证明走了预期付费链路。
// 调用后比较用户与 Key 的余额，防止只扣其中一侧。
// 随后移除钱包来源，表示用户撤销了收费回退许可。
// 旧计费偏好不能把已撤销的钱包悄悄补进候选列表。
// 第二次请求没有上游调用且余额不变，证明撤销在执行前生效。
func TestResourceRelayPlatformFallbackAndNoHiddenWallet(t *testing.T) {
	f := newResourceFixture(t)
	require.NoError(t, model.DB.Model(f.user).Update("quota", 1_000_000).Error)
	require.NoError(t, model.DB.Model(f.token).Update("remain_quota", 1_000_000).Error)
	f.setPolicy(t, model.ResourceSource{Kind: "subscription", Enabled: true}, model.ResourceSource{Kind: "wallet", Enabled: true})
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}],"max_tokens":4}`
	response := f.request(t, "POST", "/v1/chat/completions", body)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, "wallet", response.Header().Get("X-Oak-Resource-Source"))
	require.Equal(t, []string{"Bearer platform-only"}, f.calls)
	quota, err := model.GetUserQuota(f.user.Id, false)
	require.NoError(t, err)
	require.Less(t, quota, 1_000_000)
	token, err := model.GetTokenById(f.token.Id)
	require.NoError(t, err)
	require.Equal(t, quota, token.RemainQuota) // 钱包与 Key 的记账结果需要一致。
	// 移除钱包即撤销回退授权，不能被旧 subscription_first 偏好补回来。
	f.setPolicy(t, model.ResourceSource{Kind: "subscription", Enabled: true})
	f.calls = nil
	response = f.request(t, "POST", "/v1/chat/completions", body)
	require.Equal(t, 503, response.Code, response.Body.String())
	require.Empty(t, f.calls)
	after, err := model.GetUserQuota(f.user.Id, false)
	require.NoError(t, err)
	require.Equal(t, quota, after) // 撤销钱包许可后不得产生隐藏收费。
}

// 平台预算不足后可以继续免费账号；平台已发送失败则不得再次执行。
// 退款结束后读取真实余额，确认没有残留预扣和双重扣款。
// 第一阶段钱包为零，确定的调用前预算不足可以继续本人免费账号。
// Key 自身仍有预算，避免把钱包不足和身份校验混在一起。
// 第二阶段补足钱包，让请求真实到达平台执行层。
// 执行层返回 500 后不能再把同一输入重放给后续免费账号。
// 失败响应之前应完成可确认的预扣退款，数据库余额不能残留差额。
// 用户钱包和 Key 预算都需要恢复，不能只修复其中一个计数。
// 两阶段的实际权限头区分调用前跳过与发送后停止的安全边界。
func TestResourceRelayPaidFailureBoundary(t *testing.T) {
	f := newResourceFixture(t)
	require.NoError(t, model.DB.Model(f.user).Update("quota", 0).Error)
	require.NoError(t, model.DB.Model(f.token).Update("remain_quota", 1_000_000).Error)
	f.setPolicy(t, model.ResourceSource{Kind: "wallet", Enabled: true}, model.ResourceSource{Kind: "hosting", AccountID: f.accounts[0].ID, Enabled: true})
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}],"max_tokens":4}`
	response := f.request(t, "POST", "/v1/chat/completions", body)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, []string{"Bearer internal-0"}, f.calls)
	require.NoError(t, model.DB.Model(f.user).Update("quota", 1_000_000).Error)
	f.status = 500
	f.calls = nil
	response = f.request(t, "POST", "/v1/chat/completions", body)
	require.Equal(t, 502, response.Code, response.Body.String())
	require.Equal(t, []string{"Bearer platform-only"}, f.calls)
	quota, err := model.GetUserQuota(f.user.Id, false)
	require.NoError(t, err)
	require.Equal(t, 1_000_000, quota)
	token, err := model.GetTokenById(f.token.Id)
	require.NoError(t, err)
	require.Equal(t, 1_000_000, token.RemainQuota)
}

// 真实测试记录的协议名来自执行层，不能把连字符误写成内部下划线常量。
// 目录只给实际测试过的模型标记 verified，供配置导出解锁。
// 先保存真实协议拼写，目录聚合必须识别执行层的公开协议名称。
// 账号目录中的模型存在不等于经过测试，验证标记还要匹配模型名。
// 同一账号只改变 verified_model，其他可用性条件保持不变。
// 第二次目录查询仍保留模型，但不再允许宣称该模型已经验证。
// 用真实策略与控制面目录构建结果，避免直接拼接展示对象。
// 验证时间只说明发生过一次测试，不能覆盖模型或协议不匹配。
// 此用例检查配置导出的能力依据，不会发起模型推理消耗原厂额度。
func TestResourceRelayVerifiedModelContract(t *testing.T) {
	f := newResourceFixture(t)
	f.setPolicy(t, model.ResourceSource{Kind: "hosting", AccountID: f.accounts[0].ID, Enabled: true})
	require.NoError(t, model.DB.Model(f.accounts[0]).Updates(map[string]any{"verified_at": time.Now().Unix(), "verified_model": "gpt-4", "verified_protocol": "openai-responses"}).Error)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/v1/models", nil)
	ctx.Set("id", f.user.Id)
	policy, err := model.GetEffectiveResourcePolicy(f.user.Id, f.token.Id)
	require.NoError(t, err)
	items, err := resourceModels(ctx, policy)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.True(t, items[0].Verified)
	require.NoError(t, model.DB.Model(f.accounts[0]).Update("verified_model", "different-model").Error)
	items, err = resourceModels(ctx, policy)
	require.NoError(t, err)
	require.False(t, items[0].Verified) // 其他模型的测试事实不能为当前模型背书。
}

// TestResourceRelayStaleObservationDoesNotSkip 防止过期耗尽快照越过当前免费账号。
// 原厂重置可能让窗口立即可用，不能只凭看似新鲜的时间戳继续信任旧状态。
// 主观测 stale 优先于窗口 exhausted；未来重置时间也不能恢复整份观察的可信度。
// 两个账号都可执行，仅靠真实转发的受限 Key 确认调度仍先选择账号 A。
// 这里不设置钱包兜底，避免付费是否成功掩盖错误跳过首选账号的问题。
// 观测来自执行层缓存接口，测试不直接调用耗尽判断函数自证实现。
// 同时持久化本地投影，确保夹具的控制面返回与正常管理路径一致。
// 单次成功调用后必须结束，账号 B 不应收到任何请求。
func TestResourceRelayStaleObservationDoesNotSkip(t *testing.T) {
	f := newResourceFixture(t)
	observed, reset := time.Now().UnixMilli(), time.Now().Add(time.Hour).UnixMilli()
	observation := hosting.Observation{State: "stale", ObservedAtMS: &observed, Snapshot: &hosting.ObservationSnapshot{QuotaWindows: []hosting.QuotaWindow{{Scope: "account", State: "exhausted", ResetAtMS: &reset}}}}
	raw, err := common.Marshal(observation)
	require.NoError(t, err)
	f.accounts[0].ObservationJSON = string(raw)
	require.NoError(t, model.DB.Save(f.accounts[0]).Error)
	f.setPolicy(t, model.ResourceSource{Kind: "hosting", AccountID: f.accounts[0].ID, Enabled: true}, model.ResourceSource{Kind: "hosting", AccountID: f.accounts[1].ID, Enabled: true})
	response := f.request(t, "POST", "/v1/chat/completions", `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, []string{"Bearer internal-0"}, f.calls) // 旧耗尽数据不能撤销用户对首选免费账号的使用顺序。
}

// TestResourceRelayPlatformCatalogProtocols 验证目录来自本人分组的实际同步能力。
// 同名模型在另一个分组支持 Responses，不能扩大当前组仅支持 Chat 的能力。
// 图片、向量、任务专用渠道和插件绑定都不能靠通用 OpenAI 默认值进入文本目录。
// 高级自定义渠道按模型匹配路由，不能把一个模型的路径借给另一个模型。
// 数据面列表和控制台配置导出必须使用同一权限与能力交集。
// 测试仅查询目录，不发送模型请求，因此不能消耗任何真实或模拟推理额度。
// 用户允许未定价模型，确保目录排除来自能力约束而不是价格配置缺失。
// 普通 OpenAI 渠道同时提供文本、图片和向量模型，避免只测试渠道类型过滤。
// 另建通用任务渠道，用无特殊名称的模型证明不能只靠模型名前缀排除任务。
// 再建带单插件绑定的 New API 渠道，验证普通渠道类型也不能掩盖任务用途。
// 可用的 New API 文本渠道应保留三种同步协议，避免修复时一刀切掉平台模型。
// 高级自定义渠道分别声明 Chat、Responses 和图片路径，验证模型级路由匹配。
// 同名模型在其他分组支持更多协议，是全站缓存误合并的直接复现条件。
// 另一个仅存在于其他组的模型必须完全不可见，不能只裁剪它的协议。
// 先调用控制台 Key 配置处理器，覆盖用户设置、Key 归属与独立策略的实际组合。
// 再通过真实 Key 调用 OpenAI 目录，证明两个入口没有不同的能力来源。
// 对同名模型检查 supported_endpoint_types，防止展示元数据重新引入全站能力。
// 所有平台模型都应保持未验证，渠道配置并不是本人账号实测结果。
// 最后切换为免费来源并查询单模型，确认此次裁剪没有改变原挂靠能力。
// 整组不发送推理请求，调用计数为空也是目录无原厂消费副作用的证据。
func TestResourceRelayPlatformCatalogProtocols(t *testing.T) {
	f := newResourceFixture(t)
	// 绕开定价门槛以隔离能力判断，不能让未定价偶然掩盖目录泄露。
	require.NoError(t, model.UpdateUserSetting(f.user.Id, dto.UserSetting{AcceptUnsetRatioModel: true}))
	f.setPolicy(t, model.ResourceSource{Kind: "wallet", Enabled: true})
	channels := []model.Channel{
		{Type: constant.ChannelTypeOpenAI, Models: "gpt-image-1,text-embedding-3-small,o3-pro", Group: "default"},
		{Type: constant.ChannelTypeTaskPlugin, Models: "catalog-task", Group: "default"},
		{Type: constant.ChannelTypeNewAPI, Models: "catalog-bound-task", Group: "default"},
		{Type: constant.ChannelTypeNewAPI, Models: "catalog-full", Group: "default"},
		{Type: constant.ChannelTypeAdvancedCustom, Models: "catalog-chat,catalog-response,catalog-shared,catalog-image", Group: "default"},
		{Type: constant.ChannelTypeAdvancedCustom, Models: "catalog-shared,catalog-foreign", Group: "other-user-group"},
	}
	channels[2].SetSetting(dto.ChannelSettings{TaskPluginKey: "test-task-plugin"})
	// 本组的不同模型具有不同路由，只有指定模型才能列出对应同步协议。
	channels[4].SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
		{IncomingPath: "/v1/chat/completions", UpstreamPath: "/chat", Models: []string{"catalog-chat", "catalog-shared"}},
		{IncomingPath: "/v1/responses", UpstreamPath: "/responses", Models: []string{"catalog-response"}},
		{IncomingPath: "/v1/images/generations", UpstreamPath: "/images", Models: []string{"catalog-image"}},
	}}})
	// 其他用户组提供同名模型更强的协议，专门检测全站能力并集污染。
	channels[5].SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
		{IncomingPath: "/v1/responses", UpstreamPath: "/responses"},
		{IncomingPath: "/v1/responses/compact", UpstreamPath: "/compact"},
	}}})
	for index := range channels {
		channels[index].Key = "catalog-only"
		channels[index].Name = fmt.Sprintf("catalog-channel-%d", index)
		channels[index].Status = common.ChannelStatusEnabled
		require.NoError(t, channels[index].Insert())
	}
	// 使用真实控制台处理器构造导出结果，避免直接拼 DTO 验证自身实现。
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest("GET", fmt.Sprintf("/api/hosting/tokens/%d/models", f.token.Id), nil)
	c.Params = gin.Params{{Key: "token_id", Value: fmt.Sprint(f.token.Id)}}
	c.Set("id", f.user.Id)
	GetHostingTokenModels(c)
	require.Equal(t, 200, response.Code, response.Body.String())
	var catalog struct {
		Data struct {
			Models []resourceModel `json:"models"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &catalog))
	expected := map[string][]string{
		"gpt-4":            {"chat_completions"},
		"o3-pro":           {"responses"},
		"catalog-chat":     {"chat_completions"},
		"catalog-response": {"responses"},
		"catalog-shared":   {"chat_completions"},
		"catalog-full":     {"responses", "chat_completions", "responses_compact"},
	}
	require.Len(t, catalog.Data.Models, len(expected)) // 不允许图片、embedding、任务或其他组模型混入。
	for _, item := range catalog.Data.Models {
		require.Contains(t, expected, item.ID)
		require.ElementsMatch(t, expected[item.ID], item.Protocols)
		require.False(t, item.Verified) // 平台渠道可用不能伪造本人挂靠实测事实。
	}
	// OpenAI 列表不能重新使用全站 supported_endpoint_types 把隔离能力又加回来。
	apiResponse := f.request(t, "GET", "/v1/models", "")
	require.Equal(t, 200, apiResponse.Code, apiResponse.Body.String())
	var apiModels struct {
		Data []dto.OpenAIModels `json:"data"`
	}
	require.NoError(t, common.Unmarshal(apiResponse.Body.Bytes(), &apiModels))
	require.Len(t, apiModels.Data, len(expected))
	for _, item := range apiModels.Data {
		require.Contains(t, expected, item.Id)
		if item.Id == "catalog-shared" || item.Id == "gpt-4" {
			require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, item.SupportedEndpointTypes)
		}
	}
	require.Empty(t, f.calls) // 能力目录不得通过偷偷执行模型请求来判断是否可用。
	// 最后开启原有免费账号，确认平台能力裁剪没有改动挂靠的协议目录。
	f.setPolicy(t, model.ResourceSource{Kind: "hosting", AccountID: f.accounts[0].ID, Enabled: true})
	privateResponse := f.request(t, "GET", "/v1/models/gpt-4", "")
	require.Equal(t, 200, privateResponse.Code)
	var privateModel dto.OpenAIModels
	require.NoError(t, common.Unmarshal(privateResponse.Body.Bytes(), &privateModel))
	require.ElementsMatch(t, []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse, constant.EndpointTypeOpenAIResponseCompact}, privateModel.SupportedEndpointTypes)
}
