package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

const modelDevSyncURL = "https://models.dev/catalog.json"

// Catalog types for parsing models.dev/catalog.json.
// These are separate from the ratio_sync.go modelsDev* types because they
// carry richer metadata (id, name, reasoning, tool_call, limit, cost).

type modelDevCatalog struct {
	Models    map[string]any              `json:"models"`
	Providers map[string]modelDevProvider `json:"providers"`
}

type modelDevProvider struct {
	Id     string                   `json:"id"`
	Name   string                   `json:"name"`
	Models map[string]modelDevModel `json:"models"`
}

type modelDevModel struct {
	Id        string        `json:"id"`
	Name      string        `json:"name"`
	Reasoning bool          `json:"reasoning"`
	ToolCall  bool          `json:"tool_call"`
	Limit     modelDevLimit `json:"limit"`
	Cost      modelDevCost  `json:"cost"`
}

type modelDevLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

type modelDevCost struct {
	Input     float64 `json:"input"`
	Output    float64 `json:"output"`
	Reasoning float64 `json:"reasoning"`
	CacheRead float64 `json:"cache_read"`
}

// Sync types for preview and apply

type modelsDevSyncSource struct {
	URL     string `json:"url"`
	Version string `json:"version"`
}

type modelsDevSyncField struct {
	Field    string `json:"field"`
	Local    any    `json:"local"`
	Upstream any    `json:"upstream"`
}

type modelsDevSyncCandidate struct {
	ModelName string               `json:"model_name"`
	Provider  string               `json:"provider"`
	Kind      string               `json:"kind"`
	Fields    []modelsDevSyncField `json:"fields"`
}

type modelsDevSyncSelection struct {
	ModelName string `json:"model_name"`
	Provider  string `json:"provider"`
}

// HTTP client for models.dev with caching

var (
	modelsDevBodyCache   []byte
	modelsDevBodyCacheMu sync.RWMutex
	modelsDevEtag        string
)

func getModelsDevHTTPClient() *http.Client {
	timeoutSec := common.GetEnvOrDefault("MODELS_DEV_HTTP_TIMEOUT_SECONDS", 15)
	dialer := &net.Dialer{Timeout: time.Duration(timeoutSec) * time.Second}
	transport := &http.Transport{
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   time.Duration(timeoutSec) * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: time.Duration(timeoutSec) * time.Second,
	}
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
	}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		if strings.HasSuffix(host, "models.dev") {
			if conn, err := dialer.DialContext(ctx, "tcp4", addr); err == nil {
				return conn, nil
			}
			return dialer.DialContext(ctx, "tcp6", addr)
		}
		return dialer.DialContext(ctx, network, addr)
	}
	return &http.Client{Transport: transport}
}

func fetchModelDevCatalog(c *gin.Context) (*modelDevCatalog, string, error) {
	ctx := context.Background()
	if c != nil {
		ctx = c.Request.Context()
	}
	return fetchModelDevCatalogWithContext(ctx)
}

