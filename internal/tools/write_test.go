package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCreatesFileAndDirectories(t *testing.T) {
	ctx, dir := testWorkDir(t)

	got, err := callTool(t, "write", `{"path":"nested/dir/file.txt","content":"hello"}`, ctx)
	if err != nil {
		t.Fatalf("write error = %v", err)
	}
	if got != "Successfully wrote to nested/dir/file.txt" {
		t.Errorf("write = %q", got)
	}

	data, err := os.ReadFile(filepath.Join(dir, "nested", "dir", "file.txt"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q", data)
	}
}

func TestWriteOverwrites(t *testing.T) {
	ctx, dir := testWorkDir(t)
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := callTool(t, "write", `{"path":"file.txt","content":"new"}`, ctx); err != nil {
		t.Fatalf("write error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Errorf("content = %q, want new", data)
	}
}

func TestWriteRequiresPath(t *testing.T) {
	ctx, _ := testWorkDir(t)
	if _, err := callTool(t, "write", `{"content":"x"}`, ctx); err == nil {
		t.Fatal("write error = nil, want error")
	}
}
