package controller

import (
	"encoding/json"
	"testing"

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
