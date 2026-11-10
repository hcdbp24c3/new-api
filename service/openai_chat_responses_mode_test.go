package service

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
)

// TestChannelTargetsChatPrefersProtocolTagOverStaleDetection covers the
// DeepSeek blank-response regression: a chat-only channel whose probe persisted
// DetectedApiType "responses" must still route incoming Responses requests to
// chat completions (and never take the Responses relay path) once the channel
// carries the "chat" protocol tag.
func TestChannelTargetsChatPrefersProtocolTagOverStaleDetection(t *testing.T) {
	tests := []struct {
		name          string
		settings      dto.ChannelOtherSettings
		wantChat      bool
		wantResponses bool
	}{
		{
			// The exact regression: stale auto-detection says responses, the
			// protocol tag says chat. Chat must win for both decisions so the
			// request is converted to chat instead of being forwarded to
			// /v1/responses (which produced the blank response).
			name: "chat tag wins over stale responses detection",
			settings: dto.ChannelOtherSettings{
				SupportedApiProtocols: []string{"chat"},
				DetectedApiType:       dto.ChannelApiTypeResponses,
			},
			wantChat:      true,
			wantResponses: false,
		},
		{
			name: "responses tag wins over stale chat detection",
			settings: dto.ChannelOtherSettings{
				SupportedApiProtocols: []string{"responses"},
				DetectedApiType:       dto.ChannelApiTypeChat,
			},
			wantChat:      false,
			wantResponses: true,
		},
		{
			name: "explicit upstream chat is respected without tags",
			settings: dto.ChannelOtherSettings{
				UpstreamApiType: dto.ChannelApiTypeChat,
			},
			wantChat:      true,
			wantResponses: false,
		},
		{
			name: "explicit upstream responses is respected without tags",
			settings: dto.ChannelOtherSettings{
				UpstreamApiType: dto.ChannelApiTypeResponses,
			},
			wantChat:      false,
			wantResponses: true,
		},
		{
			name: "detected chat routes to chat",
			settings: dto.ChannelOtherSettings{
				DetectedApiType: dto.ChannelApiTypeChat,
			},
			wantChat:      true,
			wantResponses: false,
		},
		{
			name: "detected responses routes to responses",
			settings: dto.ChannelOtherSettings{
				DetectedApiType: dto.ChannelApiTypeResponses,
			},
			wantChat:      false,
			wantResponses: true,
		},
		{
			name:     "no tags or settings defaults to chat",
			settings: dto.ChannelOtherSettings{},
			wantChat: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantChat, ChannelTargetsChat(tt.settings), "ChannelTargetsChat")
			assert.Equal(t, tt.wantResponses, ChannelTargetsResponses(tt.settings), "ChannelTargetsResponses")
		})
	}
}

func TestChannelSupportsProtocol(t *testing.T) {
	assert.True(t, ChannelSupportsProtocol(dto.ChannelOtherSettings{SupportedApiProtocols: []string{"chat", "responses"}}, "chat"))
	assert.False(t, ChannelSupportsProtocol(dto.ChannelOtherSettings{SupportedApiProtocols: []string{"responses"}}, "chat"))
	assert.False(t, ChannelSupportsProtocol(dto.ChannelOtherSettings{}, "chat"))
}
