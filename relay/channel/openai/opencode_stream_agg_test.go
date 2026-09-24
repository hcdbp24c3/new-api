package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newOpenCodeAggTestContext(t *testing.T, sse string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "oc-agg-test")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenCode,
			UpstreamModelName: "mimo-v2.5-free",
		},
		IsStream:    false,
		RelayFormat: types.RelayFormatOpenAI,
	}
	return c, rec, resp, info
}

func TestOpenCodeSSEToNonStreamHandlerAggregates(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"mimo-v2.5-free","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"mimo-v2.5-free","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"mimo-v2.5-free","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	c, rec, resp, info := newOpenCodeAggTestContext(t, sse)

	usage, apiErr := OpenCodeSSEToNonStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 3, usage.PromptTokens)
	assert.Equal(t, 2, usage.CompletionTokens)

	var out dto.OpenAITextResponse
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &out))
	require.Len(t, out.Choices, 1)
	assert.Equal(t, "Hello", out.Choices[0].Message.StringContent())
	assert.Equal(t, "stop", out.Choices[0].FinishReason)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.NotContains(t, rec.Body.String(), "data:")
}

func TestOpenCodeSSEToNonStreamHandlerDefaultsFinishReasonStop(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"chatcmpl-2","object":"chat.completion.chunk","created":2,"model":"mimo-v2.5-free","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":null}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	c, rec, resp, info := newOpenCodeAggTestContext(t, sse)

	usage, apiErr := OpenCodeSSEToNonStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)

	var out dto.OpenAITextResponse
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &out))
	require.Len(t, out.Choices, 1)
	assert.Equal(t, "Hi", out.Choices[0].Message.StringContent())
	assert.Equal(t, "stop", out.Choices[0].FinishReason)
}

func TestOpenCodeSSEToNonStreamHandlerEmptyStream(t *testing.T) {
	for name, sse := range map[string]string{
		"empty body":  "",
		"only done":   "data: [DONE]\n",
		"blank lines": "\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			c, rec, resp, info := newOpenCodeAggTestContext(t, sse)

			usage, apiErr := OpenCodeSSEToNonStreamHandler(c, info, resp)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)

			var out dto.OpenAITextResponse
			require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &out))
			require.Len(t, out.Choices, 1)
			assert.Equal(t, "", out.Choices[0].Message.StringContent())
			assert.Equal(t, "stop", out.Choices[0].FinishReason)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		})
	}
}

func TestOpenCodeSSEToNonStreamHandlerAggregatesToolCalls(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"chatcmpl-3","object":"chat.completion.chunk","created":3,"model":"mimo-v2.5-free","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"shell","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-3","object":"chat.completion.chunk","created":3,"model":"mimo-v2.5-free","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"command\":\"ls\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-3","object":"chat.completion.chunk","created":3,"model":"mimo-v2.5-free","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	c, rec, resp, info := newOpenCodeAggTestContext(t, sse)

	usage, apiErr := OpenCodeSSEToNonStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)

	var out dto.OpenAITextResponse
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &out))
	require.Len(t, out.Choices, 1)
	assert.Equal(t, "tool_calls", out.Choices[0].FinishReason)

	toolCalls := out.Choices[0].Message.ParseToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "shell", toolCalls[0].Function.Name)
	assert.Equal(t, `{"command":"ls"}`, toolCalls[0].Function.Arguments)
	assert.Equal(t, "call_1", toolCalls[0].ID)
}

func TestDoResponseOpenCodeForceStreamAggregationWinsOverSSEContentType(t *testing.T) {
	// compatible_handler may set IsStream=true from SSE Content-Type before
	// DoResponse; ForceOpenCodeStreamAgg (client wanted non-stream) must win.
	sse := strings.Join([]string{
		`data: {"id":"chatcmpl-4","object":"chat.completion.chunk","created":4,"model":"mimo-v2.5-free","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	c, rec, resp, info := newOpenCodeAggTestContext(t, sse)
	info.IsStream = true
	info.ForceOpenCodeStreamAgg = true
	info.RelayMode = relayconstant.RelayModeChatCompletions

	usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.False(t, info.IsStream, "aggregation branch must clear IsStream so non-stream clients get JSON")

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.NotContains(t, rec.Header().Get("Content-Type"), "text/event-stream")
	assert.NotContains(t, rec.Body.String(), "data:")

	var out dto.OpenAITextResponse
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &out))
	require.Len(t, out.Choices, 1)
	assert.Equal(t, "ok", out.Choices[0].Message.StringContent())
}
