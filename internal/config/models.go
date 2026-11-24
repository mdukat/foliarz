package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/mdukat/foliarz/internal/paths"
	"github.com/mdukat/foliarz/internal/thinking"
)

//go:embed catalog.json
var builtinCatalog []byte

//go:embed defaults/models.json
var defaultModelsFile []byte

// Provider is a model provider with its resolved models.
type Provider struct {
	// ID is the provider key in models.json.
	ID string `json:"-"`

	// Name is the display name.
	Name string `json:"name,omitempty"`

	// BaseURL is the API base URL.
	BaseURL string `json:"baseUrl,omitempty"`

	// API is the API flavor used by the provider's models.
	API string `json:"api,omitempty"`

	// APIKey is the resolved API key.
	APIKey string `json:"-"`

	// Headers are request headers with resolved values.
	Headers map[string]string `json:"headers,omitempty"`

	// Compat holds provider-level compatibility settings.
	Compat map[string]any `json:"compat,omitempty"`

	// Models are the provider's models keyed by model ID.
	Models map[string]*Model `json:"-"`
}

// Model is a model after catalog, user configuration and overrides have been
// merged.
type Model struct {
	// Provider is the ID of the provider the model belongs to.
	Provider string `json:"-"`

	// ID is the model ID sent to the API.
	ID string `json:"id"`

	// Name is the display name.
	Name string `json:"name,omitempty"`

	// API overrides the provider API.
	API string `json:"api,omitempty"`

	// BaseURL overrides the provider base URL.
	BaseURL string `json:"baseUrl,omitempty"`

	// Reasoning reports whether the model supports thinking.
	Reasoning bool `json:"reasoning,omitempty"`

	// ThinkingLevelMap maps thinking levels to provider values. A nil value
	// marks the level as unsupported.
	ThinkingLevelMap map[string]*string `json:"thinkingLevelMap,omitempty"`

	// Input lists the input types the model accepts.
	Input []string `json:"input,omitempty"`

	// Cost is the per-million-token pricing.
	Cost *Cost `json:"cost,omitempty"`

	// ContextWindow is the context window size in tokens.
	ContextWindow int `json:"contextWindow,omitempty"`

	// MaxTokens is the maximum number of output tokens.
	MaxTokens int `json:"maxTokens,omitempty"`

	// Compat holds model-level compatibility settings.
	Compat map[string]any `json:"compat,omitempty"`

	// InputLimits holds model input limits.
	InputLimits map[string]any `json:"inputLimits,omitempty"`

	// SamplingParams are merged into every request body.
	SamplingParams map[string]any `json:"samplingParams,omitempty"`

	// Headers are request headers with resolved values.
	Headers map[string]string `json:"headers,omitempty"`
}

// ThinkingLevels returns the thinking levels the model supports, ordered from
// least to most reasoning. A level is supported when thinkingLevelMap maps it
// to a non-null value. A model without a thinkingLevelMap supports every level
// when it supports reasoning, and only "off" otherwise.
func (m *Model) ThinkingLevels() []string {
	if len(m.ThinkingLevelMap) == 0 {
		if !m.Reasoning {
			return []string{thinking.Off}
		}
		return thinking.Levels()
	}

	levels := make([]string, 0, len(m.ThinkingLevelMap))
	for _, level := range thinking.Levels() {
		if value, ok := m.ThinkingLevelMap[level]; ok && value != nil {
			levels = append(levels, level)
		}
	}
	return levels
}

// Cost is a model's per-million-token pricing in USD.
type Cost struct {
	Input      float64    `json:"input"`
	Output     float64    `json:"output"`
	CacheRead  float64    `json:"cacheRead"`
	CacheWrite float64    `json:"cacheWrite"`
	Tiers      []CostTier `json:"tiers,omitempty"`
}

// CostTier is a request-wide pricing tier.
type CostTier struct {
	InputTokensAbove int     `json:"inputTokensAbove"`
	Input            float64 `json:"input"`
	Output           float64 `json:"output"`
	CacheRead        float64 `json:"cacheRead"`
	CacheWrite       float64 `json:"cacheWrite"`
}

