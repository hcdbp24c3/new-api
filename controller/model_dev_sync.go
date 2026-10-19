package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"sort"
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

// loadChannelModelMetadata asks each channel that serves a synced model for its
// own metadata, so capability fields resolve from the provider that actually
// serves the model instead of a third-party catalog. Channels are visited once
// each and their results are cached for the whole sync; a provider that errors
// is skipped so its models fall back to models.dev.
func loadChannelModelMetadata(models []map[string]*model.Model) map[string]upstreamModelMetadata {
	merged := make(map[string]upstreamModelMetadata)

	var channels []model.Channel
	if err := model.DB.Find(&channels).Error; err != nil {
		logger.LogWarn(context.Background(), "model metadata sync: read channels failed: "+err.Error())
		return merged
	}

	// A model name reaches a provider either bare or behind that channel's
	// model_prefix, so index both spellings before matching.
	wanted := make(map[string]struct{})
	for _, byName := range models {
		for name := range byName {
			wanted[name] = struct{}{}
		}
	}

	visited := make(map[string]struct{}, len(channels))
	for _, channel := range channels {
		if _, done := visited[fmt.Sprintf("%d", channel.Id)]; done {
			continue
		}
		visited[fmt.Sprintf("%d", channel.Id)] = struct{}{}
		if !channelServesAnyModel(&channel, wanted) {
			continue
		}

		metadata, err := fetchChannelUpstreamModelMetadata(&channel)
		if err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf(
				"model metadata sync: channel %d metadata unavailable, falling back to models.dev: %s",
				channel.Id, err.Error()))
			continue
		}
		mergeUpstreamModelMetadata(merged, metadata, model.ExtractModelPrefix(channel.OtherSettings))
	}
	return merged
}

// channelServesAnyModel reports whether the channel lists any model the sync is
// about to touch, so a deployment does not query every provider on every run.
func channelServesAnyModel(channel *model.Channel, wanted map[string]struct{}) bool {
	prefix := model.ExtractModelPrefix(channel.OtherSettings)
	for _, name := range channel.GetModels() {
		if _, exists := wanted[name]; exists {
			return true
		}
		if prefix != "" {
			if _, exists := wanted[prefix+"/"+name]; exists {
				return true
			}
		}
	}
	return false
}

// mergeUpstreamModelMetadata folds one channel's result into the shared map.
// The first channel that reports a field for a model wins, so two providers
// offering the same model cannot fight over it during one sync.
func mergeUpstreamModelMetadata(merged, metadata map[string]upstreamModelMetadata, prefix string) {
	for name, entry := range metadata {
		key := name
		if prefix != "" {
			key = prefix + "/" + name
		}
		if _, exists := merged[key]; exists {
			continue
		}
		merged[key] = entry
	}
}

