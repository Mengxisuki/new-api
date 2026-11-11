// 回归覆盖真实返回入口，不直接调用文本清理函数来重复实现其算法。
// 输入统一是供应商 Chat JSON 或 SSE，输出按客户端协议公开字段验收。
// 用量使用固定上游快照，缺失快照的场景另外验证原始文本估算。
// 所有提示与返回均为合成中文短句，不调用真实供应商、不消耗生产额度。
// 流式夹具按字符切分，保证标签名与闭合标记一定跨事件。
// 同一组用例覆盖两种客户端协议与两种返回模式，防止接入遗漏。
// 错误用例要求阻止正式文本下发，并沿用禁止重试的上游失败合同。
// 工具、扩展字段与非目标模型单独约束，防止清理扩大为协议重写。
package openai

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// 通过真实响应处理入口检查四条返回路径，防止只修转换而遗漏原生 Chat。
// 合成正文不包含真实用户内容；上游用量用于约束清理不能改变计量。
// 每个字符独立成为 SSE delta，使开闭标签必然跨越多个网络事件。
func TestDeepSeekReasoningOutput(t *testing.T) {
	// 每个期待值描述客户端应看到的结果，而不是中间解析状态。
	// 纯正文用例同时约束换行和 HTML 等非推理标签不被误删。
	cases := []struct {
		name, content, reasoning, wantContent, wantReasoning string
		wantError                                            bool
	}{
		// 基线：已有独立推理时不改变正式回复。
		{name: "正常分离", content: "收到", reasoning: "内部规划", wantContent: "收到", wantReasoning: "内部规划"},
		// 原始复现：完整区块必须连同内部字符移走。
		{name: "thinking块", content: "<thinking>内部规划</thinking>\n收到", wantContent: "收到", wantReasoning: "内部规划"},
		// 不同名称、大小写及嵌套都不能提前结束隐藏。
		{name: "标签变体与嵌套", content: "<THINK>一<reasoning>二</reasoning>三</THINK><thought>四</thought>收到", wantContent: "收到", wantReasoning: "一二三四"},
		// 独立字段已有相同块时只保留一份。
		{name: "同一推理不重复", content: "<thinking>内部规划</thinking>收到", reasoning: "内部规划", wantContent: "收到", wantReasoning: "内部规划"},
		// 闭合之前整段归推理，不能只删闭合标记。
		{name: "只剩闭合标签", content: "内部规划</thinking>收到", wantContent: "收到", wantReasoning: "内部规划"},
		// 来自报告第四例的确定性边界恢复。
		{name: "两字段仅闭合标签不同", content: "内部规划收到", reasoning: "内部规划</thinking>收到", wantContent: "收到", wantReasoning: "内部规划"},
		// 规划后仍有正文时只删除确定重复的前缀。
		{name: "未标记重复前缀", content: "内部规划\n收到", reasoning: "内部规划", wantContent: "收到", wantReasoning: "内部规划"},
		// 报告前几例的双写不应作为正式回答成功返回。
		{name: "完全相同判异常", content: "内部规划", reasoning: "内部规划", wantError: true},
		// 错名闭合不能把后续字符重新分类成正文。
		{name: "错配闭合不能泄漏后文", content: "<thinking>内部规划</thought>收到", wantError: true},
		// 缺少闭合标记时不能把隐藏区内容重新透传。
		{name: "未闭合块判异常", content: "<thinking>内部规划", wantError: true},
		// 保留区块之前已确定的回答，隐藏未完成规划。
		{name: "尾部未闭合块保留已知正文", content: "收到<thought>内部规划", wantContent: "收到", wantReasoning: "内部规划"},
		// 没有证据的文本必须保持原字节与普通标签。
		{name: "纯正文不变", content: "保留\n原有换行 <em>样式</em>", wantContent: "保留\n原有换行 <em>样式</em>"},
	}
	// 这些子用例修改现有扫描超时配置，因此按顺序执行而不并行。
	// 每个子用例都有全新上下文，缓冲不能在不同响应之间串联。
	for _, tc := range cases {
		for _, stream := range []bool{false, true} {
			for _, responses := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/stream=%t/responses=%t", tc.name, stream, responses), func(t *testing.T) {
					// 复用现有 HTTP 上下文，实际执行扫描、转换与最终响应写入。
					// 两种客户端协议都必须看到同样的正文与推理归属。
					body := reasoningChatFixture(t, tc.content, tc.reasoning, stream, true)
					c, recorder, resp, info := newResponsesChatTestContext(t, body, stream)
					// 模型依据真实上游字段，独立于夹具中的供应商 model 回显。
					// 计量路径需要真实 RelayMode，避免误用文本补全处理分支。
					info.UpstreamModelName = "deepseek-v4.1-flash"
					info.RelayMode = relayconstant.RelayModeChatCompletions
					var usage *dto.Usage          // 观察实际处理器返回的用量。
					var apiErr *types.NewAPIError // 不吞掉真实的上游或转换错误。
					// 四个入口实际负责不同的响应写入，不通过私有清理器捷径。
					// 客户端选择只影响输出协议，不能改变同一上游文本的归属。
					if responses {
						info.RelayFormat = types.RelayFormatOpenAIResponses
						if stream {
							usage, apiErr = OaiChatToResponsesStreamHandler(c, info, resp)
						} else {
							usage, apiErr = OaiChatToResponsesHandler(c, info, resp)
						}
					} else if stream {
						usage, apiErr = OaiStreamHandler(c, info, resp)
					} else {
						usage, apiErr = OpenaiHandler(c, info, resp)
					}
					if tc.wantError {
						// 只有规划的响应必须显式失败，不能输出规划后再伪造成功结束。
						// 校验失败输出里没有任何正式文本事件，且禁止付费请求重试。
						require.NotNil(t, apiErr)
						assert.Equal(t, 502, apiErr.StatusCode)        // 未提供正式回答属于上游输出故障。
						assert.True(t, types.IsSkipRetryError(apiErr)) // 本轮已生成，不能再次自动付费生成。
						got, _ := reasoningClientText(t, recorder.Body.String(), stream, responses)
						assert.Empty(t, got) // 返回错误之前也不能先泄漏一段规划。
						return
					}
					// 有正文的修复必须继续正常返回，不能靠一律拒绝掩盖泄漏。
					// 精确比较公开文本能同时发现残留、丢字及重复发送。
					require.Nil(t, apiErr)
					require.NotNil(t, usage)
					assert.Equal(t, 42, usage.PromptTokens)     // 输入快照不随正文归属变化。
					assert.Equal(t, 17, usage.CompletionTokens) // 隐藏文本仍属于上游实际生成。
					gotContent, gotReasoning := reasoningClientText(t, recorder.Body.String(), stream, responses)
					assert.Equal(t, tc.wantContent, gotContent)     // 严格对比，不能接受仅标签消失。
					assert.Equal(t, tc.wantReasoning, gotReasoning) // 同一规划不能在这里再追加一份。
				})
			}
		}
	}
}

