package middleware

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMatchesExpectedTaskPluginUsesGenericChannelSetting(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeTaskPlugin}
	channel.SetSetting(dto.ChannelSettings{TaskPluginKey: "generic-alpha"})

	assert.True(t, channelMatchesExpectedTaskPlugin(nil, channel, "generic-alpha"))
	assert.False(t, channelMatchesExpectedTaskPlugin(nil, channel, "generic-beta"))
	assert.False(t, channelMatchesExpectedTaskPlugin(nil, channel, ""))
}

func TestChannelMatchesExpectedTaskPluginUsesPinnedLegacyIndex(t *testing.T) {
	registry := jsplugin.NewRegistry()
	alpha, err := registry.Register(distributorTaskPluginSource("legacy-alpha", constant.ChannelTypeKling), jsplugin.Options{})
	require.NoError(t, err)
	pinnedGeneration := registry.Generation()

	require.NoError(t, registry.Unregister("legacy-alpha"))
	_, err = registry.Register(distributorTaskPluginSource("legacy-beta", constant.ChannelTypeKling), jsplugin.Options{})
	require.NoError(t, err)

	c, _ := gin.CreateTestContext(nil)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{
		Generation: pinnedGeneration,
		Plugin:     alpha,
	})
	channel := &model.Channel{Type: constant.ChannelTypeKling}

	assert.True(t, channelMatchesExpectedTaskPlugin(c, channel, "legacy-alpha"))
	assert.False(t, channelMatchesExpectedTaskPlugin(c, channel, "legacy-beta"))
	assert.False(t, channelMatchesExpectedTaskPlugin(c, &model.Channel{Type: constant.ChannelTypeJimeng}, "legacy-alpha"))
}

func TestChannelMatchesExpectedTaskPluginRejectsUnindexedLegacyChannel(t *testing.T) {
	registry := jsplugin.NewRegistry()
	plugin, err := registry.Register(distributorTaskPluginSource("legacy-alpha", constant.ChannelTypeKling), jsplugin.Options{})
	require.NoError(t, err)

	c, _ := gin.CreateTestContext(nil)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{
		Generation: registry.Generation(),
		Plugin:     plugin,
	})

	assert.False(t, channelMatchesExpectedTaskPlugin(c, &model.Channel{Type: constant.ChannelTypeJimeng}, "legacy-alpha"))
	assert.False(t, channelMatchesExpectedTaskPlugin(c, &model.Channel{Type: 0}, "legacy-alpha"))
	assert.True(t, channelMatchesExpectedTaskPlugin(c, &model.Channel{Type: constant.ChannelTypeJimeng}, ""))
	assert.False(t, channelMatchesExpectedTaskPlugin(nil, &model.Channel{Type: constant.ChannelTypeKling}, "legacy-alpha"))

	c.Set("expected_task_plugin_key", "legacy-alpha")
	setupErr := SetupContextForSelectedChannel(c, &model.Channel{Type: constant.ChannelTypeJimeng}, "task-model")
	require.NotNil(t, setupErr)
	assert.Contains(t, setupErr.Error(), "does not match")
}

