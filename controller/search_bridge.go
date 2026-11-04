package controller

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// 搜索编排只处理 HTTP Responses，不更换用户的 Key、分组或资金授权。
// GPT 是真实搜索执行模型，目标模型仍决定是否搜索并生成最终回答。
// 每一轮都经过原鉴权、预扣、结算和日志；外层绝不再结算一遍。
// 内部函数不会返回客户端执行，公开输出仅包含真实上游结果。
// 上限约束自动循环及响应内存；失败不会悄悄删除工具继续生成。
const searchBridgeFunction = "oak_web_search" // 保留名称由入口统一校验，不能与客户端函数重名。
const searchBridgeMaxCalls = 4                // 限制平台自动发起的搜索轮数，避免模型无界循环。
const searchBridgeMaxBytes = 8 << 20          // 每个子响应的硬内存上限，不是用户上传限制。

// searchBridgeReservedTool 拒绝函数、命名空间及动态工具中的保留名称。
// 只检查结构化声明和调用名称，不扫描用户提示词中的普通文本。
// 不信任客户端伪造的内部函数历史，防止执行归属产生歧义。
func searchBridgeReservedTool(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		if v["name"] == searchBridgeFunction {
			return true
		}
		for _, child := range v {
			if searchBridgeReservedTool(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if searchBridgeReservedTool(child) {
				return true
			}
		}
	}
	return false
}

type searchBridgeContextKey struct{} // 私有类型不能由 HTTP 请求头伪造。
type searchBridgeIdentity struct {
	parent, requested, target, stage string // 关联身份与计费模型分开，GPT 阶段没有 Key 映射身份。
}

// searchBridgeStage 仅在上游已完成解析和结算之后发布。
// SSE 按阶段输出，保留真实事件，等待时由主协程发送心跳。
// runner 不直接写客户端，避免心跳与模型事件竞争 ResponseWriter。
type searchBridgeStage struct {
	events   []map[string]any // 已完成阶段的真实事件，内部函数事件会在发布前过滤。
	response map[string]any   // 上游终态是输出与用量的可信来源。
	err      error            // 稳定分类错误，不能包含原始供应商正文。
	final    bool             // 仅最终阶段允许结束客户端响应。
}

