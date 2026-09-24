package openai

import (
	"bufio"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// OpenCodeSSEToNonStreamHandler buffers a chat.completions SSE stream from the
// OpenCode free tier (which only answers stream:true) and writes a single
// non-stream JSON body for clients that requested stream:false.
func OpenCodeSSEToNonStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	model := info.UpstreamModelName
	var responseID string
	var createAt int64
	role := "assistant"
	var content strings.Builder
	var reasoning strings.Builder
	toolCalls := make([]dto.ToolCallResponse, 0)
	toolCallIndex := make(map[int]int)
	finishReason := ""
	usage := &dto.Usage{}
	containStreamUsage := false

	scanner := helper.NewStreamScanner(resp.Body)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[len("data:"):])
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}

		var errProbe struct {
			Error any `json:"error"`
		}
		if err := common.UnmarshalJsonStr(data, &errProbe); err == nil && errProbe.Error != nil {
			if oaiErr := dto.GetOpenAIError(errProbe.Error); oaiErr != nil && oaiErr.Type != "" {
				return nil, types.WithOpenAIError(*oaiErr, resp.StatusCode)
			}
			return nil, types.NewOpenAIError(fmt.Errorf("opencode stream error"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}

		var chunk dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &chunk); err != nil {
			logger.LogError(c, "failed to unmarshal opencode stream chunk: "+err.Error())
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		if chunk.Id != "" {
			responseID = chunk.Id
		}
		if chunk.Created != 0 {
			createAt = chunk.Created
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		if service.ValidUsage(chunk.Usage) {
			usage = dto.MergeUsageNonZero(usage, chunk.Usage)
			containStreamUsage = true
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Role != "" {
				role = choice.Delta.Role
			}
			content.WriteString(choice.Delta.GetContentString())
			reasoning.WriteString(choice.Delta.GetReasoningContent())
			for _, tc := range choice.Delta.ToolCalls {
				idx := len(toolCalls)
				if tc.Index != nil {
					idx = *tc.Index
				}
				if existing, ok := toolCallIndex[idx]; ok {
					if tc.ID != "" {
						toolCalls[existing].ID = tc.ID
					}
					if tc.Type != nil {
						toolCalls[existing].Type = tc.Type
					}
					if tc.Function.Name != "" {
						toolCalls[existing].Function.Name = tc.Function.Name
					}
					toolCalls[existing].Function.Arguments += tc.Function.Arguments
					continue
				}
				merged := tc
				if merged.Index == nil {
					merged.SetIndex(idx)
				}
				toolCallIndex[idx] = len(toolCalls)
				toolCalls = append(toolCalls, merged)
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				finishReason = *choice.FinishReason
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	if finishReason == "" {
		// Missing finish_reason (including empty streams) still yields a valid stop.
		finishReason = types.FinishReasonStop
	}
	if !containStreamUsage {
		usage = service.ResponseText2Usage(c, content.String()+reasoning.String(), model, info.GetEstimatePromptTokens())
	}
	applyUsagePostProcessing(info, usage, nil)

	if responseID == "" {
		responseID = helper.GetResponseID(c)
	}
	if createAt == 0 {
		createAt = time.Now().Unix()
	}

	msg := dto.Message{Role: role}
	msg.SetStringContent(content.String())
	if reasoning.Len() > 0 {
		rc := reasoning.String()
		msg.ReasoningContent = &rc
	}
	if len(toolCalls) > 0 {
		msg.SetToolCalls(toolCalls)
		for _, tc := range toolCalls {
			if tc.Function.Name != "" {
				info.CountBillableToolCall(dto.BuildInCallFunctionCall, tc.Function.Name)
			}
		}
	}

	out := dto.OpenAITextResponse{
		Id:      responseID,
		Object:  "chat.completion",
		Created: createAt,
		Model:   model,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: finishReason,
			},
		},
		Usage: *usage,
	}
	for _, choice := range out.Choices {
		if choice.FinishReason == constant.FinishReasonContentFilter {
			common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "openai_finish_reason=content_filter")
			break
		}
	}

	body, err := common.Marshal(out)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	// Override upstream event-stream Content-Type so clients receive JSON.
	resp.Header.Set("Content-Type", "application/json")
	service.IOCopyBytesGracefully(c, resp, body)
	return usage, nil
}
