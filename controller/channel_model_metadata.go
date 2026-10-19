package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/gemini"

	"github.com/samber/lo"
)

// upstreamModelMetadata is what a provider's own API reported for one model.
// Every field is optional: a zero value means the provider did not supply it,
// so the metadata sync falls back to the models.dev catalog for that field
// instead of writing a zero over a known-good value.
type upstreamModelMetadata struct {
	Description     string
	ContextLength   int
	MaxOutputTokens int
	ToolCall        *bool
}

// fetchChannelUpstreamModelMetadata reads per-model metadata from the
// provider's own API. Providers whose model list carries nothing beyond an ID
// return a nil map, which callers treat as "no upstream metadata available"
// and fall back to models.dev entirely.
//
// A failing provider returns its error so the sync degrades to models.dev for
// that channel's models instead of failing wholesale.
func fetchChannelUpstreamModelMetadata(channel *model.Channel) (map[string]upstreamModelMetadata, error) {
	if channel == nil || channel.Type == constant.ChannelTypeTaskPlugin {
		// Task plugin channels expose a static model list with no metadata.
		return nil, nil
	}
	if channel.ChannelInfo.IsMultiKey {
		// Model discovery already refuses multi-key channels, and probing each
		// key would multiply requests without adding data.
		return nil, nil
	}

	baseURL := constant.GetChannelBaseURL(channel.Type)
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	if channel.Type == constant.ChannelTypeGemini {
		return fetchGeminiChannelModelMetadata(channel, baseURL)
	}

	url, key, headers, err := resolveFetchModelsRequest(channel, baseURL)
	if err != nil {
		return nil, err
	}
	body, err := getFetchModelsResponseBody(http.MethodGet, url, channel, headers)
	if err != nil {
		return nil, sanitizeAdvancedCustomRequestError(err, key, url)
	}

	var result OpenAIModelsResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return openAIChannelModelMetadata(result.Data), nil
}

// openAIChannelModelMetadata reads the capability metadata that OpenAI-
// compatible providers sometimes attach to a model entry. Most return only
// id/created/owned_by, so those entries contribute nothing and the model keeps
// resolving through models.dev.
func openAIChannelModelMetadata(items []OpenAIModel) map[string]upstreamModelMetadata {
	metadata := make(map[string]upstreamModelMetadata, len(items))
	for _, item := range items {
		if item.ID == "" {
			continue
		}
		entry := upstreamModelMetadata{
			ContextLength:   positiveMetadataInt(item.Metadata, "context_length", "contextLength", "max_context_length", "max_input_tokens"),
			MaxOutputTokens: positiveMetadataInt(item.Metadata, "max_output_tokens", "maxOutputTokens", "max_tokens"),
		}
		if description, ok := metadataString(item.Metadata, "description"); ok {
			entry.Description = description
		}
		if toolCall, ok := metadataBool(item.Metadata, "tool_call", "toolCall", "supports_tools", "supportsTools"); ok {
			entry.ToolCall = lo.ToPtr(toolCall)
		}
		if entry == (upstreamModelMetadata{}) {
			continue
		}
		metadata[item.ID] = entry
	}
	return metadata
}

// fetchGeminiChannelModelMetadata reads Gemini's model list, which reports
// token limits and a description next to each model name.
func fetchGeminiChannelModelMetadata(channel *model.Channel, baseURL string) (map[string]upstreamModelMetadata, error) {
	key, _, apiErr := channel.GetNextEnabledKey()
	if apiErr != nil {
		return nil, fmt.Errorf("failed to get channel key: %w", apiErr)
	}
	models, err := gemini.FetchGeminiModelMetadata(baseURL, strings.TrimSpace(key), channel.GetSetting())
	if err != nil {
		return nil, err
	}

	metadata := make(map[string]upstreamModelMetadata, len(models))
	for _, item := range models {
		name, ok := item.Name.(string)
		if !ok {
			continue
		}
		name = strings.TrimPrefix(name, "models/")
		if name == "" {
			continue
		}
		entry := upstreamModelMetadata{
			ContextLength:   nonNegativeModelInt(item.InputTokenLimit),
			MaxOutputTokens: nonNegativeModelInt(item.OutputTokenLimit),
		}
		if description, ok := item.Description.(string); ok {
			entry.Description = strings.TrimSpace(description)
		}
		if entry == (upstreamModelMetadata{}) {
			continue
		}
		metadata[name] = entry
	}
	return metadata, nil
}

// nonNegativeModelInt reads a numeric field a provider reported for a model.
// Gemini encodes these as JSON numbers or strings depending on the endpoint.
func nonNegativeModelInt(value any) int {
	var number int
	switch typed := value.(type) {
	case float64:
		number = int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0
		}
		number = parsed
	default:
		return 0
	}
	if number < 0 {
		return 0
	}
	return number
}

// positiveMetadataInt returns the first usable positive integer among keys, or
// 0 when the provider reported none.
func positiveMetadataInt(metadata map[string]any, keys ...string) int {
	for _, key := range keys {
		if number := nonNegativeModelInt(metadata[key]); number > 0 {
			return number
		}
	}
	return 0
}

func metadataString(metadata map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if text, ok := metadata[key].(string); ok {
			if trimmed := strings.TrimSpace(text); trimmed != "" {
				return trimmed, true
			}
		}
	}
	return "", false
}

func metadataBool(metadata map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		if flag, ok := metadata[key].(bool); ok {
			return flag, true
		}
	}
	return false, false
}