// 所有片段使用同一 choice index，尾部 usage 独立成帧，模拟真实上游。
// 未传终态用于验证断流时不能把尚未判断的规划释放到客户端。
// 多协议入口共享此夹具，避免为每个分支复制一套模拟转换逻辑。
func reasoningChatFixture(t *testing.T, content, reasoning string, stream, terminal bool) string {
	t.Helper()
	// 包级扫描超时在单包定向运行时可能尚未初始化。
	// 显式设置并恢复，避免测试依赖其他测试先执行。
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	// 非流式加入两层扩展字段，验证清理不会丢弃供应商附加数据。
	// 固定快照同时检查返回 usage 没有按清理后的短正文被重写。
	if !stream {
		raw, err := common.Marshal(map[string]any{"id": "chat_guard", "model": "deepseek-v4.1-flash", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": content, "reasoning_content": reasoning, "vendor": true}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 42, "completion_tokens": 17, "total_tokens": 59}, "vendor": "keep"})
		require.NoError(t, err)
		return string(raw)
	}
	// SSE 先发送推理，再发送正文，符合常见兼容服务的增量时序。
	// 迟到的推理由后面的独立边界用例覆盖，不靠随机片段排列。
	var body strings.Builder
	fields := []struct{ name, text string }{{"reasoning_content", reasoning}, {"content", content}}
	for _, field := range fields {
		// 使用 rune 切分而非破坏 UTF-8，避免把编码错误误认为分离故障。
		// 单字符事件比整块标签更能复现生产环境的分帧边界。
		for _, char := range field.text {
			raw, err := common.Marshal(map[string]any{"id": "chat_guard", "model": "deepseek-v4.1-flash", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{field.name: string(char), "vendor": true}}}})
			require.NoError(t, err)
			fmt.Fprintf(&body, "data: %s\n\n", raw)
		}
	}
	// 结束原因和 usage 分开，让尾部用量不能充当文本终态。
	// 缺少结束原因的用例仍有 [DONE]，用于检验不能仅凭传输结束放行。
	if terminal { // 传输结束与模型候选终态是两个独立条件。
		body.WriteString("data: {\"id\":\"chat_guard\",\"model\":\"deepseek-v4.1-flash\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
	}
	body.WriteString("data: {\"id\":\"chat_guard\",\"model\":\"deepseek-v4.1-flash\",\"choices\":[],\"usage\":{\"prompt_tokens\":42,\"completion_tokens\":17,\"total_tokens\":59}}\n\ndata: [DONE]\n\n")
	return body.String()
}

// 只读取协议公开的文本字段，终态快照不能作为额外正文重复拼接。
// Chat 与 Responses 分别按自己的事件契约累计，而非读取内部清理状态。
// 这样能检测泄漏、丢字、重复发送以及标签被拆帧后的残留。
func reasoningClientText(t *testing.T, body string, stream, responses bool) (string, string) {
	t.Helper()
	var content, reasoning strings.Builder
	// 普通响应在返回错误前不应写入任何 body；空输出是合法的失败证据。
	// 避免为了验证异常而要求处理器伪造一个成功 JSON。
	if body == "" {
		return "", ""
	}
	frames := []string{body}
	if stream {
		frames = strings.Split(body, "\n")
	}
	for _, frame := range frames {
		// SSE 注释、事件名与 [DONE] 都不是用户文本，不参与正文累计。
		// 同时要求每个 data 载荷仍是可解析 JSON。
		if stream {
			if !strings.HasPrefix(frame, "data: ") {
				continue
			}
			frame = strings.TrimPrefix(frame, "data: ") // 仅解码数据载荷，保留文本自己的空白。
			if frame == "[DONE]" {
				continue
			}
		}
		require.True(t, gjson.Valid(frame), frame) // 清理后仍须满足客户端 JSON 契约。
		root := gjson.Parse(frame)
		// Responses 只累计 delta，忽略 done 和 completed 中的完整快照。
		// 否则测试本身会把同一份输出加两次，遮蔽真实重复问题。
		if responses {
			if stream {
				switch root.Get("type").String() {
				case "response.output_text.delta":
					content.WriteString(root.Get("delta").String())
				case "response.reasoning_summary_text.delta":
					reasoning.WriteString(root.Get("delta").String())
				}
			} else {
				// 普通 Responses 按 output 项类型分离，不能遍历所有文字兜底。
				// 正文与摘要各自进入一条结果，工具参数不属于自然语言正文。
				for _, item := range root.Get("output").Array() {
					switch item.Get("type").String() {
					case "message":
						for _, part := range item.Get("content").Array() {
							content.WriteString(part.Get("text").String())
						}
					case "reasoning":
						for _, part := range item.Get("summary").Array() {
							reasoning.WriteString(part.Get("text").String())
						}
					}
				}
			}
			continue
		}
		// Chat 普通与流式只有字段容器不同，文本契约保持相同。
		// 不通过字面字符串搜索断言，避免转义后的 JSON 造成误报。
		field := "message"
		if stream {
			field = "delta"
		}
		for _, choice := range root.Get("choices").Array() {
			message := choice.Get(field)
			content.WriteString(message.Get("content").String())
			reasoning.WriteString(message.Get("reasoning_content").String())
		}
	}
	return content.String(), reasoning.String()
}

// 反方向 Responses→Chat 没有正文时，任何推理明文都不能成为正文兜底。
// 覆盖正文为空与缺失两种形式，并确认推理摘要也继续正常保留。
// 该回归通过 RelayKit 公共接口执行，与宿主的 DeepSeek 策略无关。
func TestResponsesReasoningOnlyNeverBecomesContent(t *testing.T) {
	// 供应商可能用不同的文本部件类型承载 reasoning.content。
	// 归属应由外层 reasoning 决定，内部名字不能让它成为正式正文。
	for _, partType := range []string{"reasoning_text", "text", "output_text"} {
		t.Run(partType, func(t *testing.T) {
			response := &dto.OpenAIResponsesResponse{Output: []dto.ResponsesOutput{{Type: "reasoning", Content: []dto.ResponsesOutputContent{{Type: partType, Text: "内部规划"}}}, {Type: "message", Role: "assistant"}}}
			// 空的 assistant message 必须保持空，不得激活跨 output 兜底。
			// 推理仍可用于客户端展示或后续历史，不因修复而全部丢弃。
			chat, _, err := relayconvert.ResponsesResponseToChatCompletionsResponse(response, "chat_guard")
			require.NoError(t, err)
			require.Len(t, chat.Choices, 1)
			assert.Empty(t, chat.Choices[0].Message.StringContent())               // 空 message 不得激活跨项目正文回退。
			assert.Equal(t, "内部规划", chat.Choices[0].Message.GetReasoningContent()) // 推理归属保持可用。
		})
	}
}

// 断流时没有可验证的最终正文，必须失败而不是泄漏暂存文本。
// 非 DeepSeek 模型保持既有返回语义，不因本次兼容修复增加缓冲或清理。
// 同时保护渠道扩展字段，防止 DTO 重编码把未知字段一起丢弃。
func TestDeepSeekReasoningBoundary(t *testing.T) {
	// 这里不能只检查错误码，还要确认此前的事件没有泄漏暂存内容。
	// 上游 [DONE] 不足以证明某个候选的正文已完成。
	t.Run("无终态", func(t *testing.T) {
		c, w, resp, info := newResponsesChatTestContext(t, reasoningChatFixture(t, "待确认正文", "内部规划", true, false), true)
		info.UpstreamModelName = "deepseek-v4.1-flash"
		info.RelayMode = relayconstant.RelayModeChatCompletions
		_, err := OaiStreamHandler(c, info, resp)
		require.NotNil(t, err)
		got, _ := reasoningClientText(t, w.Body.String(), true, false)
		assert.Empty(t, got)
	})
	// 工具轮可能只有规划和调用，清理规划不应取消真实调用。
	// 兼容开关即使被管理员开启，也不能将清理后的推理重新混入正文。
	for _, responses := range []bool{false, true} {
		t.Run(fmt.Sprintf("工具轮/responses=%t", responses), func(t *testing.T) {
			body := reasoningChatFixture(t, "内部规划", "内部规划", true, true)
			body = strings.ReplaceAll(body, `"finish_reason":"stop"`, `"finish_reason":"tool_calls"`)
			tool := `data: {"id":"chat_guard","model":"deepseek-v4.1-flash","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_guard","type":"function","function":{"name":"lookup","arguments":"{}"}}]}}]}` + "\n\n"
			c, w, resp, info := newResponsesChatTestContext(t, tool+body, true)
			info.UpstreamModelName = "deepseek-v4.1-flash"
			info.RelayMode = relayconstant.RelayModeChatCompletions
			// 故意启用反向开关，防止正常路径在清理后重新注入推理。
			// 真实工具输出作为本轮有效结果，不强制要求自然语言正文。
			info.ChannelSetting.ThinkingToContent = true // 特意覆盖可能重新引入泄漏的配置。
			var apiErr *types.NewAPIError
			if responses {
				info.RelayFormat = types.RelayFormatOpenAIResponses
				_, apiErr = OaiChatToResponsesStreamHandler(c, info, resp)
			} else {
				_, apiErr = OaiStreamHandler(c, info, resp)
			}
			require.Nil(t, apiErr)
			content, reasoning := reasoningClientText(t, w.Body.String(), true, responses)
			assert.Empty(t, content)
			assert.Equal(t, "内部规划", reasoning) // 去重之后只有一份完整推理。
			// 仅仅隐藏文本不足以通过：调用名和关联 ID 也必须保留下来。
			// 后续客户端才能继续执行同一个工具轮。
			assert.Contains(t, w.Body.String(), `"name":"lookup"`)
			assert.Contains(t, w.Body.String(), `call_guard`)
		})
	}
	// 重复判断不能依赖字段出现顺序，某些兼容上游在正文之后才回传推理。
	// null 的标准字段允许退到 reasoning 别名，与 DTO 的可选字段规则一致。
	t.Run("迟到推理别名", func(t *testing.T) {
		body := `data: {"id":"chat_guard","choices":[{"index":0,"delta":{"content":"内部规划\n收到"}}]}` + "\n\n" +
			`data: {"id":"chat_guard","choices":[{"index":0,"delta":{"reasoning_content":null,"reasoning":"内部规划"},"finish_reason":"stop"}],"usage":{"prompt_tokens":42,"completion_tokens":17,"total_tokens":59}}` + "\n\ndata: [DONE]\n\n"
		c, w, resp, info := newResponsesChatTestContext(t, body, true)
		info.UpstreamModelName = "deepseek-v4.1-flash"
		info.RelayMode = relayconstant.RelayModeChatCompletions
		oldTimeout := constant.StreamingTimeout
		constant.StreamingTimeout = 30
		t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
		// 推理在末帧才到达，之前缓存的 content 仍必须参与最终去重。
		// 若按首帧决定是否透传，这个用例会在正文里留下内部规划。
		_, apiErr := OaiStreamHandler(c, info, resp)
		require.Nil(t, apiErr)
		content, reasoning := reasoningClientText(t, w.Body.String(), true, false)
		assert.Equal(t, "收到", content) // 只允许确定的正式回答留下。
		assert.Equal(t, "内部规划", reasoning)
	})
	// 用量缺失时仍按清理前的原始生成文本估算，不能因隐藏推理少记输出。
	// 普通 Chat 的估算包含两个原字段，Responses 路径保留其既有正文计量合同。
	for _, stream := range []bool{false, true} {
		for _, responses := range []bool{false, true} {
			t.Run(fmt.Sprintf("无用量/stream=%t/responses=%t", stream, responses), func(t *testing.T) {
				original := "<thinking>内部规划</thinking>收到"
				body := reasoningChatFixture(t, original, "", stream, true)
				if stream {
					body = strings.ReplaceAll(body, `,"usage":{"prompt_tokens":42,"completion_tokens":17,"total_tokens":59}`, "")
				} else {
					var err error
					body, err = sjson.Delete(body, "usage")
					require.NoError(t, err)
				}
				c, w, resp, info := newResponsesChatTestContext(t, body, stream)
				info.UpstreamModelName = "deepseek-v4.1-flash"
				info.RelayMode = relayconstant.RelayModeChatCompletions
				// 期望值调用既有公共计量入口，不另写一套 token 算法。
				// 比较的是原始生成文本，不能用清理后的“收到”自证正确。
				expected := service.ResponseText2Usage(c, original, info.UpstreamModelName, info.GetEstimatePromptTokens())
				var usage *dto.Usage
				var apiErr *types.NewAPIError
				if responses {
					info.RelayFormat = types.RelayFormatOpenAIResponses
					if stream {
						usage, apiErr = OaiChatToResponsesStreamHandler(c, info, resp)
					} else {
						usage, apiErr = OaiChatToResponsesHandler(c, info, resp)
					}
				} else if stream {
					usage, apiErr = OaiStreamHandler(c, info, resp)
				} else {
					usage, apiErr = OpenaiHandler(c, info, resp)
				}
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				assert.Equal(t, expected.CompletionTokens, usage.CompletionTokens) // 无快照时也不能少计隐藏生成。
				content, _ := reasoningClientText(t, w.Body.String(), stream, responses)
				assert.Equal(t, "收到", content)
			})
		}
	}
	// 整轮缓冲必须受现有配置上限约束，不能只限制单个 SSE 事件。
	// 两帧分别小于一 MiB，总和超过一 MiB，确定性覆盖累计上限。
	t.Run("累计缓冲超限", func(t *testing.T) {
		oldLimit, oldTimeout := constant.StreamScannerMaxBufferMB, constant.StreamingTimeout
		constant.StreamScannerMaxBufferMB, constant.StreamingTimeout = 1, 30
		t.Cleanup(func() { constant.StreamScannerMaxBufferMB, constant.StreamingTimeout = oldLimit, oldTimeout })
		// 使用纯 ASCII 控制字节数，JSON 转义不会改变载荷规模。
		// 此处不是压力测试，只触及上限两侧的明确边界。
		payload := strings.Repeat("x", (1<<19)+1)
		data, err := common.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]string{"content": payload}}}})
		require.NoError(t, err)
		c, w, resp, info := newResponsesChatTestContext(t, fmt.Sprintf("data: %s\n\ndata: %s\n\n", data, data), true)
		info.UpstreamModelName = "deepseek-v4.1-flash"
		info.RelayMode = relayconstant.RelayModeChatCompletions
		// 上限异常须在候选终态前停止，不可回退到原样发送暂存正文。
		// 原有错误合同同时保证不会自动重放这个已开始生成的请求。
		_, apiErr := OaiStreamHandler(c, info, resp)
		require.NotNil(t, apiErr)
		assert.Contains(t, apiErr.Error(), "exceeds stream size limit")
		assert.True(t, types.IsSkipRetryError(apiErr))
		content, _ := reasoningClientText(t, w.Body.String(), true, false)
		assert.Empty(t, content)
	})
	// 不连续 index 与交错候选是兼容协议的有效输入，不能按数组位置共享推理。
	// 两个候选都带重复前缀，分别恢复各自的答案。
	t.Run("候选隔离", func(t *testing.T) {
		body := `data: {"id":"chat_guard","choices":[{"index":7,"delta":{"content":"规划甲答甲","reasoning_content":"规划甲"}},{"index":2,"delta":{"content":"规划乙答乙","reasoning_content":"规划乙"}}]}` + "\n\n" +
			`data: {"id":"chat_guard","choices":[{"index":2,"delta":{},"finish_reason":"stop"},{"index":7,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":42,"completion_tokens":17,"total_tokens":59}}` + "\n\ndata: [DONE]\n\n"
		c, w, resp, info := newResponsesChatTestContext(t, body, true)
		info.UpstreamModelName = "deepseek-v4.1-flash"
		info.RelayMode = relayconstant.RelayModeChatCompletions
		oldTimeout := constant.StreamingTimeout
		constant.StreamingTimeout = 30
		t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
		// 完成帧把数组顺序反转，强制检验状态是否依据真正的 index。
		// 只在输出末帧累计文本，先前的两份待确认内容不能泄漏。
		_, apiErr := OaiStreamHandler(c, info, resp)
		require.Nil(t, apiErr)
		content, reasoning := reasoningClientText(t, w.Body.String(), true, false)
		assert.Equal(t, "答乙答甲", content)
		assert.Equal(t, "规划乙规划甲", reasoning)
	})
	// 供应商回显仍写 DeepSeek，真实上游却设为其他模型。
	// 该场景保护策略只根据 RelayInfo 的最终路由模型触发。
	t.Run("其他模型不改写", func(t *testing.T) {
		c, w, resp, info := newResponsesChatTestContext(t, reasoningChatFixture(t, "<think>示例</think>", "", false, true), false)
		info.UpstreamModelName = "gpt-test"
		_, err := OpenaiHandler(c, info, resp)
		require.Nil(t, err)
		assert.Equal(t, "<think>示例</think>", gjson.Get(w.Body.String(), "choices.0.message.content").String())
	})
	// 既检查顶层扩展，也检查 message 内扩展，覆盖两种常见丢字段位置。
	// 这项断言要求对原始 JSON 打补丁，而不是重建精简响应。
	t.Run("扩展字段保留", func(t *testing.T) {
		c, w, resp, info := newResponsesChatTestContext(t, reasoningChatFixture(t, "<think>内部规划</think>收到", "", false, true), false)
		info.UpstreamModelName = "deepseek-v4.1-flash"
		_, err := OpenaiHandler(c, info, resp)
		require.Nil(t, err)
		assert.Equal(t, "keep", gjson.Get(w.Body.String(), "vendor").String())        // 顶层供应商扩展不应丢失。
		assert.True(t, gjson.Get(w.Body.String(), "choices.0.message.vendor").Bool()) // 消息内扩展同样保留。
	})
}
