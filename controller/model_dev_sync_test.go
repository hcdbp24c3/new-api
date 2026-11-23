package controller

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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

// Token Harbor reports limits as flat fields and reasoning as an object, with
// an empty efforts list meaning "no reasoning" and null meaning "unknown".
func TestOpenAIRawChannelModelMetadataReadsGatewayShapes(t *testing.T) {
	var items []map[string]any
	require.NoError(t, common.Unmarshal([]byte(`[
		{"id":"deepseek-v4.1-flash:free","context_length":1048576,"max_output_tokens":384000,
		 "reasoning":{"efforts":["low","high"],"default_effort":"high","can_disable":true}},
		{"id":"no-reasoning","context_length":131072,"reasoning":{"efforts":[],"default_effort":null}},
		{"id":"unknown-reasoning","context_length":131072,"reasoning":{"efforts":null}},
		{"id":"openrouter-style","top_provider":{"context_length":200000,"max_completion_tokens":64000},
		 "supported_parameters":["tools","temperature"]},
		{"id":"litellm-style","model_info":{"max_input_tokens":128000,"max_output_tokens":16384,
		 "supports_function_calling":true,"supports_reasoning":true}},
		{"id":"models-dev-style","limit":{"context":400000,"output":128000},"capabilities":{"tools":true,"reasoning":false}},
		{"id":"window-style","context_window":65536},
		{"id":"relayed-blank","reasoning":false,"tool_call":false},
		{"id":"plain","object":"model","owned_by":"acme"}
	]`), &items))

	metadata := openAIRawChannelModelMetadata(items)

	free := metadata["deepseek-v4.1-flash:free"]
	assert.Equal(t, 1048576, free.ContextLength)
	assert.Equal(t, 384000, free.MaxOutputTokens)
	require.NotNil(t, free.Reasoning)
	assert.True(t, *free.Reasoning)
	assert.Nil(t, free.ToolCall)

	require.NotNil(t, metadata["no-reasoning"].Reasoning)
	assert.False(t, *metadata["no-reasoning"].Reasoning)
	assert.Nil(t, metadata["unknown-reasoning"].Reasoning)

	router := metadata["openrouter-style"]
	assert.Equal(t, 200000, router.ContextLength)
	assert.Equal(t, 64000, router.MaxOutputTokens)
	require.NotNil(t, router.ToolCall)
	assert.True(t, *router.ToolCall)
	require.NotNil(t, router.Reasoning)
	assert.False(t, *router.Reasoning)

	lite := metadata["litellm-style"]
	assert.Equal(t, 128000, lite.ContextLength)
	assert.Equal(t, 16384, lite.MaxOutputTokens)
	require.NotNil(t, lite.ToolCall)
	assert.True(t, *lite.ToolCall)
	require.NotNil(t, lite.Reasoning)
	assert.True(t, *lite.Reasoning)

	dev := metadata["models-dev-style"]
	assert.Equal(t, 400000, dev.ContextLength)
	assert.Equal(t, 128000, dev.MaxOutputTokens)
	require.NotNil(t, dev.ToolCall)
	assert.True(t, *dev.ToolCall)
	require.NotNil(t, dev.Reasoning)
	assert.False(t, *dev.Reasoning)

	assert.Equal(t, 65536, metadata["window-style"].ContextLength)
	assert.NotContains(t, metadata, "relayed-blank")
	assert.NotContains(t, metadata, "plain")
}

func capabilityTestCatalog() *modelDevCatalog {
	return &modelDevCatalog{Providers: map[string]modelDevProvider{
		"deepseek": {Id: "deepseek", Name: "DeepSeek", Models: map[string]modelDevModel{
			"deepseek-v4.1-flash": {Id: "deepseek-v4.1-flash", Reasoning: true, ToolCall: true,
				Limit: modelDevLimit{Context: 1000000, Output: 384000}, Cost: modelDevCost{Input: 0.3, Output: 1.2}},
		}},
		"opencode": {Id: "opencode", Name: "OpenCode Zen", Models: map[string]modelDevModel{
			"deepseek-v4.1-flash-free": {Id: "deepseek-v4.1-flash-free", Reasoning: true, ToolCall: true,
				Limit: modelDevLimit{Context: 200000, Output: 64000}},
		}},
		"other": {Id: "other", Name: "Other", Models: map[string]modelDevModel{
			"deepseek-v4.1-flash": {Id: "deepseek-v4.1-flash", Limit: modelDevLimit{Context: 128000, Output: 8000}},
			"vendor/nested-model": {Id: "vendor/nested-model", Limit: modelDevLimit{Context: 32000}},
		}},
		"third": {Id: "third", Name: "Third", Models: map[string]modelDevModel{
			"deepseek-v4.1-flash": {Id: "deepseek-v4.1-flash", Limit: modelDevLimit{Context: 1000000, Output: 384000}},
		}},
	}}
}