func fetchModelDevCatalogWithContext(ctx context.Context) (*modelDevCatalog, string, error) {
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, time.Duration(common.GetEnvOrDefault("MODELS_DEV_HTTP_TIMEOUT_SECONDS", 15))*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelDevSyncURL, nil)
	if err != nil {
		return nil, "", err
	}

	modelsDevBodyCacheMu.RLock()
	if modelsDevEtag != "" {
		req.Header.Set("If-None-Match", modelsDevEtag)
	}
	modelsDevBodyCacheMu.RUnlock()

	var lastErr error
	attempts := max(common.GetEnvOrDefault("MODELS_DEV_HTTP_RETRY", 3), 1)
	baseDelay := 200 * time.Millisecond
	maxMB := common.GetEnvOrDefault("MODELS_DEV_HTTP_MAX_MB", 10)
	maxBytes := int64(maxMB) << 20

	for attempt := 0; attempt < attempts; attempt++ {
		resp, err := getModelsDevHTTPClient().Do(req)
		if err != nil {
			lastErr = err
			sleep := baseDelay * time.Duration(1<<attempt)
			jitter := time.Duration(rand.Intn(150)) * time.Millisecond
			time.Sleep(sleep + jitter)
			continue
		}

		var body []byte
		func() {
			defer resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusOK:
				limited := io.LimitReader(resp.Body, maxBytes+1)
				buf, readErr := io.ReadAll(limited)
				if readErr != nil {
					lastErr = readErr
					return
				}
				if int64(len(buf)) > maxBytes {
					lastErr = fmt.Errorf("models.dev catalog exceeds size limit")
					return
				}
				modelsDevBodyCacheMu.Lock()
				if et := resp.Header.Get("ETag"); et != "" {
					modelsDevEtag = et
				}
				modelsDevBodyCache = buf
				modelsDevBodyCacheMu.Unlock()
				body = buf
				lastErr = nil
			case http.StatusNotModified:
				modelsDevBodyCacheMu.RLock()
				body = modelsDevBodyCache
				modelsDevBodyCacheMu.RUnlock()
				if len(body) == 0 {
					lastErr = fmt.Errorf("cache miss for 304 response")
					return
				}
				lastErr = nil
			default:
				lastErr = fmt.Errorf("models.dev returned %s", resp.Status)
			}
		}()

		if lastErr == nil {
			break
		}
		sleep := baseDelay * time.Duration(1<<attempt)
		jitter := time.Duration(rand.Intn(150)) * time.Millisecond
		time.Sleep(sleep + jitter)
	}

	if lastErr != nil {
		return nil, "", lastErr
	}

	modelsDevBodyCacheMu.RLock()
	body := modelsDevBodyCache
	modelsDevBodyCacheMu.RUnlock()

	var catalog modelDevCatalog
	if err := common.Unmarshal(body, &catalog); err != nil {
		return nil, "", fmt.Errorf("parse models.dev catalog: %w", err)
	}

	version := fmt.Sprintf("%x", sha256.Sum256(body))
	return &catalog, version, nil
}

// matchProviderToVendor checks if a provider name matches a vendor (case-insensitive).
func matchProviderToVendor(provider, vendor string) bool {
	if provider == "" || vendor == "" {
		return false
	}
	return strings.EqualFold(provider, vendor)
}

// modelDevEntry is one catalog model together with the provider listing it.
type modelDevEntry struct {
	ProviderKey  string
	ProviderName string
	Model        modelDevModel
}

// modelDevIndex finds catalog models by their full ID and by the last ID
// segment, so a gateway name such as "channel/vendor/model" still resolves.
// Only leading segments are dropped: a suffix such as "-free", ":free" or
// "-latest" is part of the model name and names a different catalog entry.
type modelDevIndex struct {
	byID   map[string][]modelDevEntry
	byTail map[string][]modelDevEntry
}

func buildModelDevIndex(catalog *modelDevCatalog) modelDevIndex {
	index := modelDevIndex{byID: map[string][]modelDevEntry{}, byTail: map[string][]modelDevEntry{}}
	if catalog == nil {
		return index
	}
	providerKeys := make([]string, 0, len(catalog.Providers))
	for key := range catalog.Providers {
		providerKeys = append(providerKeys, key)
	}
	slices.Sort(providerKeys)
	for _, key := range providerKeys {
		provider := catalog.Providers[key]
		modelKeys := make([]string, 0, len(provider.Models))
		for modelKey := range provider.Models {
			modelKeys = append(modelKeys, modelKey)
		}
		slices.Sort(modelKeys)
		for _, modelKey := range modelKeys {
			devModel := provider.Models[modelKey]
			if devModel.Id == "" {
				devModel.Id = modelKey
			}
			entry := modelDevEntry{ProviderKey: key, ProviderName: provider.Name, Model: devModel}
			id := strings.ToLower(devModel.Id)
			index.byID[id] = append(index.byID[id], entry)
			tail := id[strings.LastIndex(id, "/")+1:]
			index.byTail[tail] = append(index.byTail[tail], entry)
		}
	}
	return index
}

