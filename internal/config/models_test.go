package config

import (
	"encoding/json"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/mdukat/foliarz/internal/paths"
	"github.com/mdukat/foliarz/internal/thinking"
)

func writeModelsConfig(t *testing.T, dirs paths.Dirs, content string) {
	t.Helper()
	if err := os.WriteFile(dirs.ModelsPath(), []byte(content), 0o600); err != nil {
		t.Fatalf("write models config: %v", err)
	}
}

func TestLoadCreatesDefaultConfigs(t *testing.T) {
	dirs := testDirs(t)
	t.Setenv("OPENROUTER_API_KEY", "secret-key")

	cfg, err := Load(dirs)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.TUI.DefaultProvider != "openrouter" {
		t.Errorf("DefaultProvider = %q, want openrouter", cfg.TUI.DefaultProvider)
	}

	provider, model, err := cfg.DefaultModel()
	if err != nil {
		t.Fatalf("DefaultModel() error = %v", err)
	}
	if provider.ID != "openrouter" {
		t.Errorf("provider ID = %q, want openrouter", provider.ID)
	}
	if provider.APIKey != "secret-key" {
		t.Errorf("provider APIKey = %q, want resolved environment value", provider.APIKey)
	}
	if provider.BaseURL != "https://openrouter.ai/api/v1" {
		t.Errorf("provider BaseURL = %q", provider.BaseURL)
	}
	if model.ID != "deepseek/deepseek-v4.1-flash" {
		t.Errorf("model ID = %q", model.ID)
	}
	if model.Provider != "openrouter" {
		t.Errorf("model Provider = %q, want openrouter", model.Provider)
	}
	if model.ContextWindow != 1048576 {
		t.Errorf("ContextWindow = %d, want 1048576", model.ContextWindow)
	}
	if !model.Reasoning {
		t.Error("Reasoning = false, want true")
	}

	// The default models config overrides the model's OpenRouter routing,
	// exactly like the Pi configuration.
	only, _ := routingValue(model, "only").([]any)
	if len(only) != 1 || only[0] != "alibaba" {
		t.Errorf("compat.openRouterRouting.only = %v, want [alibaba]", only)
	}
	if got := routingValue(model, "allow_fallbacks"); got != false {
		t.Errorf("compat.openRouterRouting.allow_fallbacks = %v, want false", got)
	}
	// The catalog's own compat fields survive the deep merge.
	if got := model.Compat["thinkingFormat"]; got != "openrouter" {
		t.Errorf("compat.thinkingFormat = %v, want openrouter", got)
	}
}

func routingValue(model *Model, key string) any {
	routing, _ := model.Compat["openRouterRouting"].(map[string]any)
	return routing[key]
}

func TestLoadMergesOverrideFields(t *testing.T) {
	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	writeModelsConfig(t, dirs, `{
	  "providers": {
	    "openrouter": {
	      "modelOverrides": {
	        "deepseek/deepseek-v4.1-flash": {
	          "name": "Custom Name",
	          "contextWindow": 64000,
	          "cost": { "input": 1.5 },
	          "compat": {
	            "openRouterRouting": { "only": ["alibaba"], "allow_fallbacks": false }
	          }
	        }
	      }
	    }
	  }
	}`)

	cfg, err := Load(dirs)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	model, ok := cfg.Model("openrouter", "deepseek/deepseek-v4.1-flash")
	if !ok {
		t.Fatal("model not found")
	}
	if model.Name != "Custom Name" {
		t.Errorf("Name = %q, want Custom Name", model.Name)
	}
	if model.ContextWindow != 64000 {
		t.Errorf("ContextWindow = %d, want 64000", model.ContextWindow)
	}
	if model.Cost == nil {
		t.Fatal("Cost = nil")
	}
	if model.Cost.Input != 1.5 {
		t.Errorf("Cost.Input = %v, want 1.5", model.Cost.Input)
	}
	if model.Cost.Output != 0.2028 {
		t.Errorf("Cost.Output = %v, want catalog value 0.2028", model.Cost.Output)
	}
}

func TestLoadAddsUserModel(t *testing.T) {
	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	writeModelsConfig(t, dirs, `{
	  "providers": {
	    "openrouter": {
	      "models": [
	        {
	          "id": "custom/model",
	          "name": "Custom Model",
	          "api": "openai-completions",
	          "baseUrl": "https://openrouter.ai/api/v1",
	          "contextWindow": 8192
	        }
	      ]
	    }
	  }
	}`)

	cfg, err := Load(dirs)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	model, ok := cfg.Model("openrouter", "custom/model")
	if !ok {
		t.Fatal("user model not found")
	}
	if model.Name != "Custom Model" || model.ContextWindow != 8192 {
		t.Errorf("user model = %+v", model)
	}
	if _, ok := cfg.Model("openrouter", "deepseek/deepseek-v4.1-flash"); !ok {
		t.Error("catalog model disappeared after adding a user model")
	}
}