// A suffix names a different model: "-free" and ":free" are never stripped,
// only leading channel segments are.
func TestModelDevIndexLookupKeepsSuffixes(t *testing.T) {
	index := buildModelDevIndex(capabilityTestCatalog())

	entry, ok := index.lookup("opencode2api/deepseek-v4.1-flash-free", "")
	require.True(t, ok)
	assert.Equal(t, 200000, entry.Model.Limit.Context)

	_, ok = index.lookup("tokenharbor/deepseek-v4.1-flash:free", "")
	assert.False(t, ok)
	_, ok = index.lookup("deepseek-v4.1-flash-latest", "")
	assert.False(t, ok)

	// Several providers list the bare name: the common context size wins.
	entry, ok = index.lookup("anychannel/deepseek-v4.1-flash", "")
	require.True(t, ok)
	assert.Equal(t, 1000000, entry.Model.Limit.Context)

	// A provider named in the gateway name or matching the vendor wins.
	entry, ok = index.lookup("other/deepseek-v4.1-flash", "")
	require.True(t, ok)
	assert.Equal(t, 128000, entry.Model.Limit.Context)
	entry, ok = index.lookup("deepseek-v4.1-flash", "other")
	require.True(t, ok)
	assert.Equal(t, 128000, entry.Model.Limit.Context)

	entry, ok = index.lookup("channel/vendor/nested-model", "")
	require.True(t, ok)
	assert.Equal(t, 32000, entry.Model.Limit.Context)
	entry, ok = index.lookup("nested-model", "")
	require.True(t, ok)
	assert.Equal(t, 32000, entry.Model.Limit.Context)
}

// The order is provider API, then models.dev, then nothing.
func TestPlanCapabilitySyncOrder(t *testing.T) {
	channel := func(id int, models, prefix string) model.Channel {
		ch := model.Channel{Id: id, Status: common.ChannelStatusEnabled, Models: models}
		if prefix != "" {
			ch.OtherSettings = `{"model_prefix":"` + prefix + `"}`
		}
		return ch
	}
	state := capabilitySyncState{
		Index:   buildModelDevIndex(capabilityTestCatalog()),
		Vendors: []*model.Vendor{{Id: 1, Name: "DeepSeek"}},
		Models: []*model.Model{
			{Id: 1, ModelName: "deepseek-v4.1-flash", VendorID: 1, SyncOfficial: 1},
			{Id: 2, ModelName: "frozen-model", SyncOfficial: 0},
			{Id: 3, ModelName: "ruled-", NameRule: model.NameRulePrefix, SyncOfficial: 1},
		},
		Channels: []model.Channel{
			channel(1, "deepseek-v4.1-flash:free,api-only-model,unknown-model,frozen-model,ruled-model", "tokenharbor"),
			channel(2, "deepseek-v4.1-flash-free,deepseek-v4.1-flash", "opencode2api"),
			channel(3, "deepseek-v4.1-flash", ""),
		},
		Upstream: map[string]upstreamModelMetadata{
			"tokenharbor/deepseek-v4.1-flash:free": {ContextLength: 1048576, Reasoning: lo.ToPtr(true)},
			"tokenharbor/api-only-model":           {ContextLength: 64000, ToolCall: lo.ToPtr(true)},
			"tokenharbor/frozen-model":             {ContextLength: 64000},
			"tokenharbor/ruled-model":              {ContextLength: 64000},
			"deepseek-v4.1-flash":                  {MaxOutputTokens: 100000},
		},
	}

	items := planCapabilitySync(state)
	byName := map[string]capabilitySyncItem{}
	for _, item := range items {
		byName[item.Candidate.ModelName] = item
	}
	value := func(name, field string) any {
		for _, f := range byName[name].Candidate.Fields {
			if f.Field == field {
				return f.Upstream
			}
		}
		return nil
	}

	// API only: the suffixed name has no catalog entry, and none is borrowed.
	require.Contains(t, byName, "tokenharbor/deepseek-v4.1-flash:free")
	assert.Equal(t, "create", byName["tokenharbor/deepseek-v4.1-flash:free"].Candidate.Kind)
	assert.Equal(t, "api", byName["tokenharbor/deepseek-v4.1-flash:free"].Candidate.Provider)
	assert.Equal(t, 1048576, value("tokenharbor/deepseek-v4.1-flash:free", "context_length"))
	assert.Equal(t, true, value("tokenharbor/deepseek-v4.1-flash:free", "reasoning"))
	assert.Nil(t, value("tokenharbor/deepseek-v4.1-flash:free", "max_output_tokens"))
	assert.Nil(t, value("tokenharbor/deepseek-v4.1-flash:free", "tool_call"))
	assert.Equal(t, 64000, value("tokenharbor/api-only-model", "context_length"))

	// models.dev only, matched by exact name with the suffix kept.
	assert.Equal(t, 200000, value("opencode2api/deepseek-v4.1-flash-free", "context_length"))
	assert.Nil(t, byName["opencode2api/deepseek-v4.1-flash-free"].Pricing)

	// API wins per field, the catalog fills the rest, vendor-matched prices apply.
	assert.Equal(t, "update", byName["deepseek-v4.1-flash"].Candidate.Kind)
	assert.Equal(t, 100000, value("deepseek-v4.1-flash", "max_output_tokens"))
	assert.Equal(t, 1000000, value("deepseek-v4.1-flash", "context_length"))
	assert.Equal(t, 0.3, value("deepseek-v4.1-flash", "pricing_input"))

	// Nothing known, sync turned off, covered by a rule or by the bare record.
	assert.NotContains(t, byName, "tokenharbor/unknown-model")
	assert.NotContains(t, byName, "frozen-model")
	assert.NotContains(t, byName, "tokenharbor/frozen-model")
	assert.NotContains(t, byName, "tokenharbor/ruled-model")
	assert.NotContains(t, byName, "opencode2api/deepseek-v4.1-flash")
}

