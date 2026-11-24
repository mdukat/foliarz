package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dirs, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	wantConfig := filepath.Join(home, ".config", "foliarz")
	wantData := filepath.Join(home, ".local", "foliarz")
	if dirs.Config != wantConfig {
		t.Errorf("Config = %q, want %q", dirs.Config, wantConfig)
	}
	if dirs.Data != wantData {
		t.Errorf("Data = %q, want %q", dirs.Data, wantData)
	}
	if got, want := dirs.ConfigPath(), filepath.Join(wantConfig, "foliarz.json"); got != want {
		t.Errorf("ConfigPath() = %q, want %q", got, want)
	}
	if got, want := dirs.ModelsPath(), filepath.Join(wantConfig, "models.json"); got != want {
		t.Errorf("ModelsPath() = %q, want %q", got, want)
	}
	if got, want := dirs.SystemPromptPath(), filepath.Join(wantConfig, "system-prompt.md"); got != want {
		t.Errorf("SystemPromptPath() = %q, want %q", got, want)
	}
	if got, want := dirs.SkillsDir(), filepath.Join(wantConfig, "skills"); got != want {
		t.Errorf("SkillsDir() = %q, want %q", got, want)
	}
	if got, want := dirs.SessionsDir(), filepath.Join(wantData, "sessions"); got != want {
		t.Errorf("SessionsDir() = %q, want %q", got, want)
	}
}

func TestEnsure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dirs, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if err := dirs.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	for _, dir := range []string{dirs.Config, dirs.SkillsDir(), dirs.Data, dirs.SessionsDir()} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Errorf("%s permissions = %o, want 700", dir, perm)
		}
	}

	// Ensure is idempotent.
	if err := dirs.Ensure(); err != nil {
		t.Fatalf("second Ensure() error = %v", err)
	}
}