// SyncModelsDevPreview returns candidates with diffs from models.dev catalog.
func SyncModelsDevPreview(c *gin.Context) {
	catalog, version, err := fetchModelDevCatalog(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var allModels []*model.Model
	if err := model.DB.Find(&allModels).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	var allVendors []*model.Vendor
	if err := model.DB.Find(&allVendors).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	vendorByName := make(map[string]*model.Vendor, len(allVendors))
	for _, v := range allVendors {
		vendorByName[strings.ToLower(v.Name)] = v
	}

	modelByName := make(map[string]*model.Model, len(allModels))
	for _, m := range allModels {
		modelByName[m.ModelName] = m
	}

	// Build a prefix-aware lookup: bare models.dev IDs → channel prefixed names.
	// When a channel has model_prefix "deepseek", its models are stored as
	// "deepseek/deepseek-flash" in the DB but models.dev catalog uses "deepseek-flash".
	// We collect all channel prefixes and build a lookup that tries both variants.
	var syncChannels []model.Channel
	_ = model.DB.Select("settings").Find(&syncChannels)
	channelPrefixSet := make(map[string]struct{})
	for _, ch := range syncChannels {
		if p := model.ExtractModelPrefix(ch.OtherSettings); p != "" {
			channelPrefixSet[p+"/"] = struct{}{}
		}
	}

	// resolveLocalModel looks up a models.dev bare ID in the model map,
	// also trying each known channel prefix to find the prefixed DB entry.
	resolveLocalModel := func(devID string) (*model.Model, bool) {
		if local, ok := modelByName[devID]; ok {
			return local, true
		}
		for prefix := range channelPrefixSet {
			if prefixed, ok := modelByName[prefix+devID]; ok {
				return prefixed, true
			}
		}
		return nil, false
	}

	candidates := make([]modelsDevSyncCandidate, 0)
	upstreamMetadata := loadChannelModelMetadata([]map[string]*model.Model{modelByName})
	for providerKey, provider := range catalog.Providers {
		vendorFound := false
		if _, ok := vendorByName[strings.ToLower(provider.Name)]; ok {
			vendorFound = true
		}

		for _, devModel := range provider.Models {
			local, modelFound := resolveLocalModel(devModel.Id)
			if !modelFound {
				continue
			}

			candidate := modelsDevSyncCandidate{
				ModelName: devModel.Id,
				Provider:  providerKey,
				Kind:      "unchanged",
				Fields:    []modelsDevSyncField{},
			}

			if !vendorFound {
				candidate.Kind = "missing_vendor"
				candidates = append(candidates, candidate)
				continue
			}

			fields := buildModelDevCapabilityFields(local, devModel, upstreamMetadata[local.ModelName])
			if len(fields) > 0 {
				candidate.Kind = "update"
				candidate.Fields = fields
			}

			candidates = append(candidates, candidate)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Provider != candidates[j].Provider {
			return candidates[i].Provider < candidates[j].Provider
		}
		return candidates[i].ModelName < candidates[j].ModelName
	})

	common.ApiSuccess(c, gin.H{
		"source":     modelsDevSyncSource{URL: modelDevSyncURL, Version: version},
		"candidates": candidates,
	})
}

// modelCapabilitySource is the merged view of one model's capabilities: the
// provider's own API first, then the models.dev catalog for whatever the
// provider did not report. The provider is authoritative because it describes
// the model this gateway actually serves; models.dev is a third-party catalog
// that covers a model across every provider offering it.
type modelCapabilitySource struct {
	ContextLength   int
	MaxOutputTokens int
	Reasoning       bool
	ToolCall        bool
	ToolCallKnown   bool
}

// resolveModelCapability prefers the provider's metadata and falls back to
// models.dev per field. A provider that reports nothing usable, which is the
// common case for OpenAI-compatible channels, leaves the whole capability set
// to models.dev.
func resolveModelCapability(provider upstreamModelMetadata, dev modelDevModel) modelCapabilitySource {
	source := modelCapabilitySource{
		ContextLength:   dev.Limit.Context,
		MaxOutputTokens: dev.Limit.Output,
		Reasoning:       dev.Reasoning,
		ToolCall:        dev.ToolCall,
		ToolCallKnown:   true,
	}

	if provider.ContextLength > 0 {
		source.ContextLength = provider.ContextLength
	}
	if provider.MaxOutputTokens > 0 {
		source.MaxOutputTokens = provider.MaxOutputTokens
	}
	if provider.ToolCall != nil {
		source.ToolCall = *provider.ToolCall
	}
	return source
}

// buildModelDevCapabilityFields returns field diffs between a local model and
// the merged provider/catalog capabilities.
func buildModelDevCapabilityFields(local *model.Model, dev modelDevModel, provider upstreamModelMetadata) []modelsDevSyncField {
	var fields []modelsDevSyncField
	capability := resolveModelCapability(provider, dev)

	if capability.ContextLength > 0 && local.ContextLength != capability.ContextLength {
		fields = append(fields, modelsDevSyncField{Field: "context_length", Local: local.ContextLength, Upstream: capability.ContextLength})
	}
	if capability.MaxOutputTokens > 0 && local.MaxOutputTokens != capability.MaxOutputTokens {
		fields = append(fields, modelsDevSyncField{Field: "max_output_tokens", Local: local.MaxOutputTokens, Upstream: capability.MaxOutputTokens})
	}
	if local.Reasoning != capability.Reasoning {
		fields = append(fields, modelsDevSyncField{Field: "reasoning", Local: local.Reasoning, Upstream: capability.Reasoning})
	}
	if capability.ToolCallKnown && local.ToolCall != capability.ToolCall {
		fields = append(fields, modelsDevSyncField{Field: "tool_call", Local: local.ToolCall, Upstream: capability.ToolCall})
	}
	if dev.Cost.Input > 0 && local.PricingInput != dev.Cost.Input {
		fields = append(fields, modelsDevSyncField{Field: "pricing_input", Local: local.PricingInput, Upstream: dev.Cost.Input})
	}
	if dev.Cost.Output > 0 && local.PricingOutput != dev.Cost.Output {
		fields = append(fields, modelsDevSyncField{Field: "pricing_output", Local: local.PricingOutput, Upstream: dev.Cost.Output})
	}
	if dev.Cost.CacheRead > 0 && local.PricingCache != dev.Cost.CacheRead {
		fields = append(fields, modelsDevSyncField{Field: "pricing_cache", Local: local.PricingCache, Upstream: dev.Cost.CacheRead})
	}

	return fields
}

// SyncModelsDevApply applies selected model capability updates from models.dev.
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

	var allModels []*model.Model
	if err := model.DB.Find(&allModels).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var allVendors []*model.Vendor
	if err := model.DB.Find(&allVendors).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	vendorByName := make(map[string]*model.Vendor, len(allVendors))
	for _, v := range allVendors {
		vendorByName[strings.ToLower(v.Name)] = v
	}
	modelByName := make(map[string]*model.Model, len(allModels))
	for _, m := range allModels {
		modelByName[m.ModelName] = m
	}

	// Collect channel prefixes for prefix-aware model lookup (same as preview).
	var syncChannels []model.Channel
	_ = model.DB.Select("settings").Find(&syncChannels)
	channelPrefixSet := make(map[string]struct{})
	for _, ch := range syncChannels {
		if p := model.ExtractModelPrefix(ch.OtherSettings); p != "" {
			channelPrefixSet[p+"/"] = struct{}{}
		}
	}

	updatedCount := 0
	upstreamMetadata := loadChannelModelMetadata([]map[string]*model.Model{modelByName})
	for _, selection := range request.Selections {
		provider, providerExists := catalog.Providers[selection.Provider]
		if !providerExists {
			continue
		}
		devModel, modelExists := provider.Models[selection.ModelName]
		if !modelExists {
			continue
		}
		local, localExists := modelByName[selection.ModelName]
		if !localExists {
			// Try with channel prefix stripped (models.dev bare ID + prefix = DB name).
			for prefix := range channelPrefixSet {
				if prefixed, ok := modelByName[prefix+selection.ModelName]; ok {
					local = prefixed
					localExists = true
					break
				}
			}
		}
		if !localExists {
			continue
		}

		if _, vendorExists := vendorByName[strings.ToLower(provider.Name)]; !vendorExists {
			continue
		}

		capability := resolveModelCapability(upstreamMetadata[local.ModelName], devModel)
		updates := map[string]any{}
		if capability.ContextLength > 0 && local.ContextLength != capability.ContextLength {
			updates["context_length"] = capability.ContextLength
		}
		if capability.MaxOutputTokens > 0 && local.MaxOutputTokens != capability.MaxOutputTokens {
			updates["max_output_tokens"] = capability.MaxOutputTokens
		}
		if local.Reasoning != capability.Reasoning {
			updates["reasoning"] = capability.Reasoning
		}
		if capability.ToolCallKnown && local.ToolCall != capability.ToolCall {
			updates["tool_call"] = capability.ToolCall
		}
		if devModel.Cost.Input > 0 && local.PricingInput != devModel.Cost.Input {
			updates["pricing_input"] = devModel.Cost.Input
		}
		if devModel.Cost.Output > 0 && local.PricingOutput != devModel.Cost.Output {
			updates["pricing_output"] = devModel.Cost.Output
		}
		if devModel.Cost.CacheRead > 0 && local.PricingCache != devModel.Cost.CacheRead {
			updates["pricing_cache"] = devModel.Cost.CacheRead
		}

		if len(updates) > 0 {
			updates["updated_time"] = common.GetTimestamp()
			if err := model.DB.Model(&model.Model{}).Where("id = ?", local.Id).Updates(updates).Error; err != nil {
				common.ApiError(c, err)
				return
			}
			updatedCount++

			// Update runtime pricing maps from models.dev cost data.
			// Cost values are $/1M tokens; ratio unit is $0.002/1K tokens (= $2/1M tokens).
			if devModel.Cost.Input > 0 {
				ratio_setting.UpdateSingleModelRatio(devModel.Id, devModel.Cost.Input/2.0)
			}
			if devModel.Cost.Output > 0 && devModel.Cost.Input > 0 {
				ratio_setting.UpdateSingleCompletionRatio(devModel.Id, devModel.Cost.Output/devModel.Cost.Input)
			}
			if devModel.Cost.CacheRead > 0 && devModel.Cost.Input > 0 {
				ratio_setting.UpdateSingleCacheRatio(devModel.Id, devModel.Cost.CacheRead/devModel.Cost.Input)
			}
		}
	}

	recordManageAudit(c, "model.models_dev.sync", map[string]any{"updated_count": updatedCount})
	common.ApiSuccess(c, gin.H{"updated_count": updatedCount})
}

