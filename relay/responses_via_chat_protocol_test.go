package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResponsesToChatStreamRoutesByProtocolTag reproduces the DeepSeek blank
// response regression: a chat-tagged channel (SupportedApiProtocols ["chat"])
// whose probe left DetectedApiType "responses" behind must convert the
// incoming /v1/responses request to chat completions, hit
// /v1/chat/completions, and stream the content back as Responses SSE events.
// Before the fix the stale detection won and the request was forwarded to
// /v1/responses, which produced a blank response.
func TestResponsesToChatStreamRoutesByProtocolTag(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_ds","object":"chat.completion.chunk","created":1710000000,"model":"deepseek-chat","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_ds","object":"chat.completion.chunk","created":1710000000,"model":"deepseek-chat","choices":[{"index":0,"delta":{"content":"hello from deepseek"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_ds","object":"chat.completion.chunk","created":1710000000,"model":"deepseek-chat","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: {"id":"chatcmpl_ds","object":"chat.completion.chunk","created":1710000000,"model":"deepseek-chat","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	type capturedRequest struct {
		path string
		body []byte
	}
	captured := make(chan capturedRequest, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		captured <- capturedRequest{path: r.URL.Path, body: body}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(upstreamBody))
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "deepseek-chat")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "test-key")
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		SupportedApiProtocols: []string{"chat"},
		DetectedApiType:       dto.ChannelApiTypeResponses,
	})

	stream := true
	request := &dto.OpenAIResponsesRequest{Model: "deepseek-chat", Input: []byte(`"hi"`), Stream: &stream}
	info := relaycommon.GenRelayInfoResponses(c, request)
	adaptor, body, closer, apiErr := PrepareResponsesRequest(c, info, request)
	require.Nil(t, apiErr)
	defer closer.Close()

	response, err := adaptor.DoRequest(c, info, body)
	require.NoError(t, err)
	httpResponse, ok := response.(*http.Response)
	require.True(t, ok)
	usage, apiErr := adaptor.DoResponse(c, httpResponse, info)
	require.Nil(t, apiErr)
	require.IsType(t, &dto.Usage{}, usage)
	require.Equal(t, 7, usage.(*dto.Usage).TotalTokens)

	upstream := <-captured
	assert.Equal(t, "/v1/chat/completions", upstream.path)
	var upstreamRequest dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal(upstream.body, &upstreamRequest))
	require.Len(t, upstreamRequest.Messages, 1)
	assert.Equal(t, "hi", upstreamRequest.Messages[0].StringContent())
	assert.Equal(t, []relaytypes.RelayFormat{relaytypes.RelayFormatOpenAIResponses, relaytypes.RelayFormatOpenAI}, info.RequestConversionChain)

	got := recorder.Body.String()
	require.Contains(t, got, "event: response.created")
	require.Contains(t, got, "event: response.output_text.delta")
	require.Contains(t, got, `"delta":"hello from deepseek"`)
	require.Contains(t, got, "event: response.completed")
	require.Contains(t, got, `"output_tokens":3`)
	require.NotContains(t, got, "response.failed")
	require.NotContains(t, got, "server_error")
}

// TestResponsesToChatStreamRoutesDeepSeekChannel covers the actual failing
// setup: a DeepSeek-native channel (type 43) tagged as chat. The DeepSeek
// adaptor used to ignore the tag, keep the request in Responses format and
// send it to /beta/responses, which returned a blank response. It must now
// convert to chat completions, hit /v1/chat/completions, and stream content
// back as Responses SSE events.
func TestResponsesToChatStreamRoutesDeepSeekChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	upstreamBody := strings.Join([]string{
		`data: {"id":"chatcmpl_ds","object":"chat.completion.chunk","created":1710000000,"model":"deepseek-flash","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_ds","object":"chat.completion.chunk","created":1710000000,"model":"deepseek-flash","choices":[{"index":0,"delta":{"content":"hello from deepseek"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl_ds","object":"chat.completion.chunk","created":1710000000,"model":"deepseek-flash","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: {"id":"chatcmpl_ds","object":"chat.completion.chunk","created":1710000000,"model":"deepseek-flash","choices":[],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	type capturedRequest struct {
		path string
		body []byte
	}
	captured := make(chan capturedRequest, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		captured <- capturedRequest{path: r.URL.Path, body: body}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(upstreamBody))
	}))
	defer server.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "deepseek-flash")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeDeepSeek)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "test-key")
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		SupportedApiProtocols: []string{"chat"},
		DetectedApiType:       dto.ChannelApiTypeChat,
	})

	stream := true
	request := &dto.OpenAIResponsesRequest{Model: "deepseek-flash", Input: []byte(`"hi"`), Stream: &stream}
	info := relaycommon.GenRelayInfoResponses(c, request)
	adaptor, body, closer, apiErr := PrepareResponsesRequest(c, info, request)
	require.Nil(t, apiErr)
	defer closer.Close()

	response, err := adaptor.DoRequest(c, info, body)
	require.NoError(t, err)
	httpResponse, ok := response.(*http.Response)
	require.True(t, ok)
	usage, apiErr := adaptor.DoResponse(c, httpResponse, info)
	require.Nil(t, apiErr)
	require.IsType(t, &dto.Usage{}, usage)
	require.Equal(t, 7, usage.(*dto.Usage).TotalTokens)

	upstream := <-captured
	assert.Equal(t, "/v1/chat/completions", upstream.path)
	var upstreamRequest dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal(upstream.body, &upstreamRequest))
	require.Len(t, upstreamRequest.Messages, 1)
	assert.Equal(t, "hi", upstreamRequest.Messages[0].StringContent())
	assert.Equal(t, []relaytypes.RelayFormat{relaytypes.RelayFormatOpenAIResponses, relaytypes.RelayFormatOpenAI}, info.RequestConversionChain)

	got := recorder.Body.String()
	require.Contains(t, got, "event: response.created")
	require.Contains(t, got, "event: response.output_text.delta")
	require.Contains(t, got, `"delta":"hello from deepseek"`)
	require.Contains(t, got, "event: response.completed")
	require.NotContains(t, got, "response.failed")
}