func TestSharedEndpointRebindsToSelectedLegacyProvider(t *testing.T) {
	registry := jsplugin.NewRegistry()
	_, err := registry.Register(distributorEndpointPluginSource("gemini-shared", constant.ChannelTypeGemini), jsplugin.Options{})
	require.NoError(t, err)
	_, err = registry.Register(distributorEndpointPluginSource("vertex-shared", constant.ChannelTypeVertexAi), jsplugin.Options{})
	require.NoError(t, err)
	candidates := registry.Generation().LookupEndpointCandidates("POST", "/v1/responses", "task-model")
	require.Len(t, candidates, 2)

	c, _ := gin.CreateTestContext(nil)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: registry.Generation(), Plugin: candidates[0].Plugin})
	c.Set(jsplugin.ContextKeyPinnedEndpoint, jsplugin.PinnedEndpoint{
		Generation: registry.Generation(),
		Plugin:     candidates[0].Plugin,
		Protocol:   candidates[0].Protocol,
		Operation:  candidates[0].Operation,
		Model:      "task-model",
		Candidates: candidates,
	})
	c.Set("expected_task_plugin_key", candidates[0].Plugin.Meta.Key)

	geminiChannel := &model.Channel{Id: 1, Type: constant.ChannelTypeGemini}
	vertexChannel := &model.Channel{Id: 2, Type: constant.ChannelTypeVertexAi}
	assert.True(t, channelMatchesExpectedTaskPlugin(c, geminiChannel, candidates[0].Plugin.Meta.Key))
	assert.True(t, channelMatchesExpectedTaskPlugin(c, vertexChannel, candidates[0].Plugin.Meta.Key))
	assert.False(t, channelMatchesExpectedTaskPlugin(c, &model.Channel{Type: constant.ChannelTypeKling}, candidates[0].Plugin.Meta.Key))

	require.Nil(t, SetupContextForSelectedChannel(c, vertexChannel, "task-model"))
	pinnedValue, exists := c.Get(jsplugin.ContextKeyPinnedEndpoint)
	require.True(t, exists)
	pinned, ok := pinnedValue.(jsplugin.PinnedEndpoint)
	require.True(t, ok)
	assert.Equal(t, "vertex-shared", pinned.Plugin.Meta.Key)
	assert.Equal(t, "vertex-shared", c.GetString("expected_task_plugin_key"))
	assert.Equal(t, "vertex-shared", c.GetString("task_plugin_key"))
	assert.True(t, channelMatchesExpectedTaskPlugin(c, geminiChannel, "vertex-shared"), "a retry may select another declared provider")
}

func distributorTaskPluginSource(key string, channelType int) string {
	return fmt.Sprintf(`
export const meta = {
  apiVersion: 1,
  key: %q,
  name: %q,
  version: "1.0.0",
  author: {name: "Test"},
  channelTypes: [%d],
  models: ["task-model"],
  fetchMode: "per_task",
};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {taskId: "task"}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
`, key, key, channelType)
}

func distributorEndpointPluginSource(key string, channelType int) string {
	return fmt.Sprintf(`
export const meta = {
  apiVersion: 1,
  key: %q,
  name: %q,
  version: "1.0.0",
  author: {name: "Test"},
  channelTypes: [%d],
  models: ["task-model"],
  fetchMode: "per_task",
  protocols: [{name: "openai_responses", supports: ["stream", "sync", "background"]}],
};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {taskId: "task"}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
export const protocols = {openai_responses: {
  decodeRequest: function(ctx) { return {kind: "submit", model: "task-model", requestBody: ctx.body.value}; },
  renderEvents: function() { return {events: [], state: null, done: false}; },
  renderFinal: function() { return {output: []}; },
}};
`, key, key, channelType)
}

func TestTokenModelLimitAllowsLegacyAliasAndModifierVariant(t *testing.T) {
	aliasOnly := map[string]bool{"claude-3-7-sonnet-thinking": true}
	assert.True(t, TokenModelLimitAllows(aliasOnly, "claude-3-7-sonnet-thinking"))
	assert.False(t, TokenModelLimitAllows(aliasOnly, "claude-3-7-sonnet"))

	baseOnly := map[string]bool{"claude-3-7-sonnet": true}
	assert.True(t, TokenModelLimitAllows(baseOnly, "claude-3-7-sonnet@thinking:on"))
	assert.True(t, TokenModelLimitAllows(baseOnly, "claude-3-7-sonnet-thinking"))

	wildcard := map[string]bool{"gemini-2.5-flash-thinking-*": true}
	assert.True(t, TokenModelLimitAllows(wildcard, "gemini-2.5-flash-thinking-8192"))
}

// /v1/tools/search and /v1/tools/fetch always route and bill by the DeepSeek
// routing names. A model named in the body is ignored: a chat model there
// would price the call at zero tokens.
func TestGetModelRequestAssignsDeepSeekToolRoutingNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ path, body, want string }{
		{"/v1/tools/search", `{"query":"deepseek","count":3}`, constant.DeepSeekSearchModel},
		{"/v1/tools/fetch", `{"url":"https://example.com/"}`, constant.DeepSeekFetchModel},
		{"/v1/tools/search", `{"query":"deepseek","model":"deepseek-flash"}`, constant.DeepSeekSearchModel},
		{"/v1/tools/fetch", `{"url":"https://example.com/","model":"deepseek-v4-pro"}`, constant.DeepSeekFetchModel},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
		c.Request.Header.Set("Content-Type", "application/json")
		modelRequest, shouldSelectChannel, err := getModelRequest(c)
		require.NoError(t, err, tc.body)
		assert.True(t, shouldSelectChannel, tc.body)
		assert.Equal(t, tc.want, modelRequest.Model, tc.body)
	}
}