// lookup returns the catalog entry for a gateway model name. When several
// providers list the model, a provider named in the gateway name wins, then
// the provider matching the model's own vendor, then the most common context
// size (the smaller one on a tie), so the choice never depends on map order.
func (index modelDevIndex) lookup(name, vendor string) (modelDevEntry, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	segments := strings.Split(lower, "/")
	var candidates []modelDevEntry
	for start := range segments {
		if candidates = index.byID[strings.Join(segments[start:], "/")]; len(candidates) > 0 {
			break
		}
	}
	if len(candidates) == 0 {
		candidates = index.byTail[segments[len(segments)-1]]
	}
	if len(candidates) == 0 {
		return modelDevEntry{}, false
	}
	for _, candidate := range candidates {
		if slices.Contains(segments[:len(segments)-1], strings.ToLower(candidate.ProviderKey)) {
			return candidate, true
		}
	}
	for _, candidate := range candidates {
		if matchProviderToVendor(candidate.ProviderName, vendor) {
			return candidate, true
		}
	}
	counts := make(map[int]int, len(candidates))
	for _, candidate := range candidates {
		counts[candidate.Model.Limit.Context]++
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		count, bestCount := counts[candidate.Model.Limit.Context], counts[best.Model.Limit.Context]
		if count > bestCount || (count == bestCount && candidate.Model.Limit.Context < best.Model.Limit.Context) {
			best = candidate
		}
	}
	return best, true
}

// channelClientModelName is the name clients use for a channel model: the
// channel's model_prefix followed by the provider's own model name.
func channelClientModelName(name, prefix string) string {
	if prefix == "" {
		return name
	}
	return prefix + "/" + strings.TrimPrefix(name, prefix+"/")
}

// loadChannelModelMetadata asks every enabled channel that serves a wanted
// model for its own metadata, so capability fields resolve from the provider
// that actually serves the model before any third-party catalog. A provider
// that errors is skipped so its models fall back to models.dev. Results are
// merged in channel ID order to keep the outcome stable between runs.
func loadChannelModelMetadata(channels []model.Channel, wanted map[string]struct{}) map[string]upstreamModelMetadata {
	results := make([]map[string]upstreamModelMetadata, len(channels))
	slots := make(chan struct{}, 8)
	var group sync.WaitGroup
	for i := range channels {
		channel := &channels[i]
		if channel.Status != common.ChannelStatusEnabled || !channelServesAnyModel(channel, wanted) {
			continue
		}
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.LogWarn(context.Background(), fmt.Sprintf("model metadata sync: channel %d metadata panicked: %v", channel.Id, recovered))
				}
			}()
			slots <- struct{}{}
			defer func() { <-slots }()
			metadata, err := fetchChannelUpstreamModelMetadata(channel)
			if err != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf(
					"model metadata sync: channel %d metadata unavailable, falling back to models.dev: %s",
					channel.Id, err.Error()))
				return
			}
			results[i] = metadata
		}()
	}
	group.Wait()

	merged := make(map[string]upstreamModelMetadata)
	for i := range channels {
		mergeUpstreamModelMetadata(merged, results[i], model.ExtractModelPrefix(channels[i].OtherSettings))
	}
	return merged
}

// channelServesAnyModel reports whether the channel lists any model the sync is
// about to touch, so a deployment does not query a provider it has no use for.
func channelServesAnyModel(channel *model.Channel, wanted map[string]struct{}) bool {
	prefix := model.ExtractModelPrefix(channel.OtherSettings)
	for _, name := range channel.GetModels() {
		if _, exists := wanted[name]; exists {
			return true
		}
		if _, exists := wanted[channelClientModelName(name, prefix)]; exists {
			return true
		}
	}
	return false
}