// A model the gateway serves without a metadata record gets one from the
// provider's own API, and /v1/models then reports it under the client name.
func TestApplyCapabilitySyncItemCreatesRecord(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")
	require.NoError(t, db.Create(&model.Channel{Id: 1, Name: "th", Key: "k", Status: common.ChannelStatusEnabled,
		Models: "deepseek-v4.1-flash:free", Group: "default", OtherSettings: `{"model_prefix":"tokenharbor"}`}).Error)
	require.NoError(t, db.Create(&model.Model{ModelName: "deepseek-v4.1-flash:free", ContextLength: 1}).Error)

	const name = "tokenharbor/deepseek-v4.1-flash:free"
	items := planCapabilitySync(capabilitySyncState{
		Index:    buildModelDevIndex(capabilityTestCatalog()),
		Channels: []model.Channel{{Id: 1, Status: common.ChannelStatusEnabled, Models: "deepseek-v4.1-flash:free", OtherSettings: `{"model_prefix":"tokenharbor"}`}},
		Upstream: map[string]upstreamModelMetadata{name: {ContextLength: 1048576, Reasoning: lo.ToPtr(true)}},
	})
	require.Len(t, items, 1)
	changed, err := applyCapabilitySyncItem(items[0])
	require.NoError(t, err)
	assert.True(t, changed)

	var stored model.Model
	require.NoError(t, db.Where("model_name = ?", name).First(&stored).Error)
	assert.Equal(t, 1048576, stored.ContextLength)
	assert.True(t, stored.Reasoning)
	assert.Equal(t, 1, stored.Status)
	assert.Equal(t, 1, stored.SyncOfficial)

	capabilities, err := model.GetModelCapabilitiesByNames([]string{name})
	require.NoError(t, err)
	assert.Equal(t, 1048576, capabilities[name].ContextLength)
}

// A record the capability sync creates is named after the prefixed name
// clients call; the model list must still tie it to the channel serving it.
func TestEnrichModelsBindsPrefixedClientName(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")
	channel := &model.Channel{Id: 1, Name: "owu", Key: "k", Status: common.ChannelStatusEnabled,
		Models: "host/gpt-5.2", Group: "default", OtherSettings: `{"model_prefix":"owu"}`}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	record := &model.Model{ModelName: "owu/host/gpt-5.2", ContextLength: 400000, Status: 1}
	require.NoError(t, db.Create(record).Error)
	other := &model.Model{ModelName: "owu/host/gone", Status: 1}
	require.NoError(t, db.Create(other).Error)

	require.NoError(t, enrichModels([]*model.Model{record, other}))
	assert.Equal(t, 1, record.ConfiguredChannelCount)
	assert.Equal(t, model.ModelSquareVisible, record.SquareState)
	assert.Zero(t, other.ConfiguredChannelCount)
	assert.Equal(t, model.ModelSquareUnavailable, other.SquareState)
}