func TestLoadIgnoresUnknownOverride(t *testing.T) {
	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	writeModelsConfig(t, dirs, `{
	  "providers": {
	    "openrouter": {
	      "modelOverrides": {
	        "does/not-exist": { "name": "Nope" }
	      }
	    }
	  }
	}`)

	if _, err := Load(dirs); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadInvalidModelsJSON(t *testing.T) {
	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	writeModelsConfig(t, dirs, `{"providers": {`)

	_, err := Load(dirs)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "models.json") {
		t.Errorf("error %q does not mention models.json", err)
	}
}

func TestLoadMissingDefaultModel(t *testing.T) {
	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if err := os.WriteFile(dirs.ConfigPath(), []byte(`{
	  "defaultProvider": "openrouter",
	  "defaultModel": "missing/model",
	  "thinkingLevel": "off"
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	writeModelsConfig(t, dirs, `{"providers": {}}`)

	cfg, err := Load(dirs)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, _, err := cfg.DefaultModel(); err == nil {
		t.Fatal("DefaultModel() error = nil, want error")
	}
}

func TestDefaultModelThinkingLevels(t *testing.T) {
	dirs := testDirs(t)
	cfg, err := Load(dirs)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	_, model, err := cfg.DefaultModel()
	if err != nil {
		t.Fatalf("DefaultModel() error = %v", err)
	}

	// The model's thinkingLevelMap maps minimal, medium and xhigh to null.
	want := []string{thinking.Off, thinking.Low, thinking.High, thinking.Max}
	if got := model.ThinkingLevels(); !reflect.DeepEqual(got, want) {
		t.Errorf("ThinkingLevels() = %v, want %v", got, want)
	}
}

func TestThinkingLevelsWithoutMap(t *testing.T) {
	reasoning := &Model{Reasoning: true}
	if got := reasoning.ThinkingLevels(); !reflect.DeepEqual(got, thinking.Levels()) {
		t.Errorf("reasoning model ThinkingLevels() = %v, want all levels", got)
	}

	plain := &Model{Reasoning: false}
	if got := plain.ThinkingLevels(); !reflect.DeepEqual(got, []string{thinking.Off}) {
		t.Errorf("non-reasoning model ThinkingLevels() = %v, want [off]", got)
	}
}

func TestActiveSelectionUsesLastValues(t *testing.T) {
	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	config := `{
	  "defaultProvider": "openrouter",
	  "defaultModel": "deepseek/deepseek-v4.1-flash",
	  "thinkingLevel": "off",
	  "lastProvider": "openrouter",
	  "lastModel": "deepseek/deepseek-v4.1-flash",
	  "lastThinkingLevel": "high"
	}`
	if err := os.WriteFile(dirs.ConfigPath(), []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(dirs)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.TUI.ActiveThinkingLevel(); got != thinking.High {
		t.Errorf("ActiveThinkingLevel() = %q, want high", got)
	}
	_, model, err := cfg.DefaultModel()
	if err != nil {
		t.Fatalf("DefaultModel() error = %v", err)
	}
	if model.ID != "deepseek/deepseek-v4.1-flash" {
		t.Errorf("model = %q", model.ID)
	}
}

func TestSaveLastUsedPreservesOtherFields(t *testing.T) {
	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	original := `{
	  "defaultProvider": "openrouter",
	  "defaultModel": "deepseek/deepseek-v4.1-flash",
	  "thinkingLevel": "off",
	  "customSetting": true
	}`
	if err := os.WriteFile(dirs.ConfigPath(), []byte(original), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := SaveLastUsed(dirs, "openrouter", "deepseek/deepseek-v4.1-flash", "max"); err != nil {
		t.Fatalf("SaveLastUsed() error = %v", err)
	}

	data, err := os.ReadFile(dirs.ConfigPath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if doc["customSetting"] != true {
		t.Errorf("customSetting = %v, want true", doc["customSetting"])
	}
	if doc["lastThinkingLevel"] != "max" {
		t.Errorf("lastThinkingLevel = %v", doc["lastThinkingLevel"])
	}
	if doc["lastProvider"] != "openrouter" || doc["lastModel"] != "deepseek/deepseek-v4.1-flash" {
		t.Errorf("last model fields = %v / %v", doc["lastProvider"], doc["lastModel"])
	}

	info, err := os.Stat(dirs.ConfigPath())
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config permissions = %o, want 600", perm)
	}
}

func TestInvalidLastThinkingLevelIsIgnored(t *testing.T) {
	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if err := os.WriteFile(dirs.ConfigPath(), []byte(`{"thinkingLevel": "off", "lastThinkingLevel": "turbo"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	tui, err := LoadTUI(dirs)
	if err != nil {
		t.Fatalf("LoadTUI() error = %v", err)
	}
	if got := tui.ActiveThinkingLevel(); got != thinking.Off {
		t.Errorf("ActiveThinkingLevel() = %q, want off", got)
	}
}

func TestProviderCompatMergesIntoModels(t *testing.T) {
	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatal(err)
	}
	writeModelsConfig(t, dirs, `{
	  "providers": {
	    "openrouter": {
	      "compat": {
	        "providerOnly": "yes",
	        "supportsStrictMode": true
	      },
	      "modelOverrides": {
	        "deepseek/deepseek-v4.1-flash": {
	          "compat": { "supportsStrictMode": false }
	        }
	      }
	    }
	  }
	}`)

	cfg, err := Load(dirs)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	model, ok := cfg.Model("openrouter", "deepseek/deepseek-v4.1-flash")
	if !ok {
		t.Fatal("model not found")
	}
	if model.Compat["providerOnly"] != "yes" {
		t.Errorf("providerOnly = %v, want provider compat value", model.Compat["providerOnly"])
	}
	if model.Compat["thinkingFormat"] != "openrouter" {
		t.Errorf("thinkingFormat = %v, want model value to win", model.Compat["thinkingFormat"])
	}
	if model.Compat["supportsStrictMode"] != false {
		t.Errorf("supportsStrictMode = %v, want model override value", model.Compat["supportsStrictMode"])
	}
}

func TestBuiltinCatalogIncludesFlash1017(t *testing.T) {
	dirs := testDirs(t)
	cfg, err := Load(dirs)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	model, ok := cfg.Model("openrouter", "deepseek/deepseek-v4.1-flash-1017")
	if !ok {
		t.Fatal("deepseek/deepseek-v4.1-flash-1017 is missing from the built-in catalog")
	}
	if model.Name != "DeepSeek: DeepSeek V4.1 Flash 1017" {
		t.Errorf("Name = %q", model.Name)
	}
	if model.ContextWindow != 1048576 {
		t.Errorf("ContextWindow = %d", model.ContextWindow)
	}
	if model.MaxTokens != 393216 {
		t.Errorf("MaxTokens = %d", model.MaxTokens)
	}
	if !model.Reasoning {
		t.Error("Reasoning = false")
	}
	if model.Cost == nil || model.Cost.Input != 0.03 || model.Cost.Output != 0.2 {
		t.Errorf("Cost = %+v", model.Cost)
	}
}

func TestUnsetInterpolatedAPIKeyLogsWarning(t *testing.T) {
	var logged strings.Builder
	original := slog.Default()
	t.Cleanup(func() { slog.SetDefault(original) })
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))

	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatal(err)
	}
	writeModelsConfig(t, dirs, `{"providers": {"openrouter": {"apiKey": "$FOLIARZ_TEST_MISSING_KEY"}}}`)
	t.Setenv("FOLIARZ_TEST_MISSING_KEY", "")

	if _, err := Load(dirs); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !strings.Contains(logged.String(), "FOLIARZ_TEST_MISSING_KEY") {
		t.Errorf("log = %q, want a warning naming the variable", logged.String())
	}
}

