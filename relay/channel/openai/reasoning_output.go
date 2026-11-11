// 本文件处理 DeepSeek 上游把推理混入 Chat 正文的兼容问题。
// 处理位置在客户端协议转换之前，因此原生 Chat 和 Responses 使用同一规则。
// 这里不修改请求、模型选择、价格、供应商用量或历史消息。
// 已确认的证据只有显式标签和独立推理字段，不对自然语言做语义分类。
// 缺少这些证据时保留上游文本，避免把正常的说明或清单误判为推理。
// 流式文本必须等到 choice 终态，才能识别迟到推理和孤立闭合标签。
// 客户端因此会延后看到文本，但工具事件不需要等待这项判断。
// 限制在真实上游名为 deepseek- 前缀的模型，不使用客户端别名触发策略。
// 推理转正文选项在这条兼容路径上不再生效，防止处理结果再次被逆转。
// 超限、缺少终态和无可交付正文均走明确失败，不透传暂存文本。
// 诊断只记录事件和 choice index，不记录任何用户文本或推理内容。
package openai

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// 只识别协议中明确的推理标签，不按语言、文风或规划关键词猜测正文。
// 忽略大小写及标签末尾空白，闭合与起始标签使用同一组名称。
var reasoningTag = regexp.MustCompile(`(?i)<(/?)(think|thinking|reasoning|thought)\s*>`)

// chatReasoningGuard 限定 DeepSeek Chat 返回，避免改变其他模型的文本语义。
// 每次上游尝试单独创建，状态不会跨用户、请求或重试共享。
// 全轮文本缓冲使迟到的推理和孤立闭合标签也能在下发前被识别。
// 角色、工具、usage 和供应商扩展字段仍由原处理路径继续转发。
type chatReasoningGuard struct {
	context  *gin.Context             // 使用原请求上下文关联清理日志。
	choices  map[int]*reasoningChoice // 多候选各自保留推理边界。
	buffered int                      // 只统计尚未释放的文本字节。
	limit    int                      // 整个请求共用现有扫描大小限制。
}

// reasoningChoice 按 choice index 隔离累积数据，多候选不会互相去重。
// 推理别名在入口归一化，输出只使用 reasoning_content 这一可信字段。
// 非文本输出保留有效性，避免把正常工具轮误判成只有推理的异常。
type reasoningChoice struct {
	content     strings.Builder // 正文在终态前不能被猜测为安全。
	reasoning   strings.Builder // 独立推理可能晚于正文到达。
	otherOutput bool            // 有真实工具或拒绝时允许空正文。
}

// 开关依据映射后的真实上游模型，客户端别名不参与兼容策略判断。
// 沿用现有扫描缓冲上限作为整轮累计上限，超限明确失败而非泄漏回退。
func newChatReasoningGuard(c *gin.Context, info *relaycommon.RelayInfo) *chatReasoningGuard {
	if info == nil || info.ChannelMeta == nil || !strings.HasPrefix(strings.ToLower(info.UpstreamModelName), "deepseek-") {
		return nil
	}
	limit := helper.DefaultMaxScannerBufferSize
	if constant.StreamScannerMaxBufferMB > 0 {
		limit = constant.StreamScannerMaxBufferMB << 20
	}
	return &chatReasoningGuard{context: c, choices: make(map[int]*reasoningChoice), limit: limit}
}