// mergeUpstreamModelMetadata folds one channel's result into the shared map
// under the name clients use. The first channel that reports a model wins, so
// two providers offering the same model cannot fight over it during one sync.
func mergeUpstreamModelMetadata(merged, metadata map[string]upstreamModelMetadata, prefix string) {
	for name, entry := range metadata {
		key := channelClientModelName(name, prefix)
		if _, exists := merged[key]; exists {
			continue
		}
		merged[key] = entry
	}
}

// modelCapabilitySource is the merged view of one model's capabilities: the
// provider's own API first, then the models.dev catalog for whatever the
// provider did not report, and nothing when neither knows. The provider is
// authoritative because it describes the model this gateway actually serves;
// models.dev is a third-party catalog that covers a model across every
// provider offering it.
type modelCapabilitySource struct {
	ContextLength   int
	MaxOutputTokens int
	Reasoning       bool
	ReasoningKnown  bool
	ToolCall        bool
	ToolCallKnown   bool
}

// resolveModelCapability prefers the provider's metadata and falls back to
// models.dev per field. A provider that reports nothing usable, which is the
// common case for OpenAI-compatible channels, leaves the whole capability set
// to models.dev.
func resolveModelCapability(provider upstreamModelMetadata, dev modelDevModel) modelCapabilitySource {
	return mergeModelCapability(provider, &dev)
}

// mergeModelCapability accepts a nil catalog model for names models.dev does
// not list; the provider's own metadata is then the only source.
func mergeModelCapability(provider upstreamModelMetadata, dev *modelDevModel) modelCapabilitySource {
	source := modelCapabilitySource{}
	if dev != nil {
		source = modelCapabilitySource{
			ContextLength:   dev.Limit.Context,
			MaxOutputTokens: dev.Limit.Output,
			Reasoning:       dev.Reasoning,
			ReasoningKnown:  true,
			ToolCall:        dev.ToolCall,
			ToolCallKnown:   true,
		}
	}
	if provider.ContextLength > 0 {
		source.ContextLength = provider.ContextLength
	}
	if provider.MaxOutputTokens > 0 {
		source.MaxOutputTokens = provider.MaxOutputTokens
	}
	if provider.ToolCall != nil {
		source.ToolCall, source.ToolCallKnown = *provider.ToolCall, true
	}
	if provider.Reasoning != nil {
		source.Reasoning, source.ReasoningKnown = *provider.Reasoning, true
	}
	return source
}

// buildModelDevCapabilityFields returns field diffs between a local model and
// the merged provider/catalog capabilities, plus catalog pricing.
func buildModelDevCapabilityFields(local *model.Model, dev modelDevModel, provider upstreamModelMetadata) []modelsDevSyncField {
	return capabilityDiff(local, resolveModelCapability(provider, dev), &dev)
}