func TestTokenModelLimitAllowsExemptAtNameByFullName(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	original := append([]string(nil), settings.ThinkingModelBlacklist...)
	t.Cleanup(func() { settings.ThinkingModelBlacklist = original })
	settings.ThinkingModelBlacklist = append(original, "re:.*@sha256:.*")

	fullOnly := map[string]bool{"opaque@sha256:deadbeef": true}
	assert.True(t, TokenModelLimitAllows(fullOnly, "opaque@sha256:deadbeef"))

	baseOnly := map[string]bool{"opaque": true}
	assert.False(t, TokenModelLimitAllows(baseOnly, "opaque@sha256:deadbeef"))
}

func TestDistributeHidesTaskPluginDetailsButLogsDiagnostics(t *testing.T) {
	require.NoError(t, i18n.Init())
	previousCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousCacheEnabled })

	const group = "private-plugin-error-test-group"
	for _, locale := range []struct{ language, message string }{
		{"en", "No available channel for model task-model under group " + group + ": the model is claimed by a task plugin, which has no enabled channel serving it (distributor)"},
		{"zh-CN", "分组 " + group + " 下模型 task-model 无可用渠道：该模型由任务插件认领，但当前没有启用的渠道可服务此模型（distributor）"},
		{"zh-TW", "分組 " + group + " 下模型 task-model 無可用管道：該模型由任務插件認領，但目前沒有啟用的管道可服務此模型（distributor）"},
	} {
		for _, providerCount := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/%d_providers", locale.language, providerCount), func(t *testing.T) {
				registry := jsplugin.NewRegistry()
				keys := []string{"private-provider-alpha", "private-provider-beta"}[:providerCount]
				for index, key := range keys {
					_, err := registry.Register(distributorEndpointPluginSource(key, constant.ChannelTypeKling+index), jsplugin.Options{})
					require.NoError(t, err)
				}
				generation := registry.Generation()
				candidates := generation.LookupEndpointCandidates("POST", "/v1/responses", "task-model")
				require.Len(t, candidates, providerCount)

				var logs bytes.Buffer
				common.LogWriterMu.Lock()
				previousWriter := gin.DefaultErrorWriter
				gin.DefaultErrorWriter = &logs
				common.LogWriterMu.Unlock()
				t.Cleanup(func() {
					common.LogWriterMu.Lock()
					gin.DefaultErrorWriter = previousWriter
					common.LogWriterMu.Unlock()
				})

				router := gin.New()
				router.POST("/v1/responses", RequestId(), func(c *gin.Context) {
					common.SetContextKey(c, constant.ContextKeyUsingGroup, group)
					c.Set("resolved_task_model", "task-model")
					c.Set("expected_task_plugin_key", keys[0])
					c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: generation, Plugin: candidates[0].Plugin})
					if providerCount > 1 {
						c.Set(jsplugin.ContextKeyPinnedEndpoint, jsplugin.PinnedEndpoint{
							Generation: generation, Plugin: candidates[0].Plugin,
							Protocol: candidates[0].Protocol, Operation: candidates[0].Operation,
							Model: "task-model", Candidates: candidates,
						})
					}
				}, Distribute(), func(c *gin.Context) {
					t.Error("unavailable requests must stop before the relay handler")
					c.Status(http.StatusNoContent)
				})
				request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				request.Header.Set("Accept-Language", locale.language)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)

				require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
				requestID := recorder.Header().Get(common.RequestIdKey)
				require.NotEmpty(t, requestID)
				assert.JSONEq(t, fmt.Sprintf(`{"error":{"message":%q,"type":"new_api_error","code":"model_not_found"}}`,
					locale.message+" (request id: "+requestID+")"), recorder.Body.String())
				assert.NotContains(t, recorder.Body.String(), "disable or override")
				for _, key := range keys {
					assert.NotContains(t, recorder.Body.String(), key)
					assert.Contains(t, logs.String(), key)
				}
				assert.Contains(t, logs.String(), requestID)
				assert.Contains(t, logs.String(), `group="`+group+`"`)
				assert.Contains(t, logs.String(), `model="task-model"`)
				assert.Contains(t, logs.String(), "reason=no_eligible_channel")
			})
		}
	}
}

