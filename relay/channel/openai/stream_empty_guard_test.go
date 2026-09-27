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
// in logger/logger.go:115 and fail the -race gate.
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
	resp := &http.Response{StatusCode: http.StatusOK}
	if canceled {
		// A blocking pipe instead of a readable body: the scanner goroutine must
		// not be able to win the race and settle EndReason=eof before the main
		// select observes ctx.Done(). Scan blocks until StreamScannerHandler's
		// cleanup closes resp.Body (ErrClosedPipe), which happens only after
		// client_gone was set, so the guard is deterministically skipped.
		// Cleanup order: handler closes the reader first, then this closes the
		// writer; double-closing a pipe end is safe.
		pipeReader, pipeWriter := io.Pipe()
		resp.Body = pipeReader
		t.Cleanup(func() { _ = pipeWriter.Close() })
	} else {
		resp.Body = io.NopCloser(strings.NewReader(body))
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
			assert.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"),
				"guard must rewrite the SSE Content-Type so the 502 header does not lie")
		})
	}
}

// The outcome==completed shield keeps a finish_reason-only stream (no content)
// from being converted into a 502 by the empty-stream guard. A single data
// event only: with a second event the deferred-write flush would set
// c.Writer.Written() and mask the shield.
func TestOaiStreamHandlerFinishOnlyStreamNotGuarded(t *testing.T) {
	body := `data: {"id":"x","choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n" +
		"data: [DONE]\n"
	c, _, info, resp := newStreamGuardFixture(t, body, false)
	usage, err := OaiStreamHandler(c, info, resp)
	require.Nil(t, err, "completed outcome must shield the guard even with empty content")
	require.NotNil(t, usage)
}

// Regression for the guard's usable-stream predicate: RelayMode values that
// never populate responseTextBuilder/toolCount (processTokenData only handles
// chat/completions) must not have a legitimately delivered content event
// converted into a 502. RelayModeUnknown is the legacy path shape.
func TestOaiStreamHandlerLegacyModeContentEventNotGuarded(t *testing.T) {
	body := `data: {"id":"x","choices":[{"delta":{"content":"hello"}}]}` + "\n\n"
	c, recorder, info, resp := newStreamGuardFixture(t, body, false)
	info.RelayMode = relayconstant.RelayModeUnknown
	usage, err := OaiStreamHandler(c, info, resp)
	require.Nil(t, err, "a content event must stay usable regardless of RelayMode")
	require.NotNil(t, usage)
	assert.Contains(t, recorder.Body.String(), "hello")
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
