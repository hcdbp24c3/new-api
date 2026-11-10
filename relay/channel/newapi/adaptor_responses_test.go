package newapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A chat-target New API channel must convert Responses requests to chat
// completions and route them to /v1/chat/completions instead of forwarding
// them unchanged to /v1/responses (the DeepSeek blank-response bug class).
func TestNewAPIResponsesToChatForChatTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-4o",
		RelayFormat:     types.RelayFormatOpenAIResponses,
		RelayMode:       relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://upstream.example.com",
			UpstreamModelName: "gpt-4o",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				SupportedApiProtocols: []string{"chat"},
			},
		},
	}
	info.InitRequestConversionChain()

	adaptor := Adaptor{}
	converted, err := adaptor.ConvertOpenAIResponsesRequest(c, info, dto.OpenAIResponsesRequest{
		Model: "gpt-4o",
		Input: []byte(`"hi"`),
	})
	require.NoError(t, err)
	chatReq, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok, "expected *dto.GeneralOpenAIRequest, got %T", converted)
	require.Len(t, chatReq.Messages, 1)
	assert.Equal(t, "hi", chatReq.Messages[0].StringContent())
	assert.Equal(t, []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatOpenAI}, info.RequestConversionChain)

	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://upstream.example.com/v1/chat/completions", url)
}

// A responses-target New API channel keeps the native /v1/responses passthrough.
func TestNewAPIResponsesPassthroughForResponsesTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-4o",
		RelayFormat:     types.RelayFormatOpenAIResponses,
		RelayMode:       relayconstant.RelayModeResponses,
		RequestURLPath:  "/v1/responses",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://upstream.example.com",
			UpstreamModelName: "gpt-4o",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				SupportedApiProtocols: []string{"responses"},
			},
		},
	}

	adaptor := Adaptor{}
	converted, err := adaptor.ConvertOpenAIResponsesRequest(c, info, dto.OpenAIResponsesRequest{
		Model: "gpt-4o",
		Input: []byte(`"hi"`),
	})
	require.NoError(t, err)
	_, ok := converted.(dto.OpenAIResponsesRequest)
	require.True(t, ok, "expected dto.OpenAIResponsesRequest, got %T", converted)

	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://upstream.example.com/v1/responses", url)
}