// ResponsesSearchBridge 必须位于 Key 映射之后、资源分发之前。
// 仅管理员明确配置的目标且声明搜索工具时才接管；其他请求原样通过。
// 子路由为进程内私有引擎，外部无法访问或伪造可信父请求标记。
// 每个子请求重新鉴权，Key 撤销、权限收紧与余额不足都会阻止下一步。
// 映射目标在外层冻结；重新鉴权不会把搜索模型再次按 Key 规则改写。
func ResponsesSearchBridge() gin.HandlerFunc {
	child := gin.New()
	// 固定使用外层解析过的客户端 IP，不能重新信任内部转发头。
	child.ForwardedByClientIP = false
	_ = child.SetTrustedProxies(nil)
	child.Use(middleware.BodyStorageCleanup(), func(c *gin.Context) {
		identity, ok := c.Request.Context().Value(searchBridgeContextKey{}).(searchBridgeIdentity) // 仅进程内发起者能提供这份身份。
		if !ok {                                                                                   // 拒绝没有可信父请求的私有执行入口。
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Set(common.RequestIdKey, identity.parent+"."+identity.stage) // 独立 ID 避免订阅预扣的幂等记录互相覆盖。
		c.Set("oak.search_bridge_parent", identity.parent)             // 用户日志只暴露可关联的父请求标识。
		c.Set("oak.search_bridge_stage", identity.stage)               // 目标与搜索阶段在同一日志查询中可区分。
		if identity.requested != "" {                                  // GPT 阶段不继承目标模型的映射标签。
			common.SetContextKey(c, constant.ContextKeyTokenRequestedModel, identity.requested)
			common.SetContextKey(c, constant.ContextKeyTokenTargetModel, identity.target)
		}
	}, middleware.TokenAuth(), middleware.ModelRequestRateLimit(), ResourceRelay(), middleware.Distribute())
	child.POST("/v1/responses", func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIResponses) })
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || c.Request.URL.Path != "/v1/responses" { // WebSocket 与资源查询保持原协议入口。
			c.Next()
			return
		}
		envelope, err := middleware.ResourceModelRequest(c) // 沿用严格 model 解析与重复键拒绝。
		if err != nil {
			resourceRelayError(c, "invalid_request", 400)
			return
		}
		backend, err := operation_setting.SearchBridgeBackend(envelope.Model) // 只在请求开始时读取一次配置。
		if err != nil {
			resourceRelayError(c, "invalid_search_bridge_configuration", 500)
			return
		}
		if backend == "" { // 未配置目标直接走原链路，不产生额外模型调用。
			c.Next()
			return
		}
		var body map[string]any                                       // 保留未参与适配的协议扩展字段。
		if err = common.UnmarshalBodyReusable(c, &body); err != nil { // 同一正文存储也供后续校验及透传读取。
			resourceRelayError(c, "invalid_request", 400)
			return
		}
		if searchBridgeReservedTool(body) { // 动态工具和历史函数也不能占用平台内部名称。
			resourceRelayError(c, "reserved_search_tool_name", 400)
			return
		}
		tools, _ := body["tools"].([]any) // 桥接只接管当前请求明确声明的顶层搜索工具。
		var search map[string]any         // 保留整份工具选项，不能丢弃过滤域名或位置。
		for _, raw := range tools {
			tool, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if tool["type"] == "web_search" || tool["type"] == "web_search_preview" {
				if search != nil { // 两个搜索声明的选项优先级不明确，因此拒绝。
					resourceRelayError(c, "duplicate_search_tool", 400)
					return
				}
				search = tool
			}
		}
		if search == nil || body["tool_choice"] == "none" { // 禁用工具时不启动搜索规划。
			c.Next()
			return
		}
		// 先复用公共校验，不能借桥接绕过输出预算及输入类型约束。
		// 持久响应与后台任务需要跨请求状态存储，本同步实现明确拒绝。
		if _, err = helper.GetAndValidateRequest(c, types.RelayFormatOpenAIResponses); err != nil {
			resourceRelayError(c, "invalid_request", 400)
			return
		}
		if body["background"] == true || body["previous_response_id"] != nil && body["previous_response_id"] != "" {
			resourceRelayError(c, "search_bridge_requires_explicit_history", 400)
			return
		}
		if body["store"] == true { // 组合响应不属于任何单一上游的持久响应。
			resourceRelayError(c, "search_bridge_requires_store_false", 400)
			return
		}
		// 指定单一搜索工具转为仅搜索的规划轮，完成后恢复客户端工具集合。
		// 普通函数强制选择保持原样；复杂 allowed_tools 不能静默改为 auto。
		if choice, ok := body["tool_choice"].(map[string]any); ok && choice["type"] != "web_search" && choice["type"] != "web_search_preview" && choice["type"] != "function" && choice["type"] != "custom" {
			resourceRelayError(c, "unsupported_search_bridge_tool_choice", 400)
			return
		}
		originalChoice, originalInstructions := body["tool_choice"], body["instructions"] // 保存客户端公开合同，终态不能回显内部改写。
		originalStream, _ := body["stream"].(bool)                                        // 客户端模式在强制内部流式前冻结。
		parent := c.GetString(common.RequestIdKey)                                        // 沿用访问日志 ID，便于从网关定位所有费用。
		if parent == "" {
			parent = common.NewRequestId()
			c.Set(common.RequestIdKey, parent)
		}
		source := common.GetContextKeyString(c, constant.ContextKeyTokenRequestedModel) // 原请求名只用于映射日志，不用于搜索计价。
		request := c.Request.Clone(c.Request.Context())                                 // 拷贝头部，避免阶段间修改用户会话。
		request.RemoteAddr = net.JoinHostPort(c.ClientIP(), "0")
		ctx, cancel := context.WithCancel(request.Context()) // 下游取消必须传播到实际上游 HTTP。
		request = request.WithContext(ctx)                   // 子请求共享取消信号，不共享 Gin 对象生命周期。
		finished := make(chan struct{})                      // 请求返回前等候子执行器结算及退出。
		stages := make(chan searchBridgeStage)               // 无缓冲发布使客户端背压约束后续阶段。
		// 拷贝请求后才启动协程，Gin 上下文不会逃逸到后台。
		// 请求取消传播至内部 HTTP；已完成子请求费用不因外层取消而伪造退款。
		go func() {
			defer close(finished) // 即使取消或 panic，也必须释放请求收尾等待。
			defer close(stages)   // 执行结束只关闭一次，不遗留消费者等待。
			defer func() {
				if value := recover(); value != nil { // 协程异常转换为明确失败，禁止伪造完成。
					// 栈定位保留在服务端；不打印可能包含正文的 panic 值。
					logger.LogError(ctx, fmt.Sprintf("search bridge panic request_id=%s type=%T stack=%s", parent, value, debug.Stack()))
					select {
					case stages <- searchBridgeStage{err: errors.New("search_bridge_internal_error")}:
					case <-ctx.Done():
					}
				}
			}()
			err := runSearchBridge(child, request, body, search, backend, parent, source, stages)
			if err != nil {
				select {
				case stages <- searchBridgeStage{err: err}:
				case <-ctx.Done():
				}
			}
		}()
		// 结构化收尾：先取消网络，再等子请求完成退款或消费日志。
		// 避免 HTTP 请求已结束时后台仍持有上一轮的鉴权及计费状态。
		defer func() { cancel(); <-finished }()
		c.Abort()                          // 外层不再执行普通 Relay，避免重复预扣和转发。
		responseID := "resp_oak_" + parent // 组合响应使用独立 ID，不借用搜索后端的资源 ID。
		sequence := 0                      // 各上游的局部序号不能直接拼接。
		output := []any{}                  // 所有公开项目按实际阶段顺序累积。
		initial := map[string]any{"id": responseID, "object": "response", "created_at": time.Now().Unix(), "model": envelope.Model, "status": "in_progress", "output": []any{}, "store": false}
		if originalStream {
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache") // 避免缓存带用户上下文的模型输出。
			c.Header("X-Accel-Buffering", "no")   // 允许代理及时发送心跳和阶段完成事件。
			if err = writeSearchBridgeEvent(c, "response.created", map[string]any{"response": initial}, &sequence); err != nil {
				return
			}
			if err = writeSearchBridgeEvent(c, "response.in_progress", map[string]any{"response": initial}, &sequence); err != nil {
				return
			}
		}
		ticker := time.NewTicker(10 * time.Second) // 阶段缓冲期间仍保持客户端连接活跃。
		defer ticker.Stop()                        // 请求结束释放计时器，不建立后台轮询。
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if originalStream {
					if _, err = io.WriteString(c.Writer, ": keep-alive\n\n"); err != nil {
						return
					}
					c.Writer.Flush()
				}
			case stage, ok := <-stages: // 只有主协程拥有下游 Writer。
				if !ok {
					return
				}
				if stage.err != nil {
					// 费用已由各子请求结算，外层只报告稳定错误及父请求 ID。
					// 不回显子请求凭据、网络地址或用户原始提示词。
					message := stage.err.Error() + " (request id: " + parent + ")"
					if originalStream {
						failed := maps.Clone(initial)
						failed["status"] = "failed"
						failed["output"] = output // 失败前已公开的证据仍保留，不假装从未执行。
						failed["error"] = gin.H{"code": "search_bridge_failed", "message": message}
						_ = writeSearchBridgeEvent(c, "response.failed", map[string]any{"response": failed}, &sequence)
					} else {
						c.JSON(502, gin.H{"error": gin.H{"code": "search_bridge_failed", "message": message}}) // 普通 JSON 客户端获得真实失败状态。
					}
					return
				}
				if stage.final {
					stage.response["created_at"] = initial["created_at"] // 创建事件与终态必须属于同一个外层响应。
					stage.response["id"] = responseID
					stage.response["output"] = output
					stage.response["model"] = envelope.Model // 最终模型身份保持平台目标，搜索身份在独立用量中说明。
					stage.response["store"] = false
					stage.response["tools"] = tools // 返回客户端原始声明，不暴露平台内部函数。
					if originalChoice != nil {
						stage.response["tool_choice"] = originalChoice
					} else {
						delete(stage.response, "tool_choice")
					} // 缺省选择保持缺省。
					if originalInstructions != nil {
						stage.response["instructions"] = originalInstructions
					} else {
						delete(stage.response, "instructions")
					} // 不回显搜索后端的内部提示。
					if originalStream {
						typ := "response.completed"
						if stage.response["status"] == "incomplete" { // 保留上游未完成状态，不能包装成成功。
							typ = "response.incomplete"
						}
						_ = writeSearchBridgeEvent(c, typ, map[string]any{"response": stage.response}, &sequence)
					} else {
						c.JSON(200, stage.response)
					}
					return
				}
				offset := len(output) // 将局部输出索引平移到组合响应空间。
				items, _ := stage.response["output"].([]any)
				output = append(output, items...)
				if originalStream {
					for _, event := range stage.events {
						if index, ok := event["output_index"].(float64); ok {
							event["output_index"] = int(index) + offset // 项目相对顺序不变，隐藏项目不会留下空洞。
						}
						if _, ok := event["response_id"]; ok {
							event["response_id"] = responseID // 带响应引用的事件同样使用外层身份。
						}
						typ, _ := event["type"].(string)
						if err = writeSearchBridgeEvent(c, typ, event, &sequence); err != nil {
							return
						}
					}
				}
			}
		}
	}
}

