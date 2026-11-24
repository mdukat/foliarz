// Package config loads the Foliarz configuration files.
package config

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mdukat/foliarz/internal/paths"
	"github.com/mdukat/foliarz/internal/thinking"
)

//go:embed defaults/foliarz.json
var defaultTUIFile []byte

//go:embed defaults/system-prompt.md
var defaultSystemPromptFile []byte

// Config is the fully loaded Foliarz configuration.
type Config struct {
	// TUI is the TUI-scope configuration from foliarz.json.
	TUI TUI

	// SystemPrompt is the content of system-prompt.md.
	SystemPrompt string

	// Providers holds the built-in and user-defined providers with their
	// models, after applying model overrides.
	Providers map[string]*Provider
}

// Compaction settings defaults, matching Pi Agent.
const (
	DefaultCompactionReserveTokens    = 16384
	DefaultCompactionKeepRecentTokens = 20000
)

// CompactionSettings is the `compaction` object in foliarz.json.
type CompactionSettings struct {
	Enabled          *bool `json:"enabled,omitempty"`
	ReserveTokens    int   `json:"reserveTokens,omitempty"`
	KeepRecentTokens int   `json:"keepRecentTokens,omitempty"`
}

// Resolve applies defaults and returns the effective values.
func (c CompactionSettings) Resolve() (enabled bool, reserveTokens, keepRecentTokens int) {
	enabled = true
	if c.Enabled != nil {
		enabled = *c.Enabled
	}
	reserveTokens = c.ReserveTokens
	if reserveTokens <= 0 {
		reserveTokens = DefaultCompactionReserveTokens
	}
	keepRecentTokens = c.KeepRecentTokens
	if keepRecentTokens <= 0 {
		keepRecentTokens = DefaultCompactionKeepRecentTokens
	}
	return enabled, reserveTokens, keepRecentTokens
}

// TUI is the TUI-scope configuration stored in foliarz.json.
type TUI struct {
	// DefaultProvider is the provider ID used by default.
	DefaultProvider string `json:"defaultProvider"`

	// DefaultModel is the model ID used by default.
	DefaultModel string `json:"defaultModel"`

	// ThinkingLevel is the default thinking level.
	ThinkingLevel string `json:"thinkingLevel"`

	// HideThinkingBlock hides the model's thinking from the transcript.
	HideThinkingBlock bool `json:"hideThinkingBlock"`

	// LastProvider is the provider used by the last session.
	LastProvider string `json:"lastProvider,omitempty"`

	// LastModel is the model used by the last session.
	LastModel string `json:"lastModel,omitempty"`

	// LastThinkingLevel is the thinking level used by the last session.
	LastThinkingLevel string `json:"lastThinkingLevel,omitempty"`

	// Compaction configures automatic context compaction.
	Compaction CompactionSettings `json:"compaction"`
}

// ActiveProvider returns the provider to start with: the last used provider,
// or the default provider.
func (t TUI) ActiveProvider() string {
	if t.LastProvider != "" {
		return t.LastProvider
	}
	return t.DefaultProvider
}

// ActiveModel returns the model to start with: the last used model, or the
// default model.
func (t TUI) ActiveModel() string {
	if t.LastModel != "" {
		return t.LastModel
	}
	return t.DefaultModel
}

// ActiveThinkingLevel returns the thinking level to start with: the last used
// level, or the default level.
func (t TUI) ActiveThinkingLevel() string {
	if t.LastThinkingLevel != "" && thinking.Valid(t.LastThinkingLevel) {
		return t.LastThinkingLevel
	}
	return t.ThinkingLevel
}

// LoadSystemPrompt loads system-prompt.md, creating it with the default when it
// does not exist.
func LoadSystemPrompt(dirs paths.Dirs) (string, error) {
	if err := dirs.Ensure(); err != nil {
		return "", err
	}
	if err := ensureFile(dirs.SystemPromptPath(), defaultSystemPromptFile); err != nil {
		return "", err
	}

	data, err := os.ReadFile(dirs.SystemPromptPath())
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dirs.SystemPromptPath(), err)
	}
	return string(data), nil
}

func LoadTUI(dirs paths.Dirs) (TUI, error) {
	if err := dirs.Ensure(); err != nil {
		return TUI{}, err
	}
	if err := ensureFile(dirs.ConfigPath(), defaultTUIFile); err != nil {
		return TUI{}, err
	}

	data, err := os.ReadFile(dirs.ConfigPath())
	if err != nil {
		return TUI{}, fmt.Errorf("read %s: %w", dirs.ConfigPath(), err)
	}

	var tui TUI
	if err := json.Unmarshal(data, &tui); err != nil {
		return TUI{}, fmt.Errorf("parse %s: %w", dirs.ConfigPath(), err)
	}
	if err := tui.validate(); err != nil {
		return TUI{}, fmt.Errorf("%s: %w", dirs.ConfigPath(), err)
	}
	return tui, nil
}

func (t *TUI) validate() error {
	if t.ThinkingLevel == "" {
		t.ThinkingLevel = thinking.Off
	}
	if !thinking.Valid(t.ThinkingLevel) {
		return fmt.Errorf("unknown thinking level %q", t.ThinkingLevel)
	}
	// An unknown lastThinkingLevel is treated as unset rather than an error so
	// an old or edited configuration cannot block startup.
	return nil
}

// SaveLastUsed records the last used provider, model and thinking level in
// foliarz.json. Other fields of the file are preserved.
func SaveLastUsed(dirs paths.Dirs, providerID, modelID, thinkingLevel string) error {
	if err := dirs.Ensure(); err != nil {
		return err
	}

	doc := make(map[string]any)
	data, err := os.ReadFile(dirs.ConfigPath())
	switch {
	case err == nil:
		if len(strings.TrimSpace(string(data))) > 0 {
			if err := json.Unmarshal(data, &doc); err != nil {
				return fmt.Errorf("parse %s: %w", dirs.ConfigPath(), err)
			}
		}
	case os.IsNotExist(err):
	default:
		return fmt.Errorf("read %s: %w", dirs.ConfigPath(), err)
	}

	doc["lastProvider"] = providerID
	doc["lastModel"] = modelID
	doc["lastThinkingLevel"] = thinkingLevel

	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", dirs.ConfigPath(), err)
	}
	encoded = append(encoded, '\n')
	return writeFileAtomic(dirs.ConfigPath(), encoded, 0o600)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("set file permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// ensureFile creates path with content when it does not exist yet.
func ensureFile(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	return f.Close()
}
