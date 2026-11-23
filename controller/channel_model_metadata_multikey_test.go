package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The body is the shape Token Harbor's /v1/models returns.
const tokenHarborModelsBody = `{"object":"list","data":[
{"id":"claude-sonnet-4.6","object":"model","owned_by":"tokenharbor","context_length":null,"tool_call":true,"function_call":true,"reasoning":{"efforts":null,"default_effort":null,"can_disable":null}},
{"id":"claude-opus-5.5","object":"model","owned_by":"tokenharbor","context_length":1000000,"tool_call":true,"function_call":true,"reasoning":{"efforts":["low","medium","high","xhigh"],"default_effort":"medium","can_disable":false}},
{"id":"deepseek-v4.1-flash:free","object":"model","owned_by":"tokenharbor","context_length":1048576,"tool_call":true,"function_call":true,"reasoning":{"efforts":null,"default_effort":null,"can_disable":null}},
{"id":"deepseek-v4.1-flash-free","object":"model","owned_by":"opencode","context_window":200000,"max_output":32000,"reasoning":true,"tool_call":true}
]}`

func TestFetchChannelUpstreamModelMetadataAsksMultiKeyChannels(t *testing.T) {
	var authorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, tokenHarborModelsBody)
	}))
	t.Cleanup(upstream.Close)

	baseURL := upstream.URL
	channel := &model.Channel{
		Id:      7,
		Type:    constant.ChannelTypeOpenAI,
		Name:    "multi key",
		Key:     "key-1\nkey-2",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       2,
			MultiKeyMode:       constant.MultiKeyModeRandom,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
		},
	}

	metadata, err := fetchChannelUpstreamModelMetadata(channel)
	require.NoError(t, err)
	assert.Equal(t, "Bearer key-2", authorization, "the request uses an enabled key")

	free := metadata["deepseek-v4.1-flash:free"]
	assert.Equal(t, 1048576, free.ContextLength)
	require.NotNil(t, free.ToolCall)
	assert.True(t, *free.ToolCall)
	assert.Nil(t, free.Reasoning, "efforts null means unknown")

	opus := metadata["claude-opus-5.5"]
	assert.Equal(t, 1000000, opus.ContextLength)
	require.NotNil(t, opus.Reasoning)
	assert.True(t, *opus.Reasoning)

	// opencode2api names the output limit max_output.
	relay := metadata["deepseek-v4.1-flash-free"]
	assert.Equal(t, 200000, relay.ContextLength)
	assert.Equal(t, 32000, relay.MaxOutputTokens)

	sonnet := metadata["claude-sonnet-4.6"]
	assert.Zero(t, sonnet.ContextLength)
	require.NotNil(t, sonnet.ToolCall)
	assert.True(t, *sonnet.ToolCall)
}
