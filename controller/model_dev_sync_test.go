package controller

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseModelsDevCatalog(t *testing.T) {
	catalogJSON := `{
		"providers": {
			"openai": {
				"id": "openai",
				"name": "OpenAI",
				"models": {
					"o3": {
						"id": "o3",
						"name": "O3",
						"reasoning": true,
						"tool_call": true,
						"limit": {"context": 200000, "output": 100000},
						"cost": {"input": 2.0, "output": 8.0, "reasoning": 8.0, "cache_read": 0.5}
					},
					"o4-mini": {
						"id": "o4-mini",
						"name": "O4 Mini",
						"reasoning": true,
						"tool_call": true,
						"limit": {"context": 200000, "output": 100000},
						"cost": {"input": 1.1, "output": 4.4, "reasoning": 4.4, "cache_read": 0.275}
					},
					"gpt-4.1": {
						"id": "gpt-4.1",
						"name": "GPT-4.1",
						"reasoning": false,
						"tool_call": true,
						"limit": {"context": 1000000, "output": 32768},
						"cost": {"input": 2.0, "output": 8.0}
					}
				}
			},
			"deepseek": {
				"id": "deepseek",
				"name": "DeepSeek",
				"models": {
					"deepseek-r1": {
						"id": "deepseek-r1",
						"name": "DeepSeek R1",
						"reasoning": true,
						"tool_call": false,
						"limit": {"context": 1000000, "output": 65536},
						"cost": {"input": 0.55, "output": 2.19, "reasoning": 2.19, "cache_read": 0.055}
					}
				}
			}
		}
	}`

	var catalog modelDevCatalog
	err := json.Unmarshal([]byte(catalogJSON), &catalog)
	require.NoError(t, err)

	assert.Len(t, catalog.Providers, 2)

	openai, ok := catalog.Providers["openai"]
	require.True(t, ok)
	assert.Equal(t, "OpenAI", openai.Name)
	assert.Len(t, openai.Models, 3)

	o3, ok := openai.Models["o3"]
	require.True(t, ok)
	assert.True(t, o3.Reasoning)
	assert.True(t, o3.ToolCall)
	assert.Equal(t, 200000, o3.Limit.Context)
	assert.Equal(t, 100000, o3.Limit.Output)
	assert.Equal(t, 2.0, o3.Cost.Input)
	assert.Equal(t, 8.0, o3.Cost.Output)
	assert.Equal(t, 8.0, o3.Cost.Reasoning)
	assert.Equal(t, 0.5, o3.Cost.CacheRead)

	deepseek, ok := catalog.Providers["deepseek"]
	require.True(t, ok)
	assert.Equal(t, "DeepSeek", deepseek.Name)

	dsr1, ok := deepseek.Models["deepseek-r1"]
	require.True(t, ok)
	assert.True(t, dsr1.Reasoning)
	assert.False(t, dsr1.ToolCall)
}

