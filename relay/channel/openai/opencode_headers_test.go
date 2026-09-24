package openai

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	openCodeSessionIDRe = regexp.MustCompile(`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`)
	openCodeRequestIDRe = regexp.MustCompile(`^msg_[0-9A-Za-z]{16,}$`)
)

func TestOpenCodeSessionIDFormat(t *testing.T) {
	for range 20 {
		id := newOpenCodeSessionID()
		require.True(t, openCodeSessionIDRe.MatchString(id), "session id %q must match ses_ format", id)
	}
}

func TestOpenCodeRequestIDFormat(t *testing.T) {
	for range 20 {
		id := newOpenCodeRequestID()
		require.True(t, openCodeRequestIDRe.MatchString(id), "request id %q must match msg_ format", id)
	}
}

func TestApplyOpenCodeFreeTierHeaders(t *testing.T) {
	h := http.Header{}
	applyOpenCodeFreeTierHeaders(&h)
	assert.Equal(t, "cli", h.Get("x-opencode-client"))
	assert.Equal(t, "global", h.Get("x-opencode-project"))
	assert.Regexp(t, `^opencode/1\.(1[7-9]|[2-9]\d)\.`, h.Get("User-Agent"))
	assert.Regexp(t, `^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`, h.Get("x-opencode-session"))
	assert.Regexp(t, `^msg_`, h.Get("x-opencode-request"))
	assert.NotEqual(t, h.Get("x-opencode-session"), h.Get("x-opencode-request"))
}

func TestApplyOpenCodeFreeTierHeadersRespectsOverrides(t *testing.T) {
	h := http.Header{}
	h.Set("x-opencode-session", "ses_custom000000abcdefghijkl")
	h.Set("User-Agent", "opencode/1.18.31")
	applyOpenCodeFreeTierHeaders(&h)
	assert.Equal(t, "ses_custom000000abcdefghijkl", h.Get("x-opencode-session"))
	assert.Equal(t, "opencode/1.18.31", h.Get("User-Agent"))
}

func TestSetupRequestHeaderOpenCodeEmptyKeyUsesBearerPublic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenCode,
			ApiKey:      "",
		},
	}
	adaptor := &Adaptor{}
	h := http.Header{}
	require.NoError(t, adaptor.SetupRequestHeader(c, &h, info))
	assert.Equal(t, "Bearer public", h.Get("Authorization"))
	assert.Equal(t, "cli", h.Get("x-opencode-client"))
	assert.Equal(t, "global", h.Get("x-opencode-project"))
	assert.Regexp(t, `^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`, h.Get("x-opencode-session"))
	assert.Regexp(t, `^msg_`, h.Get("x-opencode-request"))
	assert.Regexp(t, `^opencode/1\.(1[7-9]|[2-9]\d)\.`, h.Get("User-Agent"))
}

func TestSetupRequestHeaderOpenCodeNonEmptyKeyPreserved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenCode,
			ApiKey:      "sk-test",
		},
	}
	adaptor := &Adaptor{}
	h := http.Header{}
	require.NoError(t, adaptor.SetupRequestHeader(c, &h, info))
	assert.Equal(t, "Bearer sk-test", h.Get("Authorization"))
}

func TestSetupRequestHeaderOpenCodeDoesNotForcePublicWhenAuthOverridePresent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:     constant.ChannelTypeOpenCode,
			ApiKey:          "",
			HeadersOverride: map[string]any{"Authorization": "Bearer overridden"},
		},
	}
	adaptor := &Adaptor{}
	h := http.Header{}
	require.NoError(t, adaptor.SetupRequestHeader(c, &h, info))
	// hasAuthOverride skips default Bearer; OpenCode branch must not inject
	// Bearer public when Authorization override is configured (applied later).
	assert.Empty(t, h.Get("Authorization"))
}