func TestNoAvailableChannelMessageWithoutPlugin(t *testing.T) {
	require.NoError(t, i18n.Init())
	plain, _ := gin.CreateTestContext(nil)
	plain.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	plain.Request.Header.Set("Accept-Language", "en")
	generic := noAvailableChannelMessage(plain, "default", "gpt-4o")
	assert.NotContains(t, generic, "task plugin")
	assert.Contains(t, generic, "gpt-4o")
}

func TestSharedEndpointRebindsToSelectedType61Plugin(t *testing.T) {
	registry := jsplugin.NewRegistry()
	for _, key := range []string{"alpha", "beta"} {
		source := strings.Replace(distributorEndpointPluginSource(key, 0), "channelTypes: [0],", "", 1)
		_, err := registry.Register(source, jsplugin.Options{})
		require.NoError(t, err)
	}
	generation := registry.Generation()
	candidates := generation.LookupEndpointCandidates("POST", "/v1/responses", "task-model")
	require.Len(t, candidates, 2)
	c, _ := gin.CreateTestContext(nil)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: generation, Plugin: candidates[0].Plugin})
	c.Set(jsplugin.ContextKeyPinnedEndpoint, jsplugin.PinnedEndpoint{Generation: generation, Plugin: candidates[0].Plugin, Protocol: candidates[0].Protocol, Operation: candidates[0].Operation, Model: "task-model", Candidates: candidates})
	c.Set("expected_task_plugin_key", "alpha")
	channel := &model.Channel{Id: 2, Type: constant.ChannelTypeTaskPlugin}
	channel.SetSetting(dto.ChannelSettings{TaskPluginKey: "unrelated"})
	assert.False(t, channelMatchesExpectedTaskPlugin(c, channel, "alpha"))
	channel.SetSetting(dto.ChannelSettings{TaskPluginKey: "beta"})
	require.Nil(t, SetupContextForSelectedChannel(c, channel, "task-model"))
	assert.Equal(t, "beta", c.GetString("task_plugin_key"))
	assert.Equal(t, "beta", c.GetString("expected_task_plugin_key"))
	assert.Equal(t, "beta", c.MustGet(jsplugin.ContextKeyPinnedEndpoint).(jsplugin.PinnedEndpoint).Plugin.Meta.Key)
}

func TestChannelMatchesExpectedTaskPluginUsesNewAPIExtensions(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeNewAPI}
	channel.SetSetting(dto.ChannelSettings{TaskPluginKey: "single", TaskExtendPluginKeys: []string{"ext-alpha", "ext-beta"}})

	assert.True(t, channelMatchesExpectedTaskPlugin(nil, channel, "ext-alpha"))
	assert.True(t, channelMatchesExpectedTaskPlugin(nil, channel, "single"), "the single key stays valid on a New API channel")
	assert.False(t, channelMatchesExpectedTaskPlugin(nil, channel, "ext-gamma"))
	assert.True(t, channelMatchesExpectedTaskPlugin(nil, channel, ""), "requests without a pinned plugin still use the gateway")
	assert.False(t, channelMatchesExpectedTaskPlugin(nil, &model.Channel{Type: constant.ChannelTypeNewAPI}, "ext-alpha"))
}

