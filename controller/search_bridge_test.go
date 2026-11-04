package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// newSearchBridgeFixture 使用真实账户、Key、价格与来源策略。
// 控制面和数据面的外部边界由既有资源夹具隔离。
// 所有配置在清理阶段恢复，不依赖其他测试运行顺序。
func newSearchBridgeFixture(t *testing.T) *resourceFixture {
	t.Helper()
	f := newResourceFixture(t) // 沿用项目三库可选夹具，不复制钱包或 Key 实现。
	// Redis 性能聚合属于异步旁路，不在搜索账单验收范围内。
	// 关闭旁路避免夹具清理全局 DB 配置时仍有指标任务读取它。
	perf := config.GlobalConfig.Get("perf_metrics_setting").(*perf_metrics_setting.PerfMetricsSetting)
	oldPerf := perf.Enabled
	perf.Enabled = false
	t.Cleanup(func() { perf.Enabled = oldPerf })
	oldTimeout, oldLog := constant.StreamingTimeout, common.LogConsumeEnabled // 保存全局配置，定向运行与全包运行必须一致。
	constant.StreamingTimeout, common.LogConsumeEnabled = 30, true            // 夹具不运行 main，显式初始化流处理器需要的超时。
	t.Cleanup(func() { constant.StreamingTimeout, common.LogConsumeEnabled = oldTimeout, oldLog })
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil { // 支持独立运行，不能假定其他测试已初始化配置。
		common.OptionMap = map[string]string{}
	}
	old := common.OptionMap[operation_setting.SearchBridgeRoutesOptionKey] // 恢复原选项，防止后续普通映射用例意外进入桥接。
	common.OptionMap[operation_setting.SearchBridgeRoutesOptionKey] = `{"kimi-k3":"gpt-6-luna"}`
	common.OptionMapRWMutex.Unlock() // 测试与运行时遵守同一并发读取合同。
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap[operation_setting.SearchBridgeRoutesOptionKey] = old
		common.OptionMapRWMutex.Unlock()
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"kimi-k3":0.02,"gpt-6-luna":0.03}`)) // 不同模型单价让错用目标价格可被余额断言发现。
	require.NoError(t, model.DB.Model(f.user).Update("quota", 1_000_000).Error)                          // 余额足够完成正常路径，失败用例再主动缩小。
	f.token.RemainQuota = 1_000_000                                                                      // Key 配额与钱包是独立约束，两者都初始化。
	f.token.ModelLimitsEnabled, f.token.ModelLimits = true, "kimi-k3,gpt-6-luna"                         // 只授予目标和搜索模型权限，源别名不必有计费渠道。
	f.token.ModelMapping = `{"gpt-6.1-terra":"kimi-k3"}`                                                 // 固定客户端模型，通过真实 Key 配置完成选择。
	_, err := f.token.UpdateWithResourcePolicy(nil)                                                      // 使用正式更新路径触发缓存失效。
	require.NoError(t, err)
	f.setPolicy(t, model.ResourceSource{Kind: "wallet", Enabled: true}) // 钱包为用户明确授权来源，不是桥接自动追加。

	f.engine = gin.New() // 测试经过实际 HTTP 中间件链，而非直接注入 RelayInfo。
	f.engine.Use(middleware.BodyStorageCleanup(), middleware.TokenAuth(), middleware.TokenModelMapping())
	f.engine.POST("/v1/responses", ResponsesSearchBridge(), ResourceRelay(), middleware.Distribute(), func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIResponses) })
	return f
}

// TestSearchBridgeThroughRelay 经过真实 Key 鉴权、路由及钱包结算。
// 本地供应商只替代外部 HTTP，不替代桥接或计费实现。
// 同时包含内部搜索和客户端函数，保护两类工具的执行归属。
// 搜索证据来自上游事件，不允许凭空构造调用成功。
// 两个模型使用不同价格，错误合并用量会导致余额断言失败。
// 配置在每个用例中恢复，不写生产配置或读取真实密钥。
func TestSearchBridgeThroughRelay(t *testing.T) {
	for _, stream := range []bool{false, true} { // 同一业务流程必须同时支持 JSON 与 SSE 客户端。
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			f := newSearchBridgeFixture(t)
			// 模型级工具价格覆盖与显式零价都走既有计费器。
			// 不能在桥接中硬编码一次搜索的费用或收费模型。
			searchPrice := 0.0
			if stream {
				searchPrice = 17
			}
			operation_setting.SetToolPriceForTest("web_search:gpt-6-luna*", searchPrice)
			t.Cleanup(func() { operation_setting.DeleteToolPriceForTest("web_search:gpt-6-luna*") })
			var models []string // 只观察数据面实际调用，不把控制面目录查询算作执行。
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				require.NoError(t, common.DecodeJson(r.Body, &body))
				name := body["model"].(string) // 上游实际收到的模型才是路由行为证据。
				models = append(models, name)
				var output []any     // 下一轮仅使用公开输出，不读取桥接内部历史。
				switch len(models) { // 调用顺序约束搜索结果必须回到原目标模型。
				case 1:
					require.Equal(t, "kimi-k3", name) // 源别名不能泄漏到目标供应商。
					raw, _ := common.Marshal(body["tools"])
					require.Contains(t, string(raw), `"name":"oak_web_search"`) // 目标需要一个可执行函数，而不是无效的托管声明。
					require.Contains(t, string(raw), `"name":"exec_command"`)   // 搜索适配不能删除用户原有的任务工具。
					require.NotContains(t, string(raw), `"type":"web_search"`)  // Kimi 不再收到其无法实际执行的工具类型。
					output = []any{gin.H{"type": "function_call", "id": "fc_search", "call_id": "call_search", "name": "oak_web_search", "arguments": `{"query":"Bun latest release"}`, "status": "completed"}}
				case 2: // 第二次调用才是付费搜索，不能先搜后问目标模型。
					require.Equal(t, "gpt-6-luna", name) // 搜索按单独模型选择与计费，不伪装成 Kimi。
					require.Equal(t, []any{map[string]any{"type": "web_search", "external_web_access": false, "filters": map[string]any{"allowed_domains": []any{"bun.sh"}}}}, body["tools"])
					output = []any{gin.H{"type": "web_search_call", "id": "ws_real", "status": "completed", "action": gin.H{"type": "search", "query": "Bun latest release", "sources": []any{gin.H{"type": "url", "url": "https://bun.sh/blog", "title": "Bun"}}}}, gin.H{"type": "message", "id": "msg_evidence", "role": "assistant", "status": "completed", "content": []any{gin.H{"type": "output_text", "text": "Source: https://bun.sh/blog", "annotations": []any{gin.H{"type": "url_citation", "url": "https://bun.sh/blog", "title": "Bun", "start_index": 8, "end_index": 27}}}}}}
				case 3: // 第三次调用验证资料确实进入目标模型续轮。
					require.Equal(t, "kimi-k3", name)
					raw, _ := common.Marshal(body["input"])
					require.Contains(t, string(raw), `"type":"function_call_output"`) // 真实函数与结果配对，不使用隐式 system 消息代替。
					require.Contains(t, string(raw), "https://bun.sh/blog")           // 来源链接不能在协议转换时丢失。
					output = []any{gin.H{"type": "function_call", "id": "fc_client", "call_id": "call_client", "name": "exec_command", "arguments": `{"cmd":"echo synthetic"}`, "status": "completed"}}
				default:
					t.Errorf("unexpected extra model invocation: %v", models) // 任何多余调用都可能额外收费，因此直接失败。
				}
				response := gin.H{"id": fmt.Sprintf("resp_%d", len(models)), "object": "response", "status": "completed", "model": name, "output": output, "usage": gin.H{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}}
				w.Header().Set("Content-Type", "text/event-stream")
				for i, item := range output { // 真实索引进入协议过滤器，不能只测最终文本。
					for _, typ := range []string{"response.output_item.added", "response.output_item.done"} { // 同时观察开始与完成，保护隐藏项目的完整生命周期。
						event, _ := common.Marshal(gin.H{"type": typ, "output_index": i, "item": item})
						fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, event)
					}
				}
				terminal, _ := common.Marshal(gin.H{"type": "response.completed", "response": response}) // 用量来自真实形状的终态，而非测试直接写账单。
				fmt.Fprintf(w, "event: response.completed\ndata: %s\n\n", terminal)
			}))
			defer upstream.Close() // 测试结束清理本地供应商，避免端口或连接泄漏。
			var channel model.Channel
			require.NoError(t, model.DB.First(&channel).Error)
			channel.Models = "kimi-k3,gpt-6-luna" // 唯一平台渠道同时服务两个计费模型。
			channel.BaseURL = &upstream.URL
			require.NoError(t, channel.Update()) // 模型能力目录通过现有更新路径刷新。
			// 与公开路由保持相同顺序，不能绕过真正的鉴权及资源策略。
			// 外层只编排输出，不生成第四笔主请求费用。
			body := fmt.Sprintf(`{"model":"gpt-6.1-terra","stream":%t,"store":false,"input":"Find Bun release and use exec_command","tools":[{"type":"web_search","external_web_access":false,"filters":{"allowed_domains":["bun.sh"]}},{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}}]}`, stream)
			result := f.request(t, "POST", "/v1/responses", body)
			require.Equal(t, 200, result.Code, result.Body.String())               // 业务成功必须反映为真实 HTTP 成功。
			require.Equal(t, []string{"kimi-k3", "gpt-6-luna", "kimi-k3"}, models) // 验证整个调用序列，而非某个函数恰好被执行。
			require.NotContains(t, result.Body.String(), "oak_web_search")         // 内部函数不能成为 Codex 待执行工具。
			require.Contains(t, result.Body.String(), "ws_real")                   // 公开搜索事件必须沿用供应商真实身份。
			require.Contains(t, result.Body.String(), "call_client")               // 客户端普通工具的关联 ID 不能因搜索被覆盖。
			if stream {
				require.Equal(t, 1, strings.Count(result.Body.String(), `"type":"response.completed"`)) // 只允许一个外层完成事件，不能提前结束客户端任务。
			}
			var logs []model.Log // 读取持久消费记录，不能用临时日志输出代替账单验证。
			require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Order("id").Find(&logs).Error)
			require.Len(t, logs, 3) // 外层不得额外收取一笔组合响应费用。
			total := 0
			for i, entry := range logs {
				require.Equal(t, models[i], entry.ModelName)      // 每笔费用仍归属实际执行模型。
				require.Contains(t, entry.Other, "search_bridge") // 全部费用都能回溯同一个父请求。
				total += entry.Quota
			}
			if searchPrice > 0 {
				require.Contains(t, logs[1].Other, `"web_search"`)
			} else {
				require.NotContains(t, logs[1].Other, `"tool_surcharges"`)
			} // 零价不追加收费条目。

			require.Equal(t, common.QuotaFromFloat(0.02*common.QuotaPerUnit), logs[0].Quota)
			require.Equal(t, common.QuotaFromFloat((0.03+operation_setting.GetToolPriceForModel("web_search", "gpt-6-luna")/1000)*common.QuotaPerUnit), logs[1].Quota)
			require.Equal(t, logs[0].Quota, logs[2].Quota)                          // 同价目标两轮分别结算，不能漏掉规划轮费用。
			require.Contains(t, logs[0].Other, `"requested_model":"gpt-6.1-terra"`) // 目标阶段保留客户端别名到计费目标的日志快照。
			require.NotContains(t, logs[1].Other, `"token_model_mapping"`)          // GPT 搜索账单不能冒用原 Key 映射标记。
			balance, err := model.GetUserQuota(f.user.Id, false)                    // 关闭缓存读取最终钱包，核对实际扣款。
			require.NoError(t, err)
			require.Equal(t, 1_000_000-total, balance)
		})
	}
}

// TestSearchBridgeLive 仅在显式提供合成探测端点时运行，不读取用户任务正文。
// 实际 API 凭据由临时回环代理保管，本地夹具只使用自己的测试 Key。
// 该测试检查真实搜索事件、目标模型续轮和正常计费日志。
// 不把上游 HTTP 200 或文本声称联网当作搜索成功。
func TestSearchBridgeLive(t *testing.T) {
	endpoint := os.Getenv("OAK_SEARCH_BRIDGE_LIVE_URL") // 真实上游验证必须显式启用，默认回归不消耗套餐。
	if endpoint == "" {
		t.Skip("set OAK_SEARCH_BRIDGE_LIVE_URL for synthetic upstream verification") // 未配置真实端点时明确跳过，不宣称联调通过。
	}
	f := newSearchBridgeFixture(t)
	constant.StreamingTimeout = 180
	target := os.Getenv("OAK_SEARCH_BRIDGE_LIVE_TARGET") // 同一真实验收覆盖两个经过验证的供应商目标。
	if target == "" {
		target = "kimi-k3"
	}
	require.Contains(t, []string{"kimi-k3", "glm-5.3"}, target) // 不允许测试脚本被误用成任意模型调用工具。
	common.OptionMapRWMutex.Lock()
	common.OptionMap[operation_setting.SearchBridgeRoutesOptionKey] = `{"kimi-k3":"gpt-6.1-sol","glm-5.3":"gpt-6.1-sol"}`
	common.OptionMapRWMutex.Unlock()
	f.token.ModelLimits = "kimi-k3,glm-5.3,gpt-6.1-sol"
	f.token.ModelMapping = fmt.Sprintf(`{"gpt-6.1-terra":%q}`, target)
	_, err := f.token.UpdateWithResourcePolicy(nil)
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"kimi-k3":0.02,"glm-5.3":0.02,"gpt-6.1-sol":0.03}`))
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel).Error)
	channel.Models = "kimi-k3,glm-5.3,gpt-6.1-sol"
	channel.BaseURL = &endpoint
	channel.HeaderOverride = common.GetPointer(`{"*":true}`) // 现场渠道已透传会话头，夹具需保持这一真实前提。
	require.NoError(t, channel.Update())
	payload := `{"model":"gpt-6.1-terra","store":false,"stream":true,"max_output_tokens":2048,"input":"Use web search now to find the latest stable Bun release on bun.sh. Search once, then answer with the version and a clickable official source URL. Do not answer from memory.","tools":[{"type":"web_search","external_web_access":true}]}`
	request := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(payload))
	request.Header.Set("Authorization", "Bearer sk-"+f.token.Key) // 仅使用测试数据库生成的 Key，生产凭据留在回环代理。
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Session-Id", "oak-live-"+common.NewRequestId()) // 每个合成任务使用独立会话，避免污染用户正在运行的任务。
	request.Header.Set("X-Opencode-Session", request.Header.Get("Session-Id"))
	result := httptest.NewRecorder()
	f.engine.ServeHTTP(result, request)
	require.Equal(t, 200, result.Code)
	require.NotContains(t, result.Body.String(), `"type":"response.failed"`, result.Body.String())
	require.Contains(t, result.Body.String(), `"type":"web_search_call"`) // 模型文字声称搜索不算真实执行。
	require.Contains(t, result.Body.String(), `"type":"url_citation"`)    // 真实来源引用是搜索验收的一部分。
	require.NotContains(t, result.Body.String(), "oak_web_search")
	require.Equal(t, 1, strings.Count(result.Body.String(), `"type":"response.completed"`))
	// 第二个真实客户端请求携带公开搜索历史，不能依赖隐藏的函数结果缓存。
	// 不重新搜索也应能使用来源摘要，验证后续任务回合可继续。
	var output []any
	for line := range strings.SplitSeq(result.Body.String(), "\n") {
		if strings.HasPrefix(line, "data: ") && gjson.Get(strings.TrimPrefix(line, "data: "), "type").String() == "response.completed" {
			require.NoError(t, common.UnmarshalJsonStr(gjson.Get(strings.TrimPrefix(line, "data: "), "response.output").Raw, &output))
		}
	}
	require.NotEmpty(t, output)                                                                                          // 没有完成输出时不得假装已经验证续轮。
	input := append([]any{gin.H{"role": "user", "content": "Find the latest Bun release using web search."}}, output...) // 重建客户端能够实际发送的完整历史。
	input = append(input, gin.H{"role": "user", "content": "Using only the preceding search results, repeat the official source URL. Do not search again."})
	continuation, err := common.Marshal(gin.H{"model": "gpt-6.1-terra", "store": false, "stream": true, "max_output_tokens": 2048, "input": input, "tools": []any{gin.H{"type": "web_search"}}})
	require.NoError(t, err)
	next := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(string(continuation))) // 独立请求重新鉴权，覆盖无缓存续轮。
	next.Header = request.Header.Clone()                                                          // 保持同一用户会话，不以换会话规避兼容问题。
	second := httptest.NewRecorder()
	f.engine.ServeHTTP(second, next)
	require.Contains(t, second.Body.String(), `"type":"response.completed"`, second.Body.String())
	require.NotContains(t, second.Body.String(), `"type":"response.failed"`) // 第二轮搜索历史不能导致被隐藏的协议失败。
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Order("id").Find(&logs).Error)
	require.GreaterOrEqual(t, len(logs), 3) // 核对真实调用已进入本地正常结算链。
	for _, entry := range logs {
		t.Logf("model=%s quota=%d request=%s", entry.ModelName, entry.Quota, entry.RequestId)
	}
}

