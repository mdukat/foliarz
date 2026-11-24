package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLsListsEntries(t *testing.T) {
	ctx, dir := testWorkDir(t)
	if err := os.Mkdir(filepath.Join(dir, "zeta"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("h"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := callTool(t, "ls", `{}`, ctx)
	if err != nil {
		t.Fatalf("ls error = %v", err)
	}
	want := ".hidden\nalpha.txt\nzeta/"
	if got != want {
		t.Errorf("ls = %q, want %q", got, want)
	}
}

func TestLsListsDirectoryArgument(t *testing.T) {
	ctx, dir := testWorkDir(t)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := callTool(t, "ls", `{"path":"sub"}`, ctx)
	if err != nil {
		t.Fatalf("ls error = %v", err)
	}
	if got != "file.txt" {
		t.Errorf("ls = %q", got)
	}
}

func TestLsLimit(t *testing.T) {
	ctx, dir := testWorkDir(t)
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := callTool(t, "ls", `{"limit":2}`, ctx)
	if err != nil {
		t.Fatalf("ls error = %v", err)
	}
	if !strings.Contains(got, "a\nb\n\n[2 entries limit reached. Use limit=4 for more]") {
		t.Errorf("ls = %q", got)
	}
}

func TestLsErrors(t *testing.T) {
	ctx, dir := testWorkDir(t)
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := callTool(t, "ls", `{"path":"missing"}`, ctx); err == nil || !strings.Contains(err.Error(), "path not found") {
		t.Errorf("missing path error = %v", err)
	}
	if _, err := callTool(t, "ls", `{"path":"file.txt"}`, ctx); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("file path error = %v", err)
	}

	outside := t.TempDir()
	if _, err := callTool(t, "ls", `{"path":"`+outside+`"}`, ctx); err == nil || !strings.Contains(err.Error(), "outside the working directory") {
		t.Errorf("outside path error = %v", err)
	}
}

func TestLsEmptyDirectory(t *testing.T) {
	ctx, dir := testWorkDir(t)
	if err := os.Mkdir(filepath.Join(dir, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := callTool(t, "ls", `{"path":"empty"}`, ctx)
	if err != nil {
		t.Fatalf("ls error = %v", err)
	}
	if got != "(empty directory)" {
		t.Errorf("ls = %q", got)
	}
}
