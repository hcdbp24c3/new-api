package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
	if constant.StreamingTimeout == 0 {
		constant.StreamingTimeout = 30
	}
}

// No t.Parallel(): concurrent tests race the intentional lock-free logCount++
// in logger/logger.go:115 and fail the -race gate (reviewer finding 2).
// OaiStreamHandler returns a typed *types.NewAPIError, so assertions must use
// require.NotNil/require.Nil — require.Error/require.NoError misread typed nil.
func newStreamGuardFixture(t *testing.T, body string, canceled bool) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo, *http.Response) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx := context.Background()
	if canceled {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(context.Background())
		cancel()
	}
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	resp := &http.Response{
		Body:       io.NopCloser(strings.NewReader(body)),
		StatusCode: http.StatusOK,
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "MiniMax-M3"},
		RelayFormat: types.RelayFormatOpenAI,
		RelayMode:   relayconstant.RelayModeChatCompletions,
		IsStream:    true,
		DisablePing: true,
	}
	return c, recorder, info, resp
}

func TestOaiStreamHandlerEmptyStreamReturnsRetryable502(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "single unwritten role chunk", body: `data: {"id":"x","choices":[{"delta":{"role":"assistant"}}]}` + "\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder, info, resp := newStreamGuardFixture(t, tc.body, false)
			usage, err := OaiStreamHandler(c, info, resp)
			require.NotNil(t, err, "empty stream must fail over instead of returning 200 + empty SSE")
			require.Nil(t, usage)
			assert.Equal(t, http.StatusBadGateway, err.StatusCode)
			assert.Equal(t, types.ErrorCodeBadResponse, err.GetErrorCode())
			assert.Zero(t, recorder.Body.Len(), "guard must fire before any byte is written")
		})
	}
}

func TestOaiStreamHandlerCompletedStreamNotGuarded(t *testing.T) {
	body := `data: {"id":"x","choices":[{"delta":{"role":"assistant"}}]}` + "\n" +
		`data: {"id":"x","choices":[{"delta":{"content":"hi"}}],"finish_reason":"stop"}` + "\n" +
		"data: [DONE]\n"
	c, recorder, info, resp := newStreamGuardFixture(t, body, false)
	usage, err := OaiStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	assert.Contains(t, recorder.Body.String(), "hi")
}

func TestOaiStreamHandlerCanceledClientNotGuarded(t *testing.T) {
	body := `data: {"id":"x","choices":[{"delta":{"role":"assistant"}}]}` + "\n\n"
	c, _, info, resp := newStreamGuardFixture(t, body, true)
	usage, err := OaiStreamHandler(c, info, resp)
	require.Nil(t, err, "client_gone/none end reasons must not trigger failover")
	require.NotNil(t, usage)
}