// capabilityDiff lists the fields whose stored value differs from the merged
// capability. pricing is nil when no catalog entry may set prices.
func capabilityDiff(local *model.Model, capability modelCapabilitySource, pricing *modelDevModel) []modelsDevSyncField {
	var fields []modelsDevSyncField
	if capability.ContextLength > 0 && local.ContextLength != capability.ContextLength {
		fields = append(fields, modelsDevSyncField{Field: "context_length", Local: local.ContextLength, Upstream: capability.ContextLength})
	}
	if capability.MaxOutputTokens > 0 && local.MaxOutputTokens != capability.MaxOutputTokens {
		fields = append(fields, modelsDevSyncField{Field: "max_output_tokens", Local: local.MaxOutputTokens, Upstream: capability.MaxOutputTokens})
	}
	if capability.ReasoningKnown && local.Reasoning != capability.Reasoning {
		fields = append(fields, modelsDevSyncField{Field: "reasoning", Local: local.Reasoning, Upstream: capability.Reasoning})
	}
	if capability.ToolCallKnown && local.ToolCall != capability.ToolCall {
		fields = append(fields, modelsDevSyncField{Field: "tool_call", Local: local.ToolCall, Upstream: capability.ToolCall})
	}
	if pricing == nil {
		return fields
	}
	if pricing.Cost.Input > 0 && local.PricingInput != pricing.Cost.Input {
		fields = append(fields, modelsDevSyncField{Field: "pricing_input", Local: local.PricingInput, Upstream: pricing.Cost.Input})
	}
	if pricing.Cost.Output > 0 && local.PricingOutput != pricing.Cost.Output {
		fields = append(fields, modelsDevSyncField{Field: "pricing_output", Local: local.PricingOutput, Upstream: pricing.Cost.Output})
	}
	if pricing.Cost.CacheRead > 0 && local.PricingCache != pricing.Cost.CacheRead {
		fields = append(fields, modelsDevSyncField{Field: "pricing_cache", Local: local.PricingCache, Upstream: pricing.Cost.CacheRead})
	}
	return fields
}

// capabilitySyncState is everything one sync run reads before deciding.
type capabilitySyncState struct {
	Models   []*model.Model
	Vendors  []*model.Vendor
	Channels []model.Channel
	Upstream map[string]upstreamModelMetadata
	Index    modelDevIndex
}

// capabilitySyncItem is one model the sync would create or update.
type capabilitySyncItem struct {
	Candidate modelsDevSyncCandidate
	Local     *model.Model   // nil when the record does not exist yet
	Pricing   *modelDevModel // catalog entry allowed to set prices
}

// planCapabilitySync decides, for every model this gateway serves or keeps a
// record for, what to store: the serving provider's own metadata first, then
// models.dev, and nothing when neither knows the model. A model without a
// record gets one, unless a name rule or a bare-name record already covers it.
func planCapabilitySync(state capabilitySyncState) []capabilitySyncItem {
	vendorNameByID := make(map[int]string, len(state.Vendors))
	for _, vendor := range state.Vendors {
		vendorNameByID[vendor.Id] = vendor.Name
	}
	exact := make(map[string]*model.Model, len(state.Models))
	var rules []*model.Model
	for _, record := range state.Models {
		if record.NameRule == model.NameRuleExact {
			exact[record.ModelName] = record
			continue
		}
		rules = append(rules, record)
	}

	// served maps a client model name to its bare name ("" without a prefix).
	served := make(map[string]string)
	for i := range state.Channels {
		if state.Channels[i].Status != common.ChannelStatusEnabled {
			continue
		}
		prefix := model.ExtractModelPrefix(state.Channels[i].OtherSettings)
		for _, name := range state.Channels[i].GetModels() {
			if name = strings.TrimSpace(name); name == "" {
				continue
			}
			client := channelClientModelName(name, prefix)
			bare := ""
			if client != name || prefix != "" {
				bare = strings.TrimPrefix(client, prefix+"/")
			}
			served[client] = bare
		}
	}

	names := make([]string, 0, len(exact)+len(served))
	for name := range exact {
		names = append(names, name)
	}
	for name := range served {
		if _, exists := exact[name]; !exists {
			names = append(names, name)
		}
	}
	slices.Sort(names)

	items := make([]capabilitySyncItem, 0)
	for _, name := range names {
		local := exact[name]
		if local != nil && local.SyncOfficial == 0 {
			// The administrator turned syncing off for this record.
			continue
		}
		upstream, hasUpstream := state.Upstream[name]
		hasUpstream = hasUpstream && upstream != (upstreamModelMetadata{})
		vendor := ""
		if local != nil {
			vendor = vendorNameByID[local.VendorID]
		}
		entry, hasEntry := state.Index.lookup(name, vendor)
		if !hasUpstream && !hasEntry {
			continue
		}
		if local == nil {
			if slices.ContainsFunc(rules, func(rule *model.Model) bool {
				return rule.MatchesName(name) || (served[name] != "" && rule.MatchesName(served[name]))
			}) {
				continue
			}
			// models.dev describes the bare model, and the bare record already
			// answers for every prefixed name; only the provider's own
			// metadata is specific to this channel.
			if bare, bareExists := exact[served[name]]; bareExists && (!hasUpstream || bare.SyncOfficial == 0) {
				continue
			}
		}

		item := capabilitySyncItem{Local: local, Candidate: modelsDevSyncCandidate{ModelName: name, Kind: "update"}}
		var dev *modelDevModel
		sources := make([]string, 0, 2)
		if hasUpstream {
			sources = append(sources, "api")
		}
		if hasEntry {
			dev = &entry.Model
			sources = append(sources, entry.ProviderKey)
			// Prices only come from a catalog provider the site keeps as a vendor.
			if slices.ContainsFunc(state.Vendors, func(v *model.Vendor) bool { return matchProviderToVendor(entry.ProviderName, v.Name) }) {
				item.Pricing = dev
			}
		}
		item.Candidate.Provider = strings.Join(sources, " + ")

		current := local
		if current == nil {
			current = &model.Model{}
			item.Candidate.Kind = "create"
		}
		item.Candidate.Fields = capabilityDiff(current, mergeModelCapability(upstream, dev), item.Pricing)
		if len(item.Candidate.Fields) == 0 {
			continue
		}
		items = append(items, item)
	}
	return items
}

