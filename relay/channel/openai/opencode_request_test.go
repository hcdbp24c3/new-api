package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openCodeRelayInfo(clientStream bool) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenCode,
			UpstreamModelName: "mimo-v2.5-free",
		},
		IsStream: clientStream,
	}
}

func toolNames(tools []dto.ToolCallRequest) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Function.Name)
	}
	return names
}

func countName(names []string, name string) int {
	n := 0
	for _, existing := range names {
		if existing == name {
			n++
		}
	}
	return n
}

func TestPrepareOpenCodeFreeTierRequestForcesStream(t *testing.T) {
	info := openCodeRelayInfo(false)
	req := &dto.GeneralOpenAIRequest{
		Model:  "mimo-v2.5-free",
		Stream: lo.ToPtr(false),
	}
	prepareOpenCodeFreeTierRequest(info, req)
	require.NotNil(t, req.Stream)
	assert.True(t, *req.Stream)
	assert.True(t, info.ForceOpenCodeStreamAgg, "non-stream client must mark aggregation")
}

func TestPrepareOpenCodeFreeTierRequestForcesStreamWhenClientOmitsStream(t *testing.T) {
	info := openCodeRelayInfo(false)
	req := &dto.GeneralOpenAIRequest{Model: "mimo-v2.5-free"}
	prepareOpenCodeFreeTierRequest(info, req)
	require.NotNil(t, req.Stream)
	assert.True(t, *req.Stream)
	assert.True(t, info.ForceOpenCodeStreamAgg, "nil stream is treated as non-stream client")
}

func TestPrepareOpenCodeFreeTierRequestKeepsClientStream(t *testing.T) {
	info := openCodeRelayInfo(true)
	req := &dto.GeneralOpenAIRequest{Model: "mimo-v2.5-free", Stream: lo.ToPtr(true)}
	prepareOpenCodeFreeTierRequest(info, req)
	require.NotNil(t, req.Stream)
	assert.True(t, *req.Stream)
	assert.False(t, info.ForceOpenCodeStreamAgg)
}

func TestPrepareOpenCodeFreeTierRequestInjectsShellAndRead(t *testing.T) {
	info := openCodeRelayInfo(true)
	req := &dto.GeneralOpenAIRequest{Model: "mimo-v2.5-free", Stream: lo.ToPtr(true)}
	prepareOpenCodeFreeTierRequest(info, req)
	names := toolNames(req.Tools)
	assert.Contains(t, names, "shell")
	assert.Contains(t, names, "read")
}

func TestPrepareOpenCodeFreeTierRequestSkipsShellWhenBashPresent(t *testing.T) {
	info := openCodeRelayInfo(true)
	req := &dto.GeneralOpenAIRequest{
		Model:  "mimo-v2.5-free",
		Stream: lo.ToPtr(true),
		Tools: []dto.ToolCallRequest{
			{Type: "function", Function: dto.FunctionRequest{Name: "bash"}},
			{Type: "function", Function: dto.FunctionRequest{Name: "read"}},
		},
	}
	prepareOpenCodeFreeTierRequest(info, req)
	names := toolNames(req.Tools)
	assert.Equal(t, 0, countName(names, "shell"), "bash already covers shell")
	assert.Equal(t, 1, countName(names, "read"))
	assert.Equal(t, 1, countName(names, "bash"))
}

func TestPrepareOpenCodeFreeTierRequestDoesNotDuplicateTools(t *testing.T) {
	info := openCodeRelayInfo(true)
	req := &dto.GeneralOpenAIRequest{
		Model:  "mimo-v2.5-free",
		Stream: lo.ToPtr(true),
		Tools: []dto.ToolCallRequest{
			{Type: "function", Function: dto.FunctionRequest{Name: "shell"}},
			{Type: "function", Function: dto.FunctionRequest{Name: "read"}},
		},
	}
	prepareOpenCodeFreeTierRequest(info, req)
	names := toolNames(req.Tools)
	assert.Equal(t, 1, countName(names, "shell"))
	assert.Equal(t, 1, countName(names, "read"))
}

func TestPrepareOpenCodeFreeTierRequestSkipsPaidChannelTypes(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI},
	}
	req := &dto.GeneralOpenAIRequest{Stream: lo.ToPtr(false)}
	prepareOpenCodeFreeTierRequest(info, req)
	require.NotNil(t, req.Stream)
	assert.False(t, *req.Stream)
	assert.False(t, info.ForceOpenCodeStreamAgg)
	assert.Empty(t, req.Tools)
}