// Load loads foliarz.json and models.json, creating both (and the application
// directories) with defaults when they do not exist.
func Load(dirs paths.Dirs) (*Config, error) {
	tui, err := LoadTUI(dirs)
	if err != nil {
		return nil, err
	}
	systemPrompt, err := LoadSystemPrompt(dirs)
	if err != nil {
		return nil, err
	}
	if err := ensureFile(dirs.ModelsPath(), defaultModelsFile); err != nil {
		return nil, err
	}

	catalog, err := parseProviders(builtinCatalog, "built-in catalog")
	if err != nil {
		return nil, err
	}
	userData, err := os.ReadFile(dirs.ModelsPath())
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dirs.ModelsPath(), err)
	}
	user, err := parseProviders(userData, dirs.ModelsPath())
	if err != nil {
		return nil, err
	}

	providers, err := resolveProviders(catalog, user)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dirs.ModelsPath(), err)
	}

	return &Config{TUI: tui, SystemPrompt: systemPrompt, Providers: providers}, nil
}

// Provider returns the provider with the given ID.
func (c *Config) Provider(id string) (*Provider, bool) {
	p, ok := c.Providers[id]
	return p, ok
}

// Model returns the model with the given provider and model IDs.
func (c *Config) Model(providerID, modelID string) (*Model, bool) {
	p, ok := c.Providers[providerID]
	if !ok {
		return nil, false
	}
	m, ok := p.Models[modelID]
	return m, ok
}

// DefaultModel returns the provider and model a new session starts with: the
// last-used pair when it is still configured, otherwise the configured defaults.
func (c *Config) DefaultModel() (*Provider, *Model, error) {
	if c.TUI.LastProvider != "" || c.TUI.LastModel != "" {
		providerID := firstNonEmpty(c.TUI.LastProvider, c.TUI.DefaultProvider)
		modelID := firstNonEmpty(c.TUI.LastModel, c.TUI.DefaultModel)
		if provider, model, ok := c.lookupModel(providerID, modelID); ok {
			return provider, model, nil
		}
		// A model removed from models.json must not block startup.
		slog.Warn("last-used model is no longer configured; falling back to defaults",
			"provider", providerID, "model", modelID)
	}

	providerID := c.TUI.DefaultProvider
	modelID := c.TUI.DefaultModel
	provider, model, ok := c.lookupModel(providerID, modelID)
	if ok {
		return provider, model, nil
	}
	switch {
	case providerID == "":
		return nil, nil, fmt.Errorf("defaultProvider is not set")
	case c.Providers[providerID] == nil:
		return nil, nil, fmt.Errorf("default provider %q is not configured", providerID)
	case modelID == "":
		return nil, nil, fmt.Errorf("defaultModel is not set")
	default:
		return nil, nil, fmt.Errorf("default model %q is not configured for provider %q", modelID, providerID)
	}
}

// lookupModel resolves a provider and model pair.
func (c *Config) lookupModel(providerID, modelID string) (*Provider, *Model, bool) {
	provider, ok := c.Providers[providerID]
	if !ok || modelID == "" {
		return nil, nil, false
	}
	model, ok := provider.Models[modelID]
	if !ok {
		return nil, nil, false
	}
	return provider, model, true
}

// rawProvider is a provider as it appears in a models JSON document.
type rawProvider struct {
	Name           string                     `json:"name,omitempty"`
	BaseURL        string                     `json:"baseUrl,omitempty"`
	API            string                     `json:"api,omitempty"`
	APIKey         string                     `json:"apiKey,omitempty"`
	Headers        map[string]string          `json:"headers,omitempty"`
	Compat         map[string]any             `json:"compat,omitempty"`
	Models         []json.RawMessage          `json:"models,omitempty"`
	ModelOverrides map[string]json.RawMessage `json:"modelOverrides,omitempty"`
}