// TestSearchBridgeFailures 区分没有搜索需求、权限拒绝和上游假成功。
// 各失败都在实际执行边界计数，不能只检查错误消息而遗漏额外收费。
// 没有使用搜索时只产生一笔目标费用，禁止无条件调用 GPT。
// 后端没有搜索事件时不得返回“已联网”的成功回答。
// 循环达到边界后停止，不再产生第五次搜索费用。
func TestSearchBridgeFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		calls   int
		failure bool
	}{
		{"no_search", 1, false}, {"forbidden", 1, true}, {"silent_backend", 2, true}, {"upstream_failure", 2, true}, {"empty_policy", 0, true}, {"loop_limit", 9, true}, {"parallel_client", 2, false}, {"insufficient_balance", 1, true}, {"no_policy", 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSearchBridgeFixture(t)
			if tc.name == "forbidden" { // 搜索后端权限不足时只能保留已完成的目标调用。
				f.token.ModelLimits = "kimi-k3"
				_, err := f.token.UpdateWithResourcePolicy(nil)
				require.NoError(t, err)
			}
			if tc.name == "empty_policy" { // 空来源策略不是允许自动退回钱包。
				f.setPolicy(t)
			}
			if tc.name == "no_policy" { // 旧 Key 没有来源策略时也必须使用原平台计费。
				require.NoError(t, model.DB.Where("token_id = ?", f.token.Id).Delete(&model.ResourcePolicy{}).Error)
			}
			if tc.name == "insufficient_balance" { // 目标一轮恰好耗尽余额，搜索不能借旧快照继续执行。
				require.NoError(t, model.DB.Model(f.user).Update("quota", common.QuotaFromFloat(0.02*common.QuotaPerUnit)).Error)
			}
			calls := 0 // 实际网络调用数用于证明失败后没有继续搜索。
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				require.NoError(t, common.DecodeJson(r.Body, &body))
				calls++
				message := gin.H{"type": "message", "id": fmt.Sprintf("msg_%d", calls), "role": "assistant", "status": "completed", "content": []any{gin.H{"type": "output_text", "text": "Synthetic result", "annotations": []any{}}}}
				output := []any{message}
				if body["model"] == "gpt-6-luna" {
					if tc.name == "upstream_failure" { // 模拟已到达供应商的故障，平台不能静默换模型。
						w.WriteHeader(500)
						fmt.Fprint(w, `{"error":{"message":"synthetic failure"}}`)
						return
					}
					if tc.name != "silent_backend" { // 无事件的 200 是能力缺失，不是搜索成功。
						output = []any{gin.H{"type": "web_search_call", "id": fmt.Sprintf("ws_%d", calls), "status": "completed", "action": gin.H{"type": "search", "query": "synthetic"}}, message}
					}
				} else if tc.name != "no_search" && (calls == 1 || tc.name == "loop_limit") {
					output = []any{gin.H{"type": "function_call", "id": fmt.Sprintf("fc_%d", calls), "call_id": fmt.Sprintf("call_%d", calls), "name": "oak_web_search", "arguments": `{"query":"synthetic"}`}}
					if tc.name == "parallel_client" { // 同轮内部与外部工具不能互相阻塞或误执行。
						output = append(output, gin.H{"type": "function_call", "id": "fc_external", "call_id": "external", "name": "exec_command", "arguments": `{"cmd":"echo synthetic"}`})
					}
				}
				response := gin.H{"id": fmt.Sprintf("resp_%d", calls), "model": body["model"], "object": "response", "status": "completed", "output": output, "usage": gin.H{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}}
				raw, err := common.Marshal(gin.H{"type": "response.completed", "response": response})
				require.NoError(t, err)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "event: response.completed\ndata: %s\n\n", raw)
			}))
			defer server.Close()
			var channel model.Channel
			require.NoError(t, model.DB.First(&channel).Error)
			channel.Models = "kimi-k3,gpt-6-luna"
			channel.BaseURL = &server.URL
			require.NoError(t, channel.Update())
			result := f.request(t, "POST", "/v1/responses", `{"model":"gpt-6.1-terra","store":false,"stream":true,"input":"synthetic","tools":[{"type":"web_search"},{"type":"function","name":"exec_command","parameters":{"type":"object"}}]}`)
			require.Equal(t, tc.calls, calls, result.Body.String()) // 覆盖循环上限、权限与来源拒绝的实际调用边界。
			require.NotContains(t, result.Body.String(), "oak_web_search")
			if tc.failure { // 同一流内失败不能再追加完成事件。
				require.Contains(t, result.Body.String(), `"type":"response.failed"`)
				require.NotContains(t, result.Body.String(), `"type":"response.completed"`)
			} else {
				require.Contains(t, result.Body.String(), `"type":"response.completed"`)
			}
			if tc.name == "no_search" {
				require.NotContains(t, result.Body.String(), `"type":"web_search_call"`)
			}
			if tc.name == "parallel_client" {
				require.Contains(t, result.Body.String(), `"call_id":"external"`)
			}
		})
	}
}