// reasoningOutputError 走既有上游失败合同；生成已发生，禁止自动重试扣费请求。
// 错误文本不携带推理、正文、渠道凭据或其他用户数据。
func reasoningOutputError(message string) *types.NewAPIError {
	return types.NewOpenAIError(fmt.Errorf("%s", message), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}

// response 清理完整非流式响应，只改文本字段而不重建整个供应商 DTO。
// 调用方应先完成原始用量提取，让内容清理不改变供应商的 token 快照。
// 返回原始 body 的快速路径保持未发生异常的响应字节和扩展字段。
func (g *chatReasoningGuard) response(body []byte, response *dto.OpenAITextResponse) ([]byte, *types.NewAPIError) {
	if g == nil {
		return body, nil
	}
	// 按数组位置修改 JSON，业务 index 只负责标识候选和诊断。
	// 多候选响应不能把前一候选的推理用于清理后一候选。
	for i, choice := range gjson.GetBytes(body, "choices").Array() {
		message := choice.Get("message")
		content := message.Get("content")
		// 此路径只处理文本 Chat；多模态内容由原有适配器保留。
		// null 正文的工具轮也不需要制造一个空的推理块。
		if content.Type != gjson.String {
			continue
		}
		reasoning := message.Get("reasoning_content")
		// 标准字段显式为空字符串时优先，null 则与 DTO 一样使用别名。
		// 不同时拼接两个别名，避免供应商兼容字段造成重复。
		if reasoning.Type == gjson.Null {
			reasoning = message.Get("reasoning")
		}
		text, thought, changed := separateReasoningText(content.String(), reasoning.String())
		if !changed {
			continue
		}
		// 只有被识别并移除的正文才触发此异常，保留原有纯推理/截断合同。
		// 工具或拒绝等独立输出存在时，不要求额外伪造一句自然语言回答。
		if strings.TrimSpace(text) == "" && content.String() != "" && choice.Get("finish_reason").String() == "stop" && !hasOtherChatOutput(message) {
			return nil, reasoningOutputError("upstream response contains reasoning but no final answer")
		}
		// 只补丁目标字段；重新序列化整个 DTO 会遗失供应商扩展。
		// 保留空 content 是为了区分工具轮和凭空生成的正式文本。
		prefix := "choices." + strconv.Itoa(i) + ".message."
		var err error
		body, err = sjson.SetBytes(body, prefix+"content", text)
		if err == nil {
			body, err = sjson.SetBytes(body, prefix+"reasoning_content", thought)
		}
		if err == nil {
			body, err = sjson.DeleteBytes(body, prefix+"reasoning")
		}
		if err != nil {
			return nil, reasoningOutputError("failed to normalize upstream reasoning fields")
		}
		// 日志能定位被清理的候选，但不会把被隐藏的内容写入监控。
		logger.LogWarn(g.context, fmt.Sprintf("upstream reasoning removed from content (choice=%d)", choice.Get("index").Int()))
	}
	// 转换器读取同一结果的 Choices，已提取的用量和其他响应元数据不变。
	// 使用全新 DTO，防止 JSON 缺失字段保留上一次解析出来的内容。
	var normalized dto.OpenAITextResponse
	if err := common.Unmarshal(body, &normalized); err != nil {
		return nil, reasoningOutputError("invalid normalized upstream chat response")
	}
	response.Choices = normalized.Choices
	return body, nil
}

// stream 按真实 index 暂存文本，终态到达时才释放清理后的完整文本。
// 不暂存工具参数或整个 SSE 事件，因此工具执行和用量事件不被阻塞。
// 只用 sjson 修改对应文本路径，保留模型、终态、引用及未知供应商字段。
func (g *chatReasoningGuard) stream(data string) (string, *types.NewAPIError) {
	if g == nil {
		return data, nil
	}
	// 不接受畸形 JSON 后再尝试提取片段，坏数据必须保持可定位。
	// 调用方把此错误交给现有 SSE 失败处理，不能自动重试。
	if !gjson.Valid(data) {
		return "", reasoningOutputError("invalid upstream chat stream JSON")
	}
	for i, choice := range gjson.Get(data, "choices").Array() {
		index := int(choice.Get("index").Int())
		// choice index 可以跳号，也可以在事件之间交错出现。
		// 用 index 查找状态，不能假设数组位置一直等于候选编号。
		state := g.choices[index]
		if state == nil {
			state = &reasoningChoice{}
			g.choices[index] = state
		}
		delta := choice.Get("delta")
		content := delta.Get("content").String()
		reasoning := delta.Get("reasoning_content")
		if reasoning.Type == gjson.Null {
			reasoning = delta.Get("reasoning")
		}
		// 上限按所有 choice 的累积字节共同计数，防止多候选绕过约束。
		// 不在坏输入上降级透传，否则缓冲溢出会重新打开推理泄漏路径。
		added := len(content) + len(reasoning.String())
		if added > g.limit-g.buffered {
			return "", reasoningOutputError("upstream reasoning buffer exceeds stream size limit")
		}
		state.content.WriteString(content)
		state.reasoning.WriteString(reasoning.String())
		state.otherOutput = state.otherOutput || hasOtherChatOutput(delta)
		g.buffered += added
		prefix := "choices." + strconv.Itoa(i) + ".delta."
		var err error
		for _, field := range []string{"content", "reasoning_content", "reasoning"} {
			data, err = sjson.Delete(data, prefix+field)
			if err != nil {
				return "", reasoningOutputError("failed to buffer upstream reasoning fields")
			}
		}
		// 空或 null 的结束原因为增量阶段，继续隐藏两种文本字段。
		// 事件中的其他字段已经保留，因此角色和工具仍可及时到达。
		if choice.Get("finish_reason").String() == "" {
			continue
		}
		text, thought, changed := separateReasoningText(state.content.String(), state.reasoning.String())
		// 成功终态若只剩被移走的推理，明确失败；length 等上游截断保持原义。
		// 下发之前完成判断，所以异常正文从未进入任何客户端协议转换器。
		if changed && strings.TrimSpace(text) == "" && state.content.Len() > 0 && choice.Get("finish_reason").String() == "stop" && !state.otherOutput {
			return "", reasoningOutputError("upstream response contains reasoning but no final answer")
		}
		if text != "" {
			data, err = sjson.Set(data, prefix+"content", text)
		}
		if err == nil && thought != "" {
			data, err = sjson.Set(data, prefix+"reasoning_content", thought)
		}
		if err != nil {
			return "", reasoningOutputError("failed to emit normalized upstream reasoning fields")
		}
		if changed {
			logger.LogWarn(g.context, fmt.Sprintf("upstream reasoning removed from content (choice=%d)", index))
		}
		// 候选完成后立即释放引用和累计配额，不保留整条会话历史。
		// 后续 usage-only 事件没有 choice，因此不会再次发送这些文本。
		g.buffered -= state.content.Len() + state.reasoning.Len()
		delete(g.choices, index)
	}
	return data, nil
}

// complete 禁止在缺少终态时将待定文本当正文释放；客户端取消由调用方处理。
// 此检查也覆盖上游提前发送 [DONE]、EOF 或遗漏某个 choice 终态的情况。
func (g *chatReasoningGuard) complete() *types.NewAPIError {
	if g != nil && g.buffered > 0 {
		return reasoningOutputError("upstream chat stream ended before reasoning could be separated")
	}
	return nil
}

// hasOtherChatOutput 只判断真实的非文本有效载荷，不用空字段冒充有效输出。
// 标准工具调用、拒绝和音频仍保持各自的协议语义与输出位置。
func hasOtherChatOutput(message gjson.Result) bool {
	return len(message.Get("tool_calls").Array()) > 0 || message.Get("function_call.name").String() != "" || message.Get("refusal").String() != "" || message.Get("audio.data").String() != ""
}

// separateReasoningText 是普通与流式唯一的内容分离规则。
// 优先信任独立推理字段，但其与正文相同的部分不能再次充当正式回答。
// 无标签、无重复证据的自然语言保持原样，不试图识别或猜测模型意图。
func separateReasoningText(content, reasoning string) (string, string, bool) {
	originalContent, originalReasoning := content, reasoning
	// 部分上游只在 reasoning 中保留孤立闭合标签，两字段去标签后完全相同。
	// 只有确定二者表达同一文本时，才利用该闭合位置恢复推理与正文边界。
	// 独立推理中引用标签示例的常见情形不满足这个等式，不会被截断。
	if tags := reasoningTag.FindAllStringSubmatchIndex(reasoning, -1); len(tags) > 0 && tags[0][3] > tags[0][2] && strings.TrimSpace(reasoningTag.ReplaceAllString(reasoning, "")) == strings.TrimSpace(reasoningTag.ReplaceAllString(content, "")) {
		// 使用带标签字段中的原始偏移切分，避免去标签后偏移发生变化。
		// 正文和推理只各取一次，不能把闭合后的正式回答继续留在推理里。
		end := tags[0]
		content = reasoning[end[1]:]
		reasoning = reasoning[:end[0]]
	}
	// 正文与待隐藏片段分别累积，扫描过程中不会向外部发送任何字符。
	// 标签本身不进入两种文本；内部字符保留原顺序。
	var answer, extracted strings.Builder
	var tags []string
	pos := 0
	for _, tag := range reasoningTag.FindAllStringSubmatchIndex(content, -1) {
		// 正则只给出标签边界，区块归属由匹配栈决定。
		// 嵌套标签不意味着中间内容可以恢复为正文。
		segment := content[pos:tag[0]]
		if len(tags) > 0 {
			extracted.WriteString(segment)
		} else {
			answer.WriteString(segment)
		}
		closing := tag[3] > tag[2]
		name := strings.ToLower(content[tag[4]:tag[5]])
		if !closing {
			tags = append(tags, name)
		} else if len(tags) > 0 {
			// 错配闭合标签不能提前结束隐藏区，否则后续规划仍会泄漏。
			// 只接受栈顶的同名闭合；未闭合的剩余内容继续归为推理。
			if tags[len(tags)-1] == name {
				tags = tags[:len(tags)-1]
			}
		} else {
			// 没有起始标签时，闭合前的整段都是待隐藏的推理，不能仅删标签。
			// 已累积的正文也移入推理，避免把先前输出的规划留在客户端。
			extracted.WriteString(answer.String())
			answer.Reset()
		}
		pos = tag[1]
	}
	// 到末尾仍未闭合时，剩余字符全部属于隐藏区。
	// 已确认在区块外的前置正文仍可保留，不将整条正常回答一并丢弃。
	if len(tags) > 0 {
		extracted.WriteString(content[pos:])
	} else {
		answer.WriteString(content[pos:])
	}
	text := answer.String()
	// 完整的独立推理前缀只能保留一份；前缀之后的正式正文继续保留。
	// 对比忽略边界空白，移除时只清理新产生的前导空白，不改正文尾部。
	native := strings.TrimSpace(reasoning)
	if native != "" {
		if rest, ok := strings.CutPrefix(strings.TrimLeft(text, " \t\r\n"), native); ok {
			text = strings.TrimLeft(rest, " \t\r\n")
		}
	}
	// 提取的同一块若已被上游单独返回，不再追加第二份。
	// 若提取块更完整，则保留完整块；独立不重叠的推理按顺序追加。
	thought := strings.TrimSpace(extracted.String())
	if thought != "" && !strings.Contains(reasoning, thought) {
		if native == "" || strings.Contains(thought, native) {
			reasoning = thought
		} else {
			reasoning += "\n\n" + thought
		}
	}
	// 未识别到异常时保持字节不变，尤其保留正常回复的换行与缩进。
	// 清理产生的前导空白可以移除，正文原有尾部空白不被裁剪。
	changed := text != originalContent || reasoning != originalReasoning
	if changed {
		text = strings.TrimLeft(text, " \t\r\n")
	}
	return text, reasoning, changed
}