func parseProviders(data []byte, source string) (map[string]rawProvider, error) {
	var doc struct {
		Providers map[string]rawProvider `json:"providers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", source, err)
	}
	return doc.Providers, nil
}

// resolveProviders combines the built-in catalog with the user's models
// configuration and applies model overrides.
func resolveProviders(catalog, user map[string]rawProvider) (map[string]*Provider, error) {
	ids := make([]string, 0, len(catalog)+len(user))
	seen := make(map[string]bool, len(catalog)+len(user))
	for id := range catalog {
		ids = append(ids, id)
		seen[id] = true
	}
	for id := range user {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	providers := make(map[string]*Provider, len(ids))
	for _, id := range ids {
		provider, err := resolveProvider(id, catalog[id], user[id])
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", id, err)
		}
		providers[id] = provider
	}
	return providers, nil
}

func resolveProvider(id string, base, user rawProvider) (*Provider, error) {
	rawAPIKey := firstNonEmpty(user.APIKey, base.APIKey)
	provider := &Provider{
		ID:      id,
		Name:    firstNonEmpty(user.Name, base.Name),
		BaseURL: firstNonEmpty(user.BaseURL, base.BaseURL),
		API:     firstNonEmpty(user.API, base.API),
		APIKey:  resolveSecret(rawAPIKey),
		Headers: resolveHeaders(mergeHeaders(base.Headers, user.Headers)),
		Compat:  mergeObjects(base.Compat, user.Compat),
		Models:  make(map[string]*Model),
	}
	if provider.APIKey == "" && strings.HasPrefix(rawAPIKey, "$") {
		slog.Warn("provider API key environment variable is not set",
			"provider", id, "variable", strings.TrimPrefix(rawAPIKey, "$"))
	}

	models, err := collectModels(base.Models, user.Models)
	if err != nil {
		return nil, err
	}
	overrides, err := collectOverrides(base.ModelOverrides, user.ModelOverrides)
	if err != nil {
		return nil, err
	}
	for modelID, override := range overrides {
		model, ok := models[modelID]
		if !ok {
			// Unknown model IDs are ignored, like in Pi.
			continue
		}
		models[modelID] = mergeObjects(model, override)
	}

	for modelID, doc := range models {
		model, err := decodeModel(provider, modelID, doc)
		if err != nil {
			return nil, err
		}
		provider.Models[model.ID] = model
	}
	return provider, nil
}

func collectModels(sources ...[]json.RawMessage) (map[string]map[string]any, error) {
	models := make(map[string]map[string]any)
	for _, docs := range sources {
		for _, raw := range docs {
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				return nil, fmt.Errorf("parse model: %w", err)
			}
			id, _ := doc["id"].(string)
			if id == "" {
				return nil, fmt.Errorf("model is missing id")
			}
			models[id] = doc
		}
	}
	return models, nil
}

func collectOverrides(sources ...map[string]json.RawMessage) (map[string]map[string]any, error) {
	overrides := make(map[string]map[string]any)
	for _, docs := range sources {
		for id, raw := range docs {
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				return nil, fmt.Errorf("parse override for %q: %w", id, err)
			}
			if existing, ok := overrides[id]; ok {
				overrides[id] = mergeObjects(existing, doc)
				continue
			}
			overrides[id] = doc
		}
	}
	return overrides, nil
}

func decodeModel(provider *Provider, modelID string, doc map[string]any) (*Model, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("model %q: %w", modelID, err)
	}

	var model Model
	if err := json.Unmarshal(raw, &model); err != nil {
		return nil, fmt.Errorf("model %q: %w", modelID, err)
	}
	if model.ID != modelID {
		return nil, fmt.Errorf("model %q declares mismatched id %q", modelID, model.ID)
	}
	model.Provider = provider.ID
	model.Headers = resolveHeaders(model.Headers)
	model.Compat = mergeObjects(provider.Compat, model.Compat)
	return &model, nil
}

// mergeObjects merges override into base. Object fields merge field by field;
// all other values are replaced.
func mergeObjects(base, override map[string]any) map[string]any {
	if base == nil && override == nil {
		return nil
	}
	out := make(map[string]any, len(base)+len(override))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		if overrideObject, ok := v.(map[string]any); ok {
			if baseObject, ok := out[k].(map[string]any); ok {
				out[k] = mergeObjects(baseObject, overrideObject)
				continue
			}
		}
		out[k] = v
	}
	return out
}

func mergeHeaders(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	merged := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range override {
		merged[k] = v
	}
	return merged
}

func resolveHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	resolved := make(map[string]string, len(headers))
	for k, v := range headers {
		resolved[k] = resolveSecret(v)
	}
	return resolved
}

// resolveSecret expands "$NAME" to the value of the NAME environment variable.
func resolveSecret(value string) string {
	if strings.HasPrefix(value, "$") && len(value) > 1 {
		return os.Getenv(value[1:])
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