// writeSearchBridgeEvent 统一维护外层序号，保证跨模型事件仍是单条 SSE。
// 不把上游的 response.completed 提前输出，只有编排完成后才发送终态。
// 所有写入均来自请求主协程，网络错误立即取消尚未完成的工作。
func writeSearchBridgeEvent(c *gin.Context, typ string, event map[string]any, sequence *int) error {
	event["type"] = typ
	event["sequence_number"] = *sequence // 序号只在发送时生成，跨阶段严格递增。
	*sequence++                          // 终态也占用一个序号，不能重用最后增量的编号。
	raw, err := common.Marshal(event)    // 编码失败向上传播，不发半条 SSE。
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", typ, raw)
	c.Writer.Flush()
	return err
}

// searchBridgeCapture 只缓冲有界的阶段结果，不持久化用户正文。
// Flush 不提交外层响应；阶段结束后才能判断哪些函数由平台执行。
// 超过大小边界返回写入失败，让现有流处理器终止并结算已观察用量。
type searchBridgeCapture struct {
	header   http.Header  // 子请求响应头与客户端头完全隔离。
	status   int          // 第一份 HTTP 状态决定阶段是否进入解析。
	body     bytes.Buffer // 只保存当前阶段，不持久记录搜索正文。
	overflow bool         // 写入超限需独立标记，不能被适配器忽略后误判成功。
}

