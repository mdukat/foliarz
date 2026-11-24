package projectcontext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("# Instructions\n\nBe careful.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	file, found, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !found {
		t.Fatal("Load() did not find AGENTS.md")
	}
	if file.Path != path {
		t.Errorf("Path = %q, want %q", file.Path, path)
	}
	if file.Content != "# Instructions\n\nBe careful." {
		t.Errorf("Content = %q", file.Content)
	}

	prompt := file.Prompt()
	for _, want := range []string{"Project-specific instructions", path, "# Instructions", "Be careful."} {
		if !strings.Contains(prompt, want) {
			t.Errorf("Prompt() does not contain %q:\n%s", want, prompt)
		}
	}
}

func TestLoadMissing(t *testing.T) {
	file, found, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if found {
		t.Error("Load() found a missing file")
	}
	if file.Content != "" {
		t.Errorf("Content = %q", file.Content)
	}
}

func TestLoadEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(" \n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, found, err := Load(dir); err != nil || found {
		t.Errorf("Load() = found %v, error %v", found, err)
	}
}