func TestSharedEndpointRebindsToBoundNewAPIExtension(t *testing.T) {
	registry := jsplugin.NewRegistry()
	for _, key := range []string{"alpha", "beta"} {
		source := strings.Replace(distributorEndpointPluginSource(key, 0), "channelTypes: [0],", "", 1)
		_, err := registry.Register(source, jsplugin.Options{})
		require.NoError(t, err)
	}
	generation := registry.Generation()
	candidates := generation.LookupEndpointCandidates("POST", "/v1/responses", "task-model")
	require.Len(t, candidates, 2)
	pin := func(expected string) *gin.Context {
		pinned := candidates[0]
		if candidates[1].Plugin.Meta.Key == expected {
			pinned = candidates[1]
		}
		c, _ := gin.CreateTestContext(nil)
		c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: generation, Plugin: pinned.Plugin})
		c.Set(jsplugin.ContextKeyPinnedEndpoint, jsplugin.PinnedEndpoint{Generation: generation, Plugin: pinned.Plugin, Protocol: pinned.Protocol, Operation: pinned.Operation, Model: "task-model", Candidates: candidates})
		c.Set("expected_task_plugin_key", expected)
		return c
	}

	channel := &model.Channel{Id: 3, Type: constant.ChannelTypeNewAPI}
	channel.SetSetting(dto.ChannelSettings{TaskExtendPluginKeys: []string{"unrelated"}})
	assert.False(t, channelMatchesExpectedTaskPlugin(pin("alpha"), channel, "alpha"))

	channel.SetSetting(dto.ChannelSettings{TaskExtendPluginKeys: []string{"beta"}})
	c := pin("alpha")
	require.Nil(t, SetupContextForSelectedChannel(c, channel, "task-model"))
	assert.Equal(t, "beta", c.GetString("task_plugin_key"), "the only bound candidate executes")
	assert.Equal(t, "beta", c.GetString("expected_task_plugin_key"))
	assert.Equal(t, "beta", c.MustGet(jsplugin.ContextKeyPinnedEndpoint).(jsplugin.PinnedEndpoint).Plugin.Meta.Key)

	channel.SetSetting(dto.ChannelSettings{TaskExtendPluginKeys: []string{"alpha", "beta"}})
	c = pin("beta")
	require.Nil(t, SetupContextForSelectedChannel(c, channel, "task-model"))
	assert.Equal(t, "alpha", c.GetString("task_plugin_key"), "the first bound candidate executes regardless of the earlier pin")
	assert.Equal(t, "alpha", c.MustGet(jsplugin.ContextKeyPinnedEndpoint).(jsplugin.PinnedEndpoint).Plugin.Meta.Key)
}

