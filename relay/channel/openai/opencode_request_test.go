package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
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

func TestOpenCodeGetRequestURLRoutesByUpstreamProtocol(t *testing.T) {
	base := "https://opencode.ai/zen/v1"
	cases := []struct {
		name      string
		model     string
		relayMode int
		isStream  bool
		wantURL   string
	}{
		{
			name:      "muse-spark uses responses endpoint",
			model:     "muse-spark-1.2",
			relayMode: relayconstant.RelayModeChatCompletions,
			wantURL:   base + "/responses",
		},
		{
			name:      "gpt uses responses endpoint",
			model:     "gpt-5",
			relayMode: relayconstant.RelayModeChatCompletions,
			wantURL:   base + "/responses",
		},
		{
			name:      "grok uses responses endpoint",
			model:     "grok-build-0.1",
			relayMode: relayconstant.RelayModeChatCompletions,
			wantURL:   base + "/responses",
		},
		{
			name:      "claude uses messages endpoint",
			model:     "claude-sonnet-4.5",
			relayMode: relayconstant.RelayModeChatCompletions,
			wantURL:   base + "/messages",
		},
		{
			name:      "gemini non-stream uses generateContent endpoint",
			model:     "gemini-3-pro",
			relayMode: relayconstant.RelayModeChatCompletions,
			isStream:  false,
			wantURL:   base + "/models/gemini-3-pro:generateContent",
		},
		{
			name:      "gemini stream uses streamGenerateContent endpoint",
			model:     "gemini-3-pro",
			relayMode: relayconstant.RelayModeChatCompletions,
			isStream:  true,
			wantURL:   base + "/models/gemini-3-pro:streamGenerateContent?alt=sse",
		},
		{
			name:      "mimo keeps chat completions endpoint",
			model:     "mimo-v2.5-free",
			relayMode: relayconstant.RelayModeChatCompletions,
			wantURL:   base + "/chat/completions",
		},
		{
			name:      "non chat relay mode keeps legacy chat completions endpoint",
			model:     "muse-spark-1.2",
			relayMode: relayconstant.RelayModeUnknown,
			wantURL:   base + "/chat/completions",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := openCodeRelayInfo(tc.isStream)
			info.RelayMode = tc.relayMode
			info.ChannelBaseUrl = base
			info.UpstreamModelName = tc.model
			got, err := (&Adaptor{}).GetRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, tc.wantURL, got)
		})
	}
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
