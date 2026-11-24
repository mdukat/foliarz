package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileToolsRejectPathsOutsideWorkDir(t *testing.T) {
	ctx, workDir := testWorkDir(t)
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}

	readArgs := fmt.Sprintf(`{"path":%q}`, outsideFile)
	if _, err := callTool(t, "read", readArgs, ctx); err == nil || !strings.Contains(err.Error(), "outside the working directory") {
		t.Errorf("read outside error = %v", err)
	}

	writeArgs := fmt.Sprintf(`{"path":%q,"content":"x"}`, filepath.Join(outsideDir, "new.txt"))
	if _, err := callTool(t, "write", writeArgs, ctx); err == nil || !strings.Contains(err.Error(), "outside the working directory") {
		t.Errorf("write outside error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "new.txt")); !os.IsNotExist(err) {
		t.Errorf("write outside created a file")
	}

	editArgs := fmt.Sprintf(`{"path":%q,"edits":[{"oldText":"outside","newText":"changed"}]}`, outsideFile)
	if _, err := callTool(t, "edit", editArgs, ctx); err == nil || !strings.Contains(err.Error(), "outside the working directory") {
		t.Errorf("edit outside error = %v", err)
	}
	data, _ := os.ReadFile(outsideFile)
	if string(data) != "outside" {
		t.Errorf("edit outside modified the file: %q", data)
	}

	// Nested paths inside the working directory are allowed.
	nested := filepath.Join(workDir, "abc", "def", "test.txt")
	if _, err := callTool(t, "write", fmt.Sprintf(`{"path":%q,"content":"inside"}`, nested), ctx); err != nil {
		t.Errorf("write nested inside error = %v", err)
	}
	if got, err := callTool(t, "read", fmt.Sprintf(`{"path":%q}`, nested), ctx); err != nil || got != "inside" {
		t.Errorf("read nested inside = %q, %v", got, err)
	}
}

func TestFileToolsRejectSymlinkEscape(t *testing.T) {
	ctx, workDir := testWorkDir(t)
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workDir, "link.txt")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}

	if _, err := callTool(t, "read", `{"path":"link.txt"}`, ctx); err == nil || !strings.Contains(err.Error(), "outside the working directory") {
		t.Errorf("read through escaping symlink error = %v", err)
	}
}

func TestFileToolsRejectParentDirectoryEscape(t *testing.T) {
	ctx, workDir := testWorkDir(t)
	outside := filepath.Join(filepath.Dir(workDir), "outside-"+filepath.Base(workDir)+".txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)

	relative := "../" + filepath.Base(outside)
	if _, err := callTool(t, "read", fmt.Sprintf(`{"path":%q}`, relative), ctx); err == nil || !strings.Contains(err.Error(), "outside the working directory") {
		t.Errorf("read ../ escape error = %v", err)
	}
}

func TestRelativePathsWorkWithoutWorkDirContext(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool, ok := Lookup("read")
	if !ok {
		t.Fatal("read tool not found")
	}
	got, err := tool.Run(context.Background(), []byte(`{"path":"file.txt"}`))
	if err != nil {
		t.Fatalf("read without workdir error = %v", err)
	}
	if got != "hello" {
		t.Errorf("read = %q", got)
	}
}
