package openai

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/samber/lo"
)

const (
	openCodeProtocolChat      = "chat"
	openCodeProtocolResponses = "responses"
	openCodeProtocolClaude    = "claude"
	openCodeProtocolGemini    = "gemini"
)

// openCodeUpstreamProtocol returns "" unless info is an OpenCode chat-relay request.
// Otherwise it maps the upstream model to the Zen endpoint family from the
// official docs table: claude-* → messages, gemini-* → models/{model},
// gpt-*/grok-*/muse-spark* → responses, everything else → chat completions.
func openCodeUpstreamProtocol(info *relaycommon.RelayInfo) string {
	if info == nil || info.ChannelMeta == nil ||
		info.ChannelType != constant.ChannelTypeOpenCode ||
		info.RelayMode != relayconstant.RelayModeChatCompletions {
		return ""
	}
	model := info.UpstreamModelName
	switch {
	case strings.HasPrefix(model, "claude-"):
		return openCodeProtocolClaude
	case strings.HasPrefix(model, "gemini-"):
		return openCodeProtocolGemini
	case strings.HasPrefix(model, "gpt-"), strings.HasPrefix(model, "grok-"),
		strings.HasPrefix(model, "muse-spark"):
		return openCodeProtocolResponses
	default:
		return openCodeProtocolChat
	}
}

func prepareOpenCodeFreeTierRequest(info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) {
	if info == nil || request == nil || info.ChannelMeta == nil {
		return
	}
	if info.ChannelType != constant.ChannelTypeOpenCode {
		return
	}
	switch openCodeUpstreamProtocol(info) {
	case openCodeProtocolClaude, openCodeProtocolGemini:
		// Paid-only lanes: forced stream is meaningless there (their DoResponse
		// never sets ForceOpenCodeStreamAgg) and the Zen free-tier gate tools
		// belong only to the chat/responses lanes.
		return
	}
	clientWantedStream := lo.FromPtrOr(request.Stream, false)
	stream := true
	request.Stream = &stream
	if !clientWantedStream {
		info.ForceOpenCodeStreamAgg = true
	}
	ensureOpenCodeGateTools(request)
}

// ensureOpenCodeGateTools declares shell (or keeps bash) + read when missing so
// the Zen free-tier gate accepts the request. Declarations only: if the model
// emits tool_calls for these names they pass through to the client as usual;
// the gateway does not execute them server-side.
func ensureOpenCodeGateTools(request *dto.GeneralOpenAIRequest) {
	have := make(map[string]struct{}, len(request.Tools))
	for _, t := range request.Tools {
		have[t.Function.Name] = struct{}{}
	}
	if _, ok := have["shell"]; !ok {
		if _, bashOK := have["bash"]; !bashOK {
			request.Tools = append(request.Tools, dto.ToolCallRequest{
				Type: "function",
				Function: dto.FunctionRequest{
					Name:        "shell",
					Description: "Execute a shell command",
					Parameters: map[string]any{
						"type":       "object",
						"properties": map[string]any{"command": map[string]any{"type": "string"}},
						"required":   []string{"command"},
					},
				},
			})
		}
	}
	if _, ok := have["read"]; !ok {
		request.Tools = append(request.Tools, dto.ToolCallRequest{
			Type: "function",
			Function: dto.FunctionRequest{
				Name:        "read",
				Description: "Read a file from disk",
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{"path": map[string]any{"type": "string"}},
					"required":   []string{"path"},
				},
			},
		})
	}
}