// TestSearchBridgeProtocolBoundary 检查流身份、公开索引及断连边界。
// 只替代子请求上游，输出过滤与协议转换仍执行正式实现。
// 内部函数不占公开索引，普通函数的 call_id 必须原样保留。
// 取消后不能让后台继续等待通道或启动新的请求。
func TestSearchBridgeProtocolBoundary(t *testing.T) {
	var response map[string]any
	require.NoError(t, common.UnmarshalJsonStr(`{"status":"completed","output":[{"type":"function_call","id":"hidden","name":"oak_web_search","call_id":"hidden_call","arguments":"{}"},{"type":"message","id":"msg","role":"assistant","content":[{"type":"output_text","text":"hello","annotations":[]}]},{"type":"custom_tool_call","id":"ct","call_id":"client_call","name":"exec","input":"code"}]}`, &response))
	stage, err := publicSearchBridgeStage(searchBridgeStage{response: response}, false) // 测试正式协议过滤器，而非复制索引映射算法。
	require.NoError(t, err)
	raw, err := common.Marshal(stage.response) // 序列化实际公开输出，避免对空私有结构做无效断言。
	require.NoError(t, err)
	require.NotContains(t, string(raw), "hidden_call")
	output := stage.response["output"].([]any)
	require.Len(t, output, 2)
	require.Equal(t, "client_call", output[1].(map[string]any)["call_id"]) // custom 工具也必须保留客户端关联身份。
	var added, done []int                                                  // 公开索引需要连续且开始完成成对。
	for _, event := range stage.events {
		if event["type"] == "response.output_item.added" {
			added = append(added, int(event["output_index"].(float64)))
			if event["output_index"] == float64(0) {
				require.Empty(t, event["item"].(map[string]any)["content"])
			}
		}
		if event["type"] == "response.output_item.done" {
			done = append(done, int(event["output_index"].(float64)))
		}
	}
	require.Equal(t, []int{0, 1}, added)
	require.Equal(t, added, done)
	// 非整数索引不能截断后误绑定到另一个公开工具。
	_, err = publicSearchBridgeStage(searchBridgeStage{response: response, events: []map[string]any{{"type": "response.output_item.done", "output_index": 0.5}}}, false)
	require.ErrorContains(t, err, "invalid_search_bridge_output_index")
	ctx, cancel := context.WithCancel(context.Background()) // 用显式取消信号验证退出，不依赖时间等待。
	cancel()
	req := httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
	require.Error(t, sendSearchBridgeStage(req, make(chan searchBridgeStage), stage))                       // 消费者不存在且已取消时必须立即解除阻塞。
	require.Error(t, mergeSearchBridgeUsage(map[string]any{}, map[string]any{"input_tokens": float64(-1)})) // 负用量不能污染客户端统计或伪装退款。
	require.True(t, searchBridgeReservedTool(map[string]any{"input": []any{map[string]any{"type": "additional_tools", "tools": []any{map[string]any{"type": "function", "name": "oak_web_search"}}}}}))
}