func TestStaleLastModelFallsBackToDefault(t *testing.T) {
	tests := []struct {
		name         string
		lastProvider string
		lastModel    string
	}{
		{"removed model", "openrouter", "removed/model"},
		{"removed provider", "removed-provider", "deepseek/deepseek-v4.1-flash"},
		{"both removed", "removed-provider", "removed/model"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dirs := testDirs(t)
			if err := dirs.Ensure(); err != nil {
				t.Fatal(err)
			}
			config := `{
			  "defaultProvider": "openrouter",
			  "defaultModel": "deepseek/deepseek-v4.1-flash",
			  "thinkingLevel": "off",
			  "lastProvider": "` + test.lastProvider + `",
			  "lastModel": "` + test.lastModel + `"
			}`
			if err := os.WriteFile(dirs.ConfigPath(), []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}

			cfg, err := Load(dirs)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			provider, model, err := cfg.DefaultModel()
			if err != nil {
				t.Fatalf("DefaultModel() error = %v", err)
			}
			if provider.ID != "openrouter" || model.ID != "deepseek/deepseek-v4.1-flash" {
				t.Errorf("resolved %q/%q, want the defaults", provider.ID, model.ID)
			}
		})
	}
}

func TestStaleDefaultsStillFail(t *testing.T) {
	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatal(err)
	}
	config := `{
	  "defaultProvider": "openrouter",
	  "defaultModel": "removed/model",
	  "thinkingLevel": "off"
	}`
	if err := os.WriteFile(dirs.ConfigPath(), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dirs)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, _, err := cfg.DefaultModel(); err == nil {
		t.Fatal("DefaultModel() error = nil, want error for missing default model")
	}
}
