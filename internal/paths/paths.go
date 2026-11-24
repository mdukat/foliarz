// Package paths resolves the directories Foliarz uses for configuration,
// logs and sessions.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// AppName is the name of the application directories are named after.
	AppName = "foliarz"

	// ConfigFile is the TUI configuration file name.
	ConfigFile = "foliarz.json"

	// ModelsFile is the models configuration file name.
	ModelsFile = "models.json"

	// SystemPromptFile is the system prompt file name.
	SystemPromptFile = "system-prompt.md"
)

// Dirs are the Foliarz directories for the current user.
type Dirs struct {
	// Config holds configuration files (~/.config/foliarz).
	Config string

	// Data holds logs and session files (~/.local/foliarz).
	Data string
}

// Resolve returns the Foliarz directories for the current user.
func Resolve() (Dirs, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Dirs{}, fmt.Errorf("resolve home directory: %w", err)
	}
	if home == "" {
		return Dirs{}, fmt.Errorf("resolve home directory: empty path")
	}
	return Dirs{
		Config: filepath.Join(home, ".config", AppName),
		Data:   filepath.Join(home, ".local", AppName),
	}, nil
}

// ConfigPath returns the path of foliarz.json.
func (d Dirs) ConfigPath() string {
	return filepath.Join(d.Config, ConfigFile)
}

// ModelsPath returns the path of models.json.
func (d Dirs) ModelsPath() string {
	return filepath.Join(d.Config, ModelsFile)
}

// SystemPromptPath returns the path of the system prompt file.
func (d Dirs) SystemPromptPath() string {
	return filepath.Join(d.Config, SystemPromptFile)
}

// LogFilePath returns the path of the Foliarz log file.
func (d Dirs) LogFilePath() string {
	return filepath.Join(d.Data, "foliarz.log")
}

// SkillsDir returns the directory holding user skills.
func (d Dirs) SkillsDir() string {
	return filepath.Join(d.Config, "skills")
}

// SessionsDir returns the directory holding session files.
func (d Dirs) SessionsDir() string {
	return filepath.Join(d.Data, "sessions")
}

// Ensure creates the configuration and data directories if they do not exist.
func (d Dirs) Ensure() error {
	for _, dir := range []string{d.Config, d.SkillsDir(), d.Data, d.SessionsDir()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	return nil
}
