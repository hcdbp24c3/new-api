package controller

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/gemini"

	"github.com/samber/lo"
)

// upstreamModelMetadata is what a provider's own API reported for one model.
// Every field is optional: a zero value or nil pointer means the provider did
// not supply it, so the metadata sync falls back to the models.dev catalog for
// that field instead of writing a zero over a known-good value.
type upstreamModelMetadata struct {
	Description     string
	ContextLength   int
	MaxOutputTokens int
	ToolCall        *bool
	Reasoning       *bool
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

	var rawResult struct {
		Data    []map[string]any `json:"data"`
		Success bool             `json:"success"`
	}
	if err := common.Unmarshal(body, &rawResult); err != nil {
		return nil, err
	}
	return openAIRawChannelModelMetadata(rawResult.Data), nil
}

func openAIChannelModelMetadata(items []OpenAIModel) map[string]upstreamModelMetadata {
	rawItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		m := map[string]any{
			"id":       item.ID,
			"metadata": item.Metadata,
		}
		rawItems = append(rawItems, m)
	}
	return openAIRawChannelModelMetadata(rawItems)
}

// openAIRawChannelModelMetadata reads the metadata shapes OpenAI-compatible
// gateways attach to their model list: flat fields (new-api, Token Harbor),
// OpenRouter's top_provider and supported_parameters, LiteLLM's model_info,
// a models.dev-style limit object and capability maps or lists.
func openAIRawChannelModelMetadata(items []map[string]any) map[string]upstreamModelMetadata {
	metadata := make(map[string]upstreamModelMetadata, len(items))
	for _, item := range items {
		id, _ := item["id"].(string)
		if id == "" {
			continue
		}
		nested, _ := item["metadata"].(map[string]any)
		modelInfo, _ := item["model_info"].(map[string]any)
		topProvider, _ := item["top_provider"].(map[string]any)
		limit, _ := item["limit"].(map[string]any)

		entry := upstreamModelMetadata{}
		for _, source := range []map[string]any{item, nested, modelInfo} {
			if entry.ContextLength == 0 {
				entry.ContextLength = positiveMetadataInt(source, "context_length", "contextLength", "context_window", "contextWindow", "max_context_length", "max_input_tokens", "max_model_len", "input_token_limit")
			}
			if entry.MaxOutputTokens == 0 {
				entry.MaxOutputTokens = positiveMetadataInt(source, "max_output_tokens", "maxOutputTokens", "max_completion_tokens", "output_token_limit", "max_tokens")
			}
			if entry.Description == "" {
				entry.Description, _ = metadataString(source, "description")
			}
			if entry.ToolCall == nil {
				entry.ToolCall = upstreamCapabilityFlag(source,
					[]string{"tool_call", "toolCall", "supports_tools", "supportsTools", "supports_function_calling", "function_calling"},
					[]string{"tools", "tool_call", "tool_use", "function_calling"},
					[]string{"tools", "tool_choice"})
			}
			if entry.Reasoning == nil {
				entry.Reasoning = upstreamCapabilityFlag(source,
					[]string{"reasoning", "supports_reasoning", "supports_thinking"},
					[]string{"reasoning", "thinking"},
					[]string{"reasoning", "reasoning_effort", "include_reasoning"})
			}
		}
		if entry.ContextLength == 0 {
			entry.ContextLength = max(positiveMetadataInt(topProvider, "context_length"), positiveMetadataInt(limit, "context"))
		}
		if entry.MaxOutputTokens == 0 {
			entry.MaxOutputTokens = max(positiveMetadataInt(topProvider, "max_completion_tokens"), positiveMetadataInt(limit, "output"))
		}
		// A gateway that relays another gateway's list fills capability flags
		// with false for models it knows nothing about. Without a limit next
		// to it, a false flag is "not reported", so models.dev may still answer.
		if entry.ContextLength == 0 && entry.MaxOutputTokens == 0 {
			if entry.ToolCall != nil && !*entry.ToolCall {
				entry.ToolCall = nil
			}
			if entry.Reasoning != nil && !*entry.Reasoning {
				entry.Reasoning = nil
			}
		}
		if entry == (upstreamModelMetadata{}) {
			continue
		}
		metadata[id] = entry
	}
	return metadata
}

// upstreamCapabilityFlag reads one capability from a model entry. boolKeys
// are plain booleans; capabilityNames are looked up in a "capabilities" map
// or list; parameterNames in OpenRouter's "supported_parameters", which is a
// complete list, so a missing name there means unsupported. Token Harbor
// reports reasoning as an object whose "efforts" list is empty when the model
// has no reasoning level and null when unknown.
func upstreamCapabilityFlag(source map[string]any, boolKeys, capabilityNames, parameterNames []string) *bool {
	if source == nil {
		return nil
	}
	if flag, ok := metadataBool(source, boolKeys...); ok {
		return lo.ToPtr(flag)
	}
	for _, key := range boolKeys {
		detail, ok := source[key].(map[string]any)
		if !ok {
			continue
		}
		if efforts, ok := detail["efforts"].([]any); ok {
			return lo.ToPtr(len(efforts) > 0)
		}
		if effort, ok := detail["default_effort"].(string); ok && effort != "" {
			return lo.ToPtr(true)
		}
	}
	switch capabilities := source["capabilities"].(type) {
	case map[string]any:
		if flag, ok := metadataBool(capabilities, capabilityNames...); ok {
			return lo.ToPtr(flag)
		}
	case []any:
		// A capability list only proves what it contains.
		if slices.ContainsFunc(capabilities, func(value any) bool {
			name, _ := value.(string)
			return slices.Contains(capabilityNames, name)
		}) {
			return lo.ToPtr(true)
		}
	}
	if parameters, ok := source["supported_parameters"].([]any); ok {
		return lo.ToPtr(slices.ContainsFunc(parameters, func(value any) bool {
			name, _ := value.(string)
			return slices.Contains(parameterNames, name)
		}))
	}
	return nil
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