func (w *searchBridgeCapture) Header() http.Header { return w.header }
func (w *searchBridgeCapture) WriteHeader(code int) {
	if w.status == 0 { // 遵循标准 ResponseWriter 的首个状态生效语义。
		w.status = code
	}
}
func (w *searchBridgeCapture) Flush() {} // 内部刷新不代表向客户端提交内容。
func (w *searchBridgeCapture) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	if w.body.Len()+len(p) > searchBridgeMaxBytes { // 先检查再写入，不让单个大增量越过上限。
		w.overflow = true
		return 0, errors.New("search_bridge_response_too_large")
	}
	return w.body.Write(p)
}

// executeSearchBridgeStage 通过同一 Key 的正常入口执行一个真实子请求。
// 每次使用新的正文存储及 Gin 上下文，不复制渠道或预扣会话指针。
// 仍受模型限流、账户归属、资源来源与目标模型权限约束。
// 内部使用流式获取用量和终态；客户端是否流式只影响外层输出。
// 子请求序号同时进入消费日志，搜索执行不伪装为目标模型收费。
func executeSearchBridgeStage(engine *gin.Engine, original *http.Request, body map[string]any, identity searchBridgeIdentity) (searchBridgeStage, error) {
	if err := original.Context().Err(); err != nil { // 取消已发生时不再触发鉴权或费用预留。
		return searchBridgeStage{}, errors.New("search_bridge_cancelled")
	}
	raw, err := common.Marshal(body) // 每个子请求持有自己编码后的不可变正文。
	if err != nil {
		return searchBridgeStage{}, errors.New("invalid_search_bridge_request")
	}
	if len(raw) > 32<<20 { // 搜索结果追加历史后也必须有整体大小边界。
		return searchBridgeStage{}, errors.New("search_bridge_history_too_large")
	}
	request := original.Clone(context.WithValue(original.Context(), searchBridgeContextKey{}, identity))
	request.Body = io.NopCloser(bytes.NewReader(raw))
	request.ContentLength = int64(len(raw)) // 实际字节数用于原请求大小门禁，不能沿用旧模型正文长度。
	request.GetBody = nil                   // 旧重放函数不能返回外层尚未适配的正文。
	request.Header.Set("Content-Type", "application/json")
	request.Header.Del("Content-Length")   // 正文重新编码后旧长度不再可信。
	request.Header.Del("Content-Encoding") // 新正文是明文 JSON，不能沿用压缩声明。
	// 搜索后端使用独立会话，不能与主模型共享供应商推理状态。
	// 主模型仍沿用用户会话头；此处不记录任何会话原值。
	if strings.HasPrefix(identity.stage, "search.") {
		session := "oak-search-" + identity.parent + "-" + identity.stage
		request.Header.Set("Session-Id", session)
		request.Header.Set("X-Opencode-Session", session)
	}
	writer := &searchBridgeCapture{header: make(http.Header)} // 每阶段创建独立响应存储，避免重试状态串用。
	engine.ServeHTTP(writer, request)                         // 同步返回时原链路已经完成用量结算。
	if writer.overflow {                                      // 截断响应绝不能作为完整搜索结果。
		return searchBridgeStage{}, errors.New("search_bridge_response_too_large")
	}
	if writer.status >= 400 { // 拒绝或上游失败不会改用另一搜索服务掩盖故障。
		return searchBridgeStage{}, fmt.Errorf("search_bridge_%s_http_%d", identity.stage, writer.status)
	}
	stage := searchBridgeStage{}
	if !strings.HasPrefix(writer.header.Get("Content-Type"), "text/event-stream") {
		if err := common.Unmarshal(writer.body.Bytes(), &stage.response); err != nil {
			return stage, errors.New("invalid_search_bridge_response")
		}
	} else {
		scanner := bufio.NewScanner(bytes.NewReader(writer.body.Bytes()))
		scanner.Buffer(make([]byte, 4096), searchBridgeMaxBytes) // 事件解析上限与响应存储上限一致。
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") { // 事件名与心跳不是 JSON 负载。
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" { // 结束哨兵不替代 Responses 的真实终态。
				continue
			}
			var event map[string]any
			if err := common.UnmarshalJsonStr(data, &event); err != nil {
				return stage, errors.New("invalid_search_bridge_event")
			}
			typ, _ := event["type"].(string)
			if typ == "error" || typ == "response.failed" {
				return stage, errors.New("search_bridge_upstream_failed")
			}
			if response, ok := event["response"].(map[string]any); ok && (typ == "response.completed" || typ == "response.done" || typ == "response.incomplete") {
				stage.response = response // 最终用量和完整 output 从终态冻结。
				continue
			}
			if typ != "response.created" && typ != "response.in_progress" {
				stage.events = append(stage.events, event)
			}
		}
		if scanner.Err() != nil { // 事件过长或读取错误必须终止，不能忽略。
			return stage, errors.New("invalid_search_bridge_stream")
		}
	}
	if stage.response == nil || stage.response["status"] != "completed" && stage.response["status"] != "incomplete" {
		return stage, errors.New("search_bridge_missing_terminal")
	}
	if _, ok := stage.response["output"].([]any); !ok { // 没有完整输出数组时不进入搜索循环。
		return stage, errors.New("search_bridge_missing_output")
	}
	return stage, nil
}
