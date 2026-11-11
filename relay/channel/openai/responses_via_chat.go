package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func OaiChatToResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var chatResp dto.OpenAITextResponse
	if err := common.Unmarshal(body, &chatResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := chatResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	// 缺少 usage 时沿用清理前的正文估算，避免文本归属修正改变计量输入。
	// 有上游快照时始终使用其原值，不按清理后的字数重算。
	var originalText string
	if len(chatResp.Choices) > 0 {
		originalText = chatResp.Choices[0].Message.StringContent()
	}
	guard := newChatReasoningGuard(c, info)
	if _, err := guard.response(body, &chatResp); err != nil {
		return nil, err
	}
	info.ObserveResponseModel(chatResp.Model)
	if responseID := helper.GetResponseID(c); responseID != "" {
		chatResp.Id = responseID
	}
	convertResult, err := service.ConvertResponse(c, info, types.RelayFormatOpenAIResponses, &chatResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	responsesResp, ok := convertResult.Value.(*dto.OpenAIResponsesResponse)
	if !ok {
		return nil, types.NewOpenAIError(fmt.Errorf("expected OpenAI responses response, got %T", convertResult.Value), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	usage := convertResult.Usage
	if usage == nil || usage.TotalTokens == 0 {
		text := service.ExtractOutputTextFromResponses(responsesResp)
		if guard != nil {
			text = originalText
		}
		usage = service.ResponseText2Usage(c, text, info.UpstreamModelName, info.GetEstimatePromptTokens())
		responsesResp.Usage = relayconvert.UsageFromChatUsage(usage)
	}

	responseBody, err := common.Marshal(responsesResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)
	return usage, nil
}

func OaiChatToResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	responseID := helper.GetResponseID(c)
	state, err := relayconvert.NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, relayconvert.ResponseStreamOptions{
		ID:                 responseID,
		Model:              info.UpstreamModelName,
		EmitSequenceNumber: true,
	})
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	// 原始文本单独参与无 usage 的估算，缓冲文本不会漏计或被重复计算。
	// guard 按请求创建，与 Responses 转换器一样不跨轮共享状态。
	guard := newChatReasoningGuard(c, info)
	var originalText strings.Builder
	streamErr := (*types.NewAPIError)(nil)
	// upstreamFailure is the upstream or conversion failure the client was
	// told about with response.failed.
	var upstreamFailure *types.NewAPIError

	sendEvent := func(event relayconvert.ChatToResponsesStreamEvent) bool {
		data, err := common.Marshal(event.Payload)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
			return false
		}
		if err := helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: event.Type}, string(data)); err != nil {
			// When the client went away the upstream already produced output,
			// so the stream stops and settles normally instead of failing; any
			// other write error stays a server error.
			if c.Request.Context().Err() != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
				return false
			}
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
		return true
	}
	failResponsesStream := func(err error) bool {
		failureResults, handled := state.FailResponsesStream("server_error", err.Error(), "")
		if !handled {
			return false
		}
		for _, result := range failureResults {
			event, ok := result.Value.(relayconvert.ChatToResponsesStreamEvent)
			if !ok {
				streamErr = types.NewOpenAIError(fmt.Errorf("expected OAI responses stream event, got %T", result.Value), types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return true
			}
			if !sendEvent(event) {
				return true
			}
		}
		return true
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			stopStream(sr, streamErr)
			return
		}

		var errorResp dto.OpenAITextResponse
		if err := common.UnmarshalJsonStr(data, &errorResp); err == nil {
			if oaiError := errorResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
				if failResponsesStream(fmt.Errorf("%s", oaiError.Message)) {
					// The client got response.failed, so the request is never
					// retried; the recorded error keeps the stream failed.
					upstreamFailure = types.WithOpenAIError(*oaiError, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
					sr.Stop(upstreamFailure)
					return
				}
				streamErr = types.WithOpenAIError(*oaiError, resp.StatusCode)
				stopStream(sr, streamErr)
				return
			}
		}

		var chunk dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &chunk); err != nil {
			logger.LogError(c, common.LogText("failed to unmarshal chat stream response: %s", err.Error()))
			if failResponsesStream(err) {
				upstreamFailure = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
				sr.Stop(upstreamFailure)
				return
			}
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			stopStream(sr, streamErr)
			return
		}

		// 保存转换器原先计量的正文、推理与工具参数，清理不影响上游用量。
		// 只有 DeepSeek 需要此副本，其他模型继续使用已有 state.UsageText。
		if guard != nil {
			for _, choice := range chunk.Choices {
				originalText.WriteString(choice.Delta.GetReasoningContent())
				originalText.WriteString(choice.Delta.GetContentString())
				for _, call := range choice.Delta.ToolCalls {
					originalText.WriteString(call.Function.Arguments)
				}
			}
			cleaned, err := guard.stream(data)
			if err != nil {
				failResponsesStream(err)
				streamErr = err
				stopStream(sr, err)
				return
			}
			// JSON 中省略的字段不会清空已有 DTO，必须先清零以免旧正文复活。
			// 清理器保留的角色、工具、引用和 usage 随后由同一事件重新填充。
			chunk = dto.ChatCompletionsStreamResponse{}
			if err := common.UnmarshalJsonStr(cleaned, &chunk); err != nil {
				streamErr = reasoningOutputError("invalid normalized upstream chat stream")
				failResponsesStream(streamErr)
				stopStream(sr, streamErr)
				return
			}
		}
		info.ObserveResponseModel(chunk.Model)
		results, err := service.ConvertStreamResponseChunk(c, info, state, &chunk)
		if err != nil {
			if failResponsesStream(err) {
				upstreamFailure = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
				sr.Stop(upstreamFailure)
				return
			}
			streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
			stopStream(sr, streamErr)
			return
		}
		for _, result := range results {
			event, ok := result.Value.(relayconvert.ChatToResponsesStreamEvent)
			if !ok {
				streamErr = types.NewOpenAIError(fmt.Errorf("expected OAI responses stream event, got %T", result.Value), types.ErrorCodeBadResponse, http.StatusInternalServerError)
				stopStream(sr, streamErr)
				return
			}
			if !sendEvent(event) {
				stopStream(sr, streamErr)
				return
			}
		}
	})

	if streamErr != nil {
		return nil, streamErr
	}
	// 没有 choice 终态时不能把缓冲文本作为完整回答释放或标记 completed。
	// 客户端取消仍由既有取消路径处理，不额外创建上游异常。
	if c.Request.Context().Err() == nil {
		if err := guard.complete(); err != nil {
			failResponsesStream(err)
			info.StreamStatus.MarkFailed(string(err.GetErrorCode()), "upstream_error", err.StatusCode)
			return nil, err
		}
	}

	usage := state.Usage()
	noUpstreamUsage := usage == nil || usage.TotalTokens == 0
	if upstreamFailure != nil && noUpstreamUsage && state.UsageText() == "" {
		// The stream failed before any output reached the client, which
		// already received response.failed: the request is not charged.
		return nil, upstreamFailure
	}
	if noUpstreamUsage {
		text := state.UsageText()
		if guard != nil {
			text = originalText.String()
		}
		usage = service.ResponseText2Usage(c, text, info.UpstreamModelName, info.GetEstimatePromptTokens())
		state.SetUsage(usage)
	}
	if info.StreamStatus.EndReason == relaycommon.StreamEndReasonClientGone {
		return usage, nil
	}

	finalResults, err := service.FinalizeStreamResponse(c, info, state)
	if err != nil {
		if failResponsesStream(err) {
			return usage, streamErr
		}
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	for _, result := range finalResults {
		event, ok := result.Value.(relayconvert.ChatToResponsesStreamEvent)
		if !ok {
			return nil, types.NewOpenAIError(fmt.Errorf("expected OAI responses stream event, got %T", result.Value), types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
		if !sendEvent(event) {
			if streamErr != nil {
				return nil, streamErr
			}
			return usage, nil
		}
	}

	return usage, nil
}

// stopStream ends a converted stream from the scanner callback. A nil
// streamErr (the stream stops because the client went away) must reach
// StreamResult.Stop as an untyped nil; a typed nil *NewAPIError is a non-nil
// error that StreamResult would record as a failure and dereference.
func stopStream(sr *helper.StreamResult, streamErr *types.NewAPIError) {
	if streamErr == nil {
		sr.Stop(nil)
		return
	}
	sr.Stop(streamErr)
}
