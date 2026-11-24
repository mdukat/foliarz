package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitWritesLog(t *testing.T) {
	original := slog.Default()
	t.Cleanup(func() { slog.SetDefault(original) })

	path := filepath.Join(t.TempDir(), "nested", "foliarz.log")
	file, err := Init(path)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	slog.Error("session save failed", "error", "disk full")
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "session save failed") || !strings.Contains(string(data), "disk full") {
		t.Errorf("log content = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("log permissions = %o, want 600", perm)
	}
}