// loadCapabilitySyncPlan reads the site state, asks the channels for their
// own metadata and returns the resulting plan.
func loadCapabilitySyncPlan(catalog *modelDevCatalog) ([]capabilitySyncItem, error) {
	state := capabilitySyncState{Index: buildModelDevIndex(catalog)}
	if err := model.DB.Find(&state.Models).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Find(&state.Vendors).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Order("id").Find(&state.Channels).Error; err != nil {
		return nil, err
	}
	wanted := make(map[string]struct{}, len(state.Models))
	for _, record := range state.Models {
		wanted[record.ModelName] = struct{}{}
	}
	for i := range state.Channels {
		prefix := model.ExtractModelPrefix(state.Channels[i].OtherSettings)
		for _, name := range state.Channels[i].GetModels() {
			wanted[channelClientModelName(strings.TrimSpace(name), prefix)] = struct{}{}
		}
	}
	state.Upstream = loadChannelModelMetadata(state.Channels, wanted)
	return planCapabilitySync(state), nil
}

// applyCapabilitySyncItem writes one planned change and reports whether the
// database changed.
func applyCapabilitySyncItem(item capabilitySyncItem) (bool, error) {
	if len(item.Candidate.Fields) == 0 {
		return false, nil
	}
	updates := make(map[string]any, len(item.Candidate.Fields)+1)
	for _, field := range item.Candidate.Fields {
		updates[field.Field] = field.Upstream
	}
	local := item.Local
	if local == nil {
		local = &model.Model{ModelName: item.Candidate.ModelName, Status: 1, SyncOfficial: 1, NameRule: model.NameRuleExact}
		if err := local.Insert(); err != nil {
			return false, err
		}
	}
	updates["updated_time"] = common.GetTimestamp()
	if err := model.DB.Model(&model.Model{}).Where("id = ?", local.Id).Updates(updates).Error; err != nil {
		return false, err
	}
	return true, nil
}

