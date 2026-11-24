package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBashRequiresTimeout(t *testing.T) {
	ctx, dir := testWorkDir(t)
	_, err := callTool(t, "bash", `{"command":"touch marker.txt"}`, ctx)
	if err == nil || !strings.Contains(err.Error(), "timeout is required") {
		t.Fatalf("bash error = %v, want timeout required", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker.txt")); !os.IsNotExist(err) {
		t.Error("bash executed without a timeout")
	}
}

func TestBashRunsCommand(t *testing.T) {
	ctx, _ := testWorkDir(t)
	got, err := callTool(t, "bash", `{"command":"echo hello","timeout":10}`, ctx)
	if err != nil {
		t.Fatalf("bash error = %v", err)
	}
	if strings.TrimSpace(got) != "hello" {
		t.Errorf("bash = %q", got)
	}
}

func TestBashReportsExitCode(t *testing.T) {
	ctx, _ := testWorkDir(t)
	got, err := callTool(t, "bash", `{"command":"echo out; exit 3","timeout":10}`, ctx)
	if err == nil || !strings.Contains(err.Error(), "Command exited with code 3") {
		t.Fatalf("bash error = %v, want exit code error", err)
	}
	if !strings.Contains(got, "out") {
		t.Errorf("bash output = %q", got)
	}
}

func TestBashTimeout(t *testing.T) {
	ctx, _ := testWorkDir(t)
	start := time.Now()
	_, err := callTool(t, "bash", `{"command":"sleep 10","timeout":0.2}`, ctx)
	if err == nil || !strings.Contains(err.Error(), "Command timed out after 0.2 seconds") {
		t.Fatalf("bash error = %v, want timeout error", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("timeout took %v", elapsed)
	}
}

func TestBashTruncatesOutput(t *testing.T) {
	ctx, _ := testWorkDir(t)
	got, err := callTool(t, "bash", `{"command":"seq 1 3000","timeout":30}`, ctx)
	if err != nil {
		t.Fatalf("bash error = %v", err)
	}
	if !strings.Contains(got, "[Showing lines 1001-3000 of 3000.") {
		t.Errorf("bash output does not contain truncation notice:\n%s", got[len(got)-500:])
	}
	const marker = "Full output: "
	index := strings.LastIndex(got, marker)
	if index == -1 {
		t.Fatal("bash output has no full output path")
	}
	path := strings.TrimSuffix(got[index+len(marker):], "]")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("full output file %s: %v", path, err)
	}
	os.Remove(path)
}

func TestBashUsesWorkingDirectory(t *testing.T) {
	ctx, dir := testWorkDir(t)
	got, err := callTool(t, "bash", `{"command":"pwd","timeout":10}`, ctx)
	if err != nil {
		t.Fatalf("bash error = %v", err)
	}
	if strings.TrimSpace(got) != dir {
		t.Errorf("pwd = %q, want %q", got, dir)
	}
}

func TestBashCancellationKillsProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(WithWorkDir(context.Background(), t.TempDir()))
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	if _, err := callTool(t, "bash", `{"command":"sleep 30","timeout":60}`, ctx); err == nil || !strings.Contains(err.Error(), "Command aborted") {
		t.Fatalf("bash error = %v, want abort error", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("cancellation took %v", elapsed)
	}
}

func TestBashTimeoutWithDetachedChild(t *testing.T) {
	ctx, _ := testWorkDir(t)
	start := time.Now()
	_, err := callTool(t, "bash", `{"command":"setsid sleep 5 & sleep 5","timeout":0.3}`, ctx)
	elapsed := time.Since(start)
	if elapsed > 15*time.Second {
		t.Fatalf("timeout took %v with a detached child", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %v, want timeout", err)
	}
}

func TestCleanOutputFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)

	oldPath := filepath.Join(dir, bashOutputPrefix+"old")
	newPath := filepath.Join(dir, bashOutputPrefix+"new")
	otherPath := filepath.Join(dir, "unrelated.txt")
	for _, path := range []string{oldPath, newPath, otherPath} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oldTime := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	if removed := CleanOutputFiles(7 * 24 * time.Hour); removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("old bash output file was not removed")
	}
	for _, path := range []string{newPath, otherPath} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was removed: %v", path, err)
		}
	}
}