// 协议边界验证真实正文重写，包括二进制文件和重复字段拒绝。
// 此处只隔离认证上下文，分发及计费由 controller 的真实请求测试覆盖。
// 每个用例同时验证存储正文与读取模型，防止只改路由字符串而未改上游输入。
// JSON 场景包含显式零值和 false，防止重编码时丢失客户端语义。
// 嵌套 metadata.model 不是路由字段，不能被递归替换。
// 表单模型可能百分号编码，先解码再匹配才能与客户端意图一致。
// 图片上传使用包含非 UTF-8 字节的文件，不能按字符串编码后回传。
// multipart 的 prompt 字段用于验证非模型文本没有丢失。
// 文件名和字段名分别进入真实 ParseMultipartForm 路径。
// 正文替换后的读回必须来自 BodyStorage，不能仅靠 Request.Body。
// 未命中模型保持原始字节，既有签名或扩展字段不应被无故重排。
// 固定工具接口不把正文 model 当作价格身份，保留原请求。
// Gemini 的动作后缀保持 generateContent，不应成为映射名称的一部分。
// Realtime 使用 GET 查询参数，不能落入 POST 正文解析逻辑。
// Realtime 断言使用 Gin 的公开 Query API，保护查询缓存一致性。
// 重复 JSON 模型拒绝转发，不能由不同解码器选中不同值。
// 重复表单模型同样拒绝，不把 JSON 防护错误地视为覆盖全部协议。
// 所有用例走真实 Gin 中间件链，Abort 必须阻止后续处理。
// 错误状态通过 HTTP 记录器检查，不直接调用私有解析器。
// 没有配置模型价格，因为此层不负责收费计算。
// 权限与钱包结算由控制器层的行为测试单独覆盖。
// 请求体清理中间件在每个用例结束后释放存储。
// 测试文件流显式关闭，避免上传用例泄漏文件描述符。
// 临时 MultipartForm 文件也显式清理，不能污染后续用例。
// 每个用例使用新引擎，避免 Gin 路由参数或缓存跨请求复用。
// 模型规则包含中间节点，测试最终目标而不是只替换一次。
// 原始正文由测试构造，预期目标只在断言中给出。
// 测试没有复制映射算法，所有改写均调用生产中间件。
// 不同协议共享相同配置，防止 UI 保存后只在某一种协议生效。
// 不为协议自动补默认模型，省略模型的解释仍属于既有处理器。
// 测试请求不会触发真实上游，便于安全检查二进制与坏输入。
// 请求中其他字段无需符合完整模型 API，因为此层仅验证模型提取合同。
// 错误路径未断言具体语言，允许服务端根据请求语言翻译错误。
// 成功路径验证精确目标拼写，不能被大小写规范化改变。
// 组配置不在测试上下文中设置，模型改写不应暗中补分组。
// 路由路径去掉查询部分后注册，但实际请求保留原查询。
// 文件内容和非模型字段同时检查，避免只验证单一字段导致遗漏。
func TestTokenModelMappingProtocolBodies(t *testing.T) {
	// 独立运行也要加载错误翻译，不能依赖其他用例偶然初始化全局 bundle。
	require.NoError(t, i18n.Init())
	var multipartBody bytes.Buffer
	writer := multipart.NewWriter(&multipartBody)
	require.NoError(t, writer.WriteField("model", "alias"))
	require.NoError(t, writer.WriteField("prompt", "keep this"))
	file, err := writer.CreateFormFile("image", "sample.png")
	require.NoError(t, err)
	_, err = file.Write([]byte{0, 1, 2, 255})
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	for _, tc := range []struct {
		name, path, contentType, body string
		status                        int
	}{
		{"json", "/v1/chat/completions", "application/json", `{"model":"alias","n":0,"stream":false,"metadata":{"model":"keep"}}`, 200},
		{"form", "/v1/audio/transcriptions", "application/x-www-form-urlencoded", "model=alias&prompt=keep+this", 200},
		{"multipart", "/v1/images/edits", writer.FormDataContentType(), multipartBody.String(), 200},
		{"gemini", "/v1beta/models/alias:generateContent", "application/json", `{"contents":[]}`, 200},
		{"realtime", "/v1/realtime?model=alias", "", "", 200},
		{"unmatched", "/v1/chat/completions", "application/json", `{"model":"unmatched"}`, 200},
		{"duplicate json", "/v1/chat/completions", "application/json", `{"model":"alias","model":"other"}`, 400},
		{"duplicate form", "/v1/audio/transcriptions", "application/x-www-form-urlencoded", "model=alias&model=other", 400},
		{"fixed tools", "/v1/tools/search", "application/json", `{"model":"alias","query":"keep"}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			engine.Use(BodyStorageCleanup(), func(c *gin.Context) {
				common.SetContextKey(c, constant.ContextKeyTokenModelMapping, `{"alias":"middle","middle":"target"}`)
				c.Next()
			}, TokenModelMapping())
			method := http.MethodPost
			if tc.name == "realtime" {
				method = http.MethodGet
			}
			route, _, _ := strings.Cut(tc.path, "?")
			engine.Handle(method, route, func(c *gin.Context) {
				if tc.name == "gemini" {
					require.Equal(t, "/v1beta/models/target:generateContent", c.Request.URL.Path)
					return
				}
				if tc.name == "realtime" {
					require.Equal(t, "target", c.Query("model"))
					return
				}
				raw, readErr := io.ReadAll(c.Request.Body)
				require.NoError(t, readErr)
				if tc.name == "multipart" {
					c.Request.Body = io.NopCloser(bytes.NewReader(raw))
					require.NoError(t, c.Request.ParseMultipartForm(1024))
					defer c.Request.MultipartForm.RemoveAll()
					require.Equal(t, "target", c.Request.FormValue("model"))
					require.Equal(t, "keep this", c.Request.FormValue("prompt"))
					image, _, openErr := c.Request.FormFile("image")
					require.NoError(t, openErr)
					defer image.Close()
					data, readErr := io.ReadAll(image)
					require.NoError(t, readErr)
					require.Equal(t, []byte{0, 1, 2, 255}, data)
				} else if tc.name == "unmatched" || tc.name == "fixed tools" {
					require.Equal(t, tc.body, string(raw))
					require.Empty(t, common.GetContextKeyString(c, constant.ContextKeyTokenTargetModel))
				} else {
					require.Contains(t, string(raw), "target")
					if tc.name == "json" {
						require.Contains(t, string(raw), `"n":0`)
						require.Contains(t, string(raw), `"stream":false`)
						require.Contains(t, string(raw), `"metadata":{"model":"keep"}`)
					}
					storage, readErr := common.GetBodyStorage(c)
					require.NoError(t, readErr)
					stored, readErr := storage.Bytes()
					require.NoError(t, readErr)
					require.Equal(t, raw, stored)
				}
			})
			request := httptest.NewRequest(method, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", tc.contentType)
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
		})
	}
}