// SyncModelsDevPreview returns the models whose capabilities would change,
// with the provider API as the first source and models.dev as the second.
func SyncModelsDevPreview(c *gin.Context) {
	catalog, version, err := fetchModelDevCatalog(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items, err := loadCapabilitySyncPlan(catalog)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	candidates := make([]modelsDevSyncCandidate, 0, len(items))
	for _, item := range items {
		candidates = append(candidates, item.Candidate)
	}
	common.ApiSuccess(c, gin.H{
		"source":     modelsDevSyncSource{URL: modelDevSyncURL, Version: version},
		"candidates": candidates,
	})
}

// SyncModelsDevApply applies the selected capability changes.
func SyncModelsDevApply(c *gin.Context) {
	var request struct {
		SourceVersion string                   `json:"source_version"`
		Selections    []modelsDevSyncSelection `json:"selections"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || len(request.Selections) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Preview and select changes before applying"})
		return
	}

	catalog, version, err := fetchModelDevCatalog(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if version != request.SourceVersion {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "Upstream catalog changed; preview again"})
		return
	}
	items, err := loadCapabilitySyncPlan(catalog)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	selected := make(map[string]struct{}, len(request.Selections))
	for _, selection := range request.Selections {
		selected[selection.ModelName] = struct{}{}
	}

	updatedCount, createdCount := 0, 0
	for _, item := range items {
		if _, ok := selected[item.Candidate.ModelName]; !ok {
			continue
		}
		changed, err := applyCapabilitySyncItem(item)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if !changed {
			continue
		}
		updatedCount++
		if item.Local == nil {
			createdCount++
		}
		if item.Pricing == nil {
			continue
		}
		// Update runtime pricing maps from models.dev cost data.
		// Cost values are $/1M tokens; ratio unit is $0.002/1K tokens (= $2/1M tokens).
		cost := item.Pricing.Cost
		if cost.Input > 0 {
			ratio_setting.UpdateSingleModelRatio(item.Pricing.Id, cost.Input/2.0)
		}
		if cost.Output > 0 && cost.Input > 0 {
			ratio_setting.UpdateSingleCompletionRatio(item.Pricing.Id, cost.Output/cost.Input)
		}
		if cost.CacheRead > 0 && cost.Input > 0 {
			ratio_setting.UpdateSingleCacheRatio(item.Pricing.Id, cost.CacheRead/cost.Input)
		}
	}
	if createdCount > 0 {
		model.RefreshPricing()
	}

	recordManageAudit(c, "model.models_dev.sync", map[string]any{"updated_count": updatedCount, "created_count": createdCount})
	common.ApiSuccess(c, gin.H{"updated_count": updatedCount})
}

// modelDevSyncHandler runs the periodic capability sync job.
type modelDevSyncHandler struct{}

func (modelDevSyncHandler) Type() string  { return model.SystemTaskTypeModelDevSync }
func (modelDevSyncHandler) Enabled() bool { return operation_setting.IsModelDevSyncEnabled() }
func (modelDevSyncHandler) Interval() time.Duration {
	hours := operation_setting.GetModelDevSyncIntervalHours()
	return time.Duration(hours * float64(time.Hour))
}
func (modelDevSyncHandler) NewPayload() any { return nil }

func (h modelDevSyncHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	// The provider API is the first source, so a models.dev outage must not
	// stop the run: it only removes the fallback.
	catalog, _, catalogErr := fetchModelDevCatalogWithContext(ctx)
	if catalogErr != nil {
		logger.LogWarn(ctx, "model_dev_sync: models.dev unavailable, using provider metadata only: "+catalogErr.Error())
	}
	items, err := loadCapabilitySyncPlan(catalog)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}

	updatedCount, createdCount := 0, 0
	for _, item := range items {
		changed, err := applyCapabilitySyncItem(item)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("model_dev_sync: update %s failed: %v", item.Candidate.ModelName, err))
			continue
		}
		if !changed {
			continue
		}
		updatedCount++
		if item.Local == nil {
			createdCount++
		}
	}
	if createdCount > 0 {
		model.RefreshPricing()
	}

	result := map[string]any{"updated_count": updatedCount, "created_count": createdCount}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, result, nil)
}

func init() {
	service.RegisterSystemTaskHandler(modelDevSyncHandler{})
}