// TestSearchBridgeConfiguration 使用正式选项校验，避免管理 UI 独占校验职责。
// 空配置关闭功能，不预设收费模型；非法目标及链式后端显式拒绝。
// 原生 GPT 不依赖桥接配置，坏配置不能阻断无关模型请求。
func TestSearchBridgeConfiguration(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"kimi-k3":"kimi-k3"}`, `{"unknown":"gpt-6.1-sol"}`, `{"kimi-k3":"gpt-"}`, `{"kimi-k3":"gpt-6 sol"}`} {
		_, err := operation_setting.ParseSearchBridgeRoutes(raw)
		require.Error(t, err, raw)
	}
	routes, err := operation_setting.ParseSearchBridgeRoutes(`{}`) // 关闭配置是一等状态，不能被默认收费模型替代。
	require.NoError(t, err)
	require.Empty(t, routes)
	_, err = operation_setting.ParseSearchBridgeRoutes(`{"glm-5.3":"gpt-6.1-sol"}`)
	require.NoError(t, err)
	backend, err := operation_setting.SearchBridgeBackend("gpt-6.1-sol") // 原生 GPT 请求不依赖本适配器的配置。
	require.NoError(t, err)
	require.Empty(t, backend)
}

// TestSearchBridgeCancellation 取消正在执行的真实 HTTP，而不只取消内存通道。
// 上游已开始输出，但尚未产生终态；客户端此时断开连接。
// 桥接必须把取消传播到 HTTP transport，且不能进入 GPT 搜索轮。
// 通过明确的事件同步判断结果，不用固定 sleep 猜测请求是否已经开始。
// 超时仅用于报告未终止的故障，不作为测试成功条件。
func TestSearchBridgeCancellation(t *testing.T) {
	f := newSearchBridgeFixture(t)
	started, cancelled := make(chan struct{}), make(chan struct{})
	// 请求头提交后才能判定是真正的在途取消。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"pending\",\"status\":\"in_progress\",\"output\":[]}}\n\n")
		w.(http.Flusher).Flush()
		close(started)       // 第二次上游调用会触发重复关闭，暴露取消后错误重试。
		<-r.Context().Done() // 正式 transport 必须收到同一取消信号。
		close(cancelled)
	}))
	defer server.Close()
	var channel model.Channel
	require.NoError(t, model.DB.First(&channel).Error)
	channel.Models = "kimi-k3,gpt-6-luna"
	channel.BaseURL = &server.URL
	require.NoError(t, channel.Update())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"gpt-6.1-terra","stream":true,"store":false,"input":"synthetic","tools":[{"type":"web_search"}]}`)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer sk-"+f.token.Key)
	returned := make(chan struct{})
	go func() { f.engine.ServeHTTP(httptest.NewRecorder(), request); close(returned) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream did not start")
	}
	cancel() // 只有上游开始之后取消，防止把入口短路误当作取消传播。
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("client handler did not stop")
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream request was not cancelled")
	}
}