func TestMatchProviderToVendor(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		vendor   string
		want     bool
	}{
		{"exact match", "OpenAI", "OpenAI", true},
		{"case insensitive", "openai", "OpenAI", true},
		{"case insensitive reverse", "OPENAI", "openai", true},
		{"mismatch", "OpenAI", "Anthropic", false},
		{"empty provider", "", "OpenAI", false},
		{"empty vendor", "OpenAI", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchProviderToVendor(tt.provider, tt.vendor)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestModelsDevSyncCandidateDiff(t *testing.T) {
	candidate := modelsDevSyncCandidate{
		ModelName: "gpt-4.1",
		Provider:  "openai",
		Fields: []modelsDevSyncField{
			{Field: "context_length", Local: float64(0), Upstream: float64(1000000)},
			{Field: "max_output_tokens", Local: float64(0), Upstream: float64(32768)},
			{Field: "pricing_input", Local: float64(0), Upstream: float64(2.0)},
		},
	}

	assert.Equal(t, "gpt-4.1", candidate.ModelName)
	assert.Equal(t, "openai", candidate.Provider)
	assert.Len(t, candidate.Fields, 3)
	assert.Equal(t, "context_length", candidate.Fields[0].Field)
	assert.Equal(t, "max_output_tokens", candidate.Fields[1].Field)
	assert.Equal(t, "pricing_input", candidate.Fields[2].Field)
}

func TestModelDevSyncHandler(t *testing.T) {
	h := modelDevSyncHandler{}

	assert.Equal(t, "model_dev_sync", h.Type())
	assert.True(t, h.Enabled())
	assert.Equal(t, 24*time.Hour, h.Interval())
	assert.Nil(t, h.NewPayload())
}

// A provider's own API describes the model this gateway actually serves, so it
// wins over the models.dev catalog, which describes one model across every
// provider offering it. models.dev still fills whatever the provider omits.
func TestResolveModelCapabilityPrefersProviderOverCatalog(t *testing.T) {
	catalog := modelDevModel{
		Reasoning: true,
		ToolCall:  false,
		Limit:     modelDevLimit{Context: 128000, Output: 16384},
	}

	t.Run("provider wins for every field it reports", func(t *testing.T) {
		capability := resolveModelCapability(upstreamModelMetadata{
			ContextLength:   1048576,
			MaxOutputTokens: 65536,
			ToolCall:        lo.ToPtr(true),
		}, catalog)

		assert.Equal(t, 1048576, capability.ContextLength)
		assert.Equal(t, 65536, capability.MaxOutputTokens)
		assert.True(t, capability.ToolCall)
		// The provider reported no reasoning flag, so the catalog decides.
		assert.True(t, capability.Reasoning)
	})

	t.Run("catalog fills fields the provider omits", func(t *testing.T) {
		capability := resolveModelCapability(upstreamModelMetadata{}, catalog)

		assert.Equal(t, 128000, capability.ContextLength)
		assert.Equal(t, 16384, capability.MaxOutputTokens)
		assert.False(t, capability.ToolCall)
		assert.True(t, capability.ToolCallKnown)
	})

	t.Run("provider reporting false overrides catalog true", func(t *testing.T) {
		capability := resolveModelCapability(upstreamModelMetadata{ToolCall: lo.ToPtr(false)}, modelDevModel{ToolCall: true})

		assert.False(t, capability.ToolCall)
	})
}

// Most OpenAI-compatible providers return only id/created/owned_by. Those
// entries must contribute nothing so the model keeps resolving via models.dev
// instead of being zeroed out.
func TestOpenAIChannelModelMetadataIgnoresMetadataFreeEntries(t *testing.T) {
	items := []OpenAIModel{
		{ID: "plain-model", Object: "model", Created: 1, OwnedBy: "acme"},
		{ID: "rich-model", Metadata: map[string]any{
			"context_length":    float64(200000),
			"max_output_tokens": float64(32000),
			"description":       "  Fast model  ",
			"tool_call":         true,
		}},
	}

	metadata := openAIChannelModelMetadata(items)

	assert.NotContains(t, metadata, "plain-model")
	require.Contains(t, metadata, "rich-model")
	assert.Equal(t, 200000, metadata["rich-model"].ContextLength)
	assert.Equal(t, 32000, metadata["rich-model"].MaxOutputTokens)
	assert.Equal(t, "Fast model", metadata["rich-model"].Description)
	require.NotNil(t, metadata["rich-model"].ToolCall)
	assert.True(t, *metadata["rich-model"].ToolCall)
}

// A provider limit of zero means "not reported", never "this model has no
// context window", so it must not become a synced value.
func TestOpenAIChannelModelMetadataRejectsNonPositiveLimits(t *testing.T) {
	metadata := openAIChannelModelMetadata([]OpenAIModel{
		{ID: "zeroed", Metadata: map[string]any{"context_length": float64(0)}},
		{ID: "negative", Metadata: map[string]any{"context_length": float64(-1)}},
		{ID: "stringed", Metadata: map[string]any{"max_output_tokens": "8192"}},
	})

	assert.NotContains(t, metadata, "zeroed")
	assert.NotContains(t, metadata, "negative")
	assert.Equal(t, 8192, metadata["stringed"].MaxOutputTokens)
}

// The first channel that reports a field for a model wins, so two providers
// offering the same model cannot fight over it during one sync.
func TestMergeUpstreamModelMetadataFirstWriterWins(t *testing.T) {
	merged := map[string]upstreamModelMetadata{}

	mergeUpstreamModelMetadata(merged, map[string]upstreamModelMetadata{
		"gpt-4o": {ContextLength: 128000},
	}, "")
	mergeUpstreamModelMetadata(merged, map[string]upstreamModelMetadata{
		"gpt-4o": {ContextLength: 999999},
		"o3":     {ContextLength: 200000},
	}, "")

	assert.Equal(t, 128000, merged["gpt-4o"].ContextLength)
	assert.Equal(t, 200000, merged["o3"].ContextLength)
}

// A channel with model_prefix stores models as "prefix/name" in the database
// while the provider reports the bare name, so the merged key must carry the
// prefix back.
func TestMergeUpstreamModelMetadataAppliesChannelPrefix(t *testing.T) {
	merged := map[string]upstreamModelMetadata{}

	mergeUpstreamModelMetadata(merged, map[string]upstreamModelMetadata{
		"deepseek-chat": {ContextLength: 128000},
	}, "deepseek")

	require.Contains(t, merged, "deepseek/deepseek-chat")
	assert.Equal(t, 128000, merged["deepseek/deepseek-chat"].ContextLength)
}
