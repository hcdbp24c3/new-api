package service

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// ShouldChatCompletionsUseResponsesGlobal determines if chat requests should be forwarded
// to the upstream Responses API. With policy removed, conversion between Chat and Responses
// is always enabled when the channel supports or targets Responses.
func ShouldChatCompletionsUseResponsesGlobal(channelID int, channelType int, model string) bool {
	// Channels that specifically run on Responses or have been auto-detected as Responses
	return false
}

// ChannelSupportsProtocol checks if the channel explicitly supports a given protocol tag.
func ChannelSupportsProtocol(settings dto.ChannelOtherSettings, protocol string) bool {
	for _, p := range settings.SupportedApiProtocols {
		if p == protocol {
			return true
		}
	}
	return false
}

// ChannelTargetsResponses checks if the channel's configured, detected, or tagged api type is responses.
func ChannelTargetsResponses(settings dto.ChannelOtherSettings) bool {
	// Protocol tags are the authoritative record of what the upstream actually
	// supports. When any tag is present, auto-detection must not override it:
	// a chat-tagged channel (DeepSeek, most OpenAI-compatible providers) must
	// never be routed through the Responses protocol just because an earlier
	// probe persisted DetectedApiType "responses".
	if len(settings.SupportedApiProtocols) > 0 {
		return ChannelSupportsProtocol(settings, "responses")
	}
	if settings.UpstreamApiType == dto.ChannelApiTypeResponses {
		return true
	}
	if settings.UpstreamApiType == dto.ChannelApiTypeChat {
		return false
	}
	if settings.DetectedApiType == dto.ChannelApiTypeResponses {
		return true
	}
	return false
}

// ChannelTargetsChat checks if the channel's configured, detected, or tagged api type is chat.
func ChannelTargetsChat(settings dto.ChannelOtherSettings) bool {
	// The explicit protocol tag wins over auto-detection: a chat-tagged
	// channel (DeepSeek, most OpenAI-compatible providers) converts incoming
	// Responses requests to chat completions even when DetectedApiType was
	// persisted as "responses" by an earlier probe.
	if len(settings.SupportedApiProtocols) > 0 {
		return ChannelSupportsProtocol(settings, "chat")
	}
	if settings.UpstreamApiType == dto.ChannelApiTypeChat {
		return true
	}
	if settings.UpstreamApiType == dto.ChannelApiTypeResponses {
		return false
	}
	if settings.DetectedApiType == dto.ChannelApiTypeChat {
		return true
	}
	if settings.DetectedApiType == dto.ChannelApiTypeResponses {
		return false
	}
	// Default to chat if no other tags or settings are provided
	return true
}

// RelayFormatProtocol returns the SupportedApiProtocols tag that matches an
// incoming relay format, or "" when the format has no native protocol tag.
// Multi-protocol channels (e.g. dc-ai gateways that natively serve chat,
// responses, Anthropic and Gemini) use these tags to pass requests through in
// the client's own protocol instead of converting them.
func RelayFormatProtocol(format types.RelayFormat) string {
	switch format {
	case types.RelayFormatOpenAI:
		return "chat"
	case types.RelayFormatOpenAIResponses:
		return "responses"
	case types.RelayFormatClaude:
		return "anthropic"
	case types.RelayFormatGemini:
		return "gemini"
	default:
		return ""
	}
}

// ChannelNativelySupports reports whether the channel's protocol tags include
// the protocol of the given incoming relay format.
func ChannelNativelySupports(settings dto.ChannelOtherSettings, format types.RelayFormat) bool {
	protocol := RelayFormatProtocol(format)
	if protocol == "" {
		return false
	}
	return ChannelSupportsProtocol(settings, protocol)
}

// NativeProtocolPath returns the upstream path suffix for the incoming relay
// format, used by explicit-endpoint channel probes that must hit the native
// protocol endpoint instead of a converted one.
func NativeProtocolPath(info *relaycommon.RelayInfo) (string, bool) {
	if info == nil {
		return "", false
	}
	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		return "/v1/chat/completions", true
	case types.RelayFormatOpenAIResponses:
		return "/v1/responses", true
	case types.RelayFormatClaude:
		return "/v1/messages", true
	case types.RelayFormatGemini:
		action := "generateContent"
		if info.IsStream {
			action = "streamGenerateContent?alt=sse"
		}
		if info.UpstreamModelName == "" {
			return "", false
		}
		return "/v1beta/models/" + info.UpstreamModelName + ":" + action, true
	default:
		return "", false
	}
}
