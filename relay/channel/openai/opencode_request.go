package openai

import (
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/samber/lo"
)

func prepareOpenCodeFreeTierRequest(info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) {
	if info == nil || request == nil || info.ChannelMeta == nil {
		return
	}
	if info.ChannelType != constant.ChannelTypeOpenCode {
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
