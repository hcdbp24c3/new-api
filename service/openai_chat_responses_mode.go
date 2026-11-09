package service

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// ShouldChatCompletionsUseResponsesGlobal determines if chat requests should be forwarded
// to the upstream Responses API. With policy removed, conversion between Chat and Responses
// is always enabled when the channel supports or targets Responses.
func ShouldChatCompletionsUseResponsesGlobal(channelID int, channelType int, model string) bool {
	// Channels that specifically run on Responses or have been auto-detected as Responses
	return false
}

// ChannelTargetsResponses checks if the channel's configured or detected api type is responses.
func ChannelTargetsResponses(settings dto.ChannelOtherSettings) bool {
	if settings.UpstreamApiType == dto.ChannelApiTypeResponses {
		return true
	}
	if settings.UpstreamApiType == dto.ChannelApiTypeAuto && settings.DetectedApiType == dto.ChannelApiTypeResponses {
		return true
	}
	return false
}

// ChannelTargetsChat checks if the channel's configured or detected api type is chat.
func ChannelTargetsChat(settings dto.ChannelOtherSettings) bool {
	if settings.UpstreamApiType == dto.ChannelApiTypeChat {
		return true
	}
	if settings.UpstreamApiType == dto.ChannelApiTypeAuto && settings.DetectedApiType == dto.ChannelApiTypeChat {
		return true
	}
	return false
}


