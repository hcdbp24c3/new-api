package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Non-stream counterpart of newStreamGuardFixture: same gin/RelayInfo shape,
// but IsStream=false and a readable body, because OpenaiHandler reads the whole
// response up front instead of scanning SSE events.
func newNonStreamHandlerFixture(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo, *http.Response) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "MiniMax-M3"},
		RelayFormat: types.RelayFormatOpenAI,
		RelayMode:   relayconstant.RelayModeChatCompletions,
		IsStream:    false,
	}
	return c, recorder, info, resp
}

// MiniMax reports non-stream failures as HTTP 200 + a base_resp envelope with no
// usable choices, which used to reach the client as 200 + empty body. It must
// become a retryable 502 carrying the upstream status_code/status_msg.
func TestOpenaiHandlerBaseRespErrorReturnsRetryable502(t *testing.T) {
	body := `{"id":"x","choices":null,"base_resp":{"status_code":1008,"status_msg":"insufficient balance"}}`
	c, _, info, resp := newNonStreamHandlerFixture(t, body)
	usage, err := OpenaiHandler(c, info, resp)
	require.NotNil(t, err, "base_resp failure must fail over instead of returning 200 + empty body")
	require.Nil(t, usage)
	assert.Equal(t, http.StatusBadGateway, err.StatusCode)
	assert.Equal(t, types.ErrorCodeBadResponse, err.GetErrorCode())
	assert.Contains(t, err.Error(), "code=1008")
	assert.Contains(t, err.Error(), "insufficient balance")
}

// status_code 0 is the success envelope: the response must pass through with
// the content intact and no error.
func TestOpenaiHandlerBaseRespSuccessPassthrough(t *testing.T) {
	body := `{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],` +
		`"base_resp":{"status_code":0,"status_msg":""},` +
		`"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`
	c, recorder, info, resp := newNonStreamHandlerFixture(t, body)
	usage, err := OpenaiHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	assert.Contains(t, recorder.Body.String(), "hello")
}

// Baseline captured against unmodified code: a top-level error object keeps its
// existing shape (upstream status code, upstream message, upstream error code)
// so the base_resp check must not shadow or rewrite it.
func TestOpenaiHandlerNormalErrorFieldUnchanged(t *testing.T) {
	body := `{"error":{"message":"upstream says no","type":"invalid_request_error","code":"bad_req"}}`
	c, _, info, resp := newNonStreamHandlerFixture(t, body)
	usage, err := OpenaiHandler(c, info, resp)
	require.NotNil(t, err)
	require.Nil(t, usage)
	assert.Equal(t, http.StatusOK, err.StatusCode)
	assert.Equal(t, "upstream says no", err.ToOpenAIError().Message)
	assert.Equal(t, "bad_req", string(err.GetErrorCode()))
}