// modelDevSyncHandler runs the periodic models.dev catalog sync job.
type modelDevSyncHandler struct{}

func (modelDevSyncHandler) Type() string  { return model.SystemTaskTypeModelDevSync }
func (modelDevSyncHandler) Enabled() bool { return operation_setting.IsModelDevSyncEnabled() }
func (modelDevSyncHandler) Interval() time.Duration {
	hours := operation_setting.GetModelDevSyncIntervalHours()
	return time.Duration(hours * float64(time.Hour))
}
func (modelDevSyncHandler) NewPayload() any { return nil }

func (h modelDevSyncHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	catalog, _, err := fetchModelDevCatalogWithContext(ctx)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}

	var allModels []*model.Model
	if err := model.DB.Find(&allModels).Error; err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	var allVendors []*model.Vendor
	if err := model.DB.Find(&allVendors).Error; err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}

	vendorByName := make(map[string]*model.Vendor, len(allVendors))
	for _, v := range allVendors {
		vendorByName[strings.ToLower(v.Name)] = v
	}
	modelByName := make(map[string]*model.Model, len(allModels))
	for _, m := range allModels {
		modelByName[m.ModelName] = m
	}

	updatedCount := 0
	upstreamMetadata := loadChannelModelMetadata([]map[string]*model.Model{modelByName})
	for _, provider := range catalog.Providers {
		if _, vendorExists := vendorByName[strings.ToLower(provider.Name)]; !vendorExists {
			continue
		}
		for _, devModel := range provider.Models {
			local, modelExists := modelByName[devModel.Id]
			if !modelExists {
				continue
			}

			capability := resolveModelCapability(upstreamMetadata[local.ModelName], devModel)
			updates := map[string]any{}
			if capability.ContextLength > 0 && local.ContextLength != capability.ContextLength {
				updates["context_length"] = capability.ContextLength
			}
			if capability.MaxOutputTokens > 0 && local.MaxOutputTokens != capability.MaxOutputTokens {
				updates["max_output_tokens"] = capability.MaxOutputTokens
			}
			if local.Reasoning != capability.Reasoning {
				updates["reasoning"] = capability.Reasoning
			}
			if capability.ToolCallKnown && local.ToolCall != capability.ToolCall {
				updates["tool_call"] = capability.ToolCall
			}
			if devModel.Cost.Input > 0 && local.PricingInput != devModel.Cost.Input {
				updates["pricing_input"] = devModel.Cost.Input
			}
			if devModel.Cost.Output > 0 && local.PricingOutput != devModel.Cost.Output {
				updates["pricing_output"] = devModel.Cost.Output
			}
			if devModel.Cost.CacheRead > 0 && local.PricingCache != devModel.Cost.CacheRead {
				updates["pricing_cache"] = devModel.Cost.CacheRead
			}

			if len(updates) > 0 {
				updates["updated_time"] = common.GetTimestamp()
				if err := model.DB.Model(&model.Model{}).Where("id = ?", local.Id).Updates(updates).Error; err != nil {
					logger.LogWarn(ctx, fmt.Sprintf("model_dev_sync: update %s failed: %v", devModel.Id, err))
					continue
				}
				updatedCount++
			}
		}
	}

	result := map[string]any{"updated_count": updatedCount}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, result, nil)
}

func init() {
	service.RegisterSystemTaskHandler(modelDevSyncHandler{})
}
