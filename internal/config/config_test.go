package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdukat/foliarz/internal/paths"
	"github.com/mdukat/foliarz/internal/thinking"
)

func testDirs(t *testing.T) paths.Dirs {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	dirs, err := paths.Resolve()
	if err != nil {
		t.Fatalf("paths.Resolve() error = %v", err)
	}
	return dirs
}

func TestLoadTUICreatesDefaults(t *testing.T) {
	dirs := testDirs(t)

	tui, err := LoadTUI(dirs)
	if err != nil {
		t.Fatalf("LoadTUI() error = %v", err)
	}

	if tui.DefaultProvider != "openrouter" {
		t.Errorf("DefaultProvider = %q, want openrouter", tui.DefaultProvider)
	}
	if tui.DefaultModel != "deepseek/deepseek-v4.1-flash" {
		t.Errorf("DefaultModel = %q, want deepseek/deepseek-v4.1-flash", tui.DefaultModel)
	}
	if tui.ThinkingLevel != thinking.Off {
		t.Errorf("ThinkingLevel = %q, want off", tui.ThinkingLevel)
	}
	if tui.HideThinkingBlock {
		t.Error("HideThinkingBlock = true, want false")
	}

	info, err := os.Stat(dirs.ConfigPath())
	if err != nil {
		t.Fatalf("stat config file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file permissions = %o, want 600", perm)
	}
}

func TestLoadTUIReadsExistingFile(t *testing.T) {
	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	custom := `{
  "defaultProvider": "openrouter",
  "defaultModel": "deepseek/deepseek-v4.1-flash",
  "thinkingLevel": "high",
  "hideThinkingBlock": true
}`
	if err := os.WriteFile(dirs.ConfigPath(), []byte(custom), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	tui, err := LoadTUI(dirs)
	if err != nil {
		t.Fatalf("LoadTUI() error = %v", err)
	}
	if tui.ThinkingLevel != thinking.High {
		t.Errorf("ThinkingLevel = %q, want high", tui.ThinkingLevel)
	}
	if !tui.HideThinkingBlock {
		t.Error("HideThinkingBlock = false, want true")
	}
}

func TestLoadTUIEmptyThinkingLevelDefaultsOff(t *testing.T) {
	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if err := os.WriteFile(dirs.ConfigPath(), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	tui, err := LoadTUI(dirs)
	if err != nil {
		t.Fatalf("LoadTUI() error = %v", err)
	}
	if tui.ThinkingLevel != thinking.Off {
		t.Errorf("ThinkingLevel = %q, want off", tui.ThinkingLevel)
	}
}

func TestLoadTUIInvalidThinkingLevel(t *testing.T) {
	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if err := os.WriteFile(dirs.ConfigPath(), []byte(`{"thinkingLevel": "turbo"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadTUI(dirs)
	if err == nil {
		t.Fatal("LoadTUI() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "turbo") {
		t.Errorf("error %q does not mention the invalid level", err)
	}
}

func TestLoadTUIMalformedJSON(t *testing.T) {
	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if err := os.WriteFile(dirs.ConfigPath(), []byte(`{`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := LoadTUI(dirs); err == nil {
		t.Fatal("LoadTUI() error = nil, want error")
	}
}

func TestLoadTUIPreservesUserFile(t *testing.T) {
	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	custom := []byte(`{"defaultProvider": "openrouter", "defaultModel": "custom/model", "thinkingLevel": "low"}`)
	if err := os.WriteFile(dirs.ConfigPath(), custom, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := LoadTUI(dirs); err != nil {
		t.Fatalf("LoadTUI() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Clean(dirs.ConfigPath()))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != string(custom) {
		t.Errorf("LoadTUI() rewrote the user config file")
	}
}

func TestLoadSystemPromptCreatesDefault(t *testing.T) {
	dirs := testDirs(t)

	prompt, err := LoadSystemPrompt(dirs)
	if err != nil {
		t.Fatalf("LoadSystemPrompt() error = %v", err)
	}
	if !strings.Contains(prompt, "Foliarz") {
		t.Error("default system prompt does not mention Foliarz")
	}
	if strings.Contains(prompt, "Pi Agent") || strings.Contains(prompt, "inside pi") {
		t.Error("default system prompt still mentions Pi")
	}
	for _, want := range []string{"FOLIA", "NORMAL", "YOLO", "- read:", "- ls:", "- write:", "- edit:", "- bash:", "- datetime:"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("default system prompt does not contain %q", want)
		}
	}

	info, err := os.Stat(dirs.SystemPromptPath())
	if err != nil {
		t.Fatalf("stat system prompt: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("system prompt permissions = %o, want 600", perm)
	}
}

func TestLoadSystemPromptReadsExistingFile(t *testing.T) {
	dirs := testDirs(t)
	if err := dirs.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	custom := "You are a custom assistant.\n"
	if err := os.WriteFile(dirs.SystemPromptPath(), []byte(custom), 0o600); err != nil {
		t.Fatalf("write system prompt: %v", err)
	}

	prompt, err := LoadSystemPrompt(dirs)
	if err != nil {
		t.Fatalf("LoadSystemPrompt() error = %v", err)
	}
	if prompt != custom {
		t.Errorf("prompt = %q, want %q", prompt, custom)
	}
}

func TestLoadIncludesSystemPrompt(t *testing.T) {
	dirs := testDirs(t)
	cfg, err := Load(dirs)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !strings.Contains(cfg.SystemPrompt, "Foliarz") {
		t.Error("Config.SystemPrompt is not loaded")
	}
}

func TestCompactionDefaults(t *testing.T) {
	dirs := testDirs(t)
	tui, err := LoadTUI(dirs)
	if err != nil {
		t.Fatalf("LoadTUI() error = %v", err)
	}
	enabled, reserve, keep := tui.Compaction.Resolve()
	if !enabled {
		t.Error("compaction enabled = false, want true")
	}
	if reserve != 16384 {
		t.Errorf("reserveTokens = %d, want 16384", reserve)
	}
	if keep != 20000 {
		t.Errorf("keepRecentTokens = %d, want 20000", keep)
	}
}
