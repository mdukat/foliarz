package tools

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testWorkDir(t *testing.T) (context.Context, string) {
	t.Helper()
	dir := t.TempDir()
	return WithWorkDir(context.Background(), dir), dir
}

func callTool(t *testing.T, name string, args string, ctx context.Context) (string, error) {
	t.Helper()
	tool, ok := Lookup(name)
	if !ok {
		t.Fatalf("tool %q not found", name)
	}
	return tool.Run(ctx, []byte(args))
}

func TestReadFile(t *testing.T) {
	ctx, dir := testWorkDir(t)
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := callTool(t, "read", `{"path":"file.txt"}`, ctx)
	if err != nil {
		t.Fatalf("read error = %v", err)
	}
	if want := "one\ntwo\nthree"; got != want {
		t.Errorf("read = %q, want %q", got, want)
	}
}

func TestReadOffsetAndLimit(t *testing.T) {
	ctx, dir := testWorkDir(t)
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := callTool(t, "read", `{"path":"file.txt","offset":2,"limit":2}`, ctx)
	if err != nil {
		t.Fatalf("read error = %v", err)
	}
	want := "two\nthree\n\n[1 more lines in file. Use offset=4 to continue.]"
	if got != want {
		t.Errorf("read = %q, want %q", got, want)
	}
}

func TestReadOffsetBeyondEnd(t *testing.T) {
	ctx, dir := testWorkDir(t)
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := callTool(t, "read", `{"path":"file.txt","offset":10}`, ctx); err == nil {
		t.Fatal("read error = nil, want offset error")
	} else if !strings.Contains(err.Error(), "beyond end of file") {
		t.Errorf("error = %q", err)
	}
}

func TestReadMissingFile(t *testing.T) {
	ctx, _ := testWorkDir(t)
	if _, err := callTool(t, "read", `{"path":"missing.txt"}`, ctx); err == nil {
		t.Fatal("read error = nil, want error")
	}
}

func TestReadTruncatesLongFiles(t *testing.T) {
	ctx, dir := testWorkDir(t)
	path := filepath.Join(dir, "long.txt")
	lines := make([]string, 2500)
	for i := range lines {
		lines[i] = "line"
	}
	builder := strings.NewReader(strings.Join(lines, "\n"))
	data, err := io.ReadAll(builder)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := callTool(t, "read", `{"path":"long.txt"}`, ctx)
	if err != nil {
		t.Fatalf("read error = %v", err)
	}
	if !strings.Contains(got, "[Showing lines 1-2000 of 2500. Use offset=2001 to continue.]") {
		t.Errorf("read notice missing:\n%s", got)
	}
	if lines := strings.Count(got, "\nline"); lines != 1999 {
		t.Errorf("line count = %d, want 1999 extra lines", lines)
	}
}

func TestReadCountsLinesWithoutPhantomTrailingLine(t *testing.T) {
	ctx, dir := testWorkDir(t)
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := callTool(t, "read", `{"path":"file.txt","offset":4}`, ctx)
	if err != nil {
		t.Fatalf("read error = %v", err)
	}
	if got != "four" {
		t.Errorf("read = %q, want %q", got, "four")
	}

	if _, err := callTool(t, "read", `{"path":"file.txt","offset":5}`, ctx); err == nil {
		t.Error("read past the last line did not fail")
	} else if !strings.Contains(err.Error(), "4 lines total") {
		t.Errorf("error = %q", err)
	}
}

func TestReadEmptyFile(t *testing.T) {
	ctx, dir := testWorkDir(t)
	if err := os.WriteFile(filepath.Join(dir, "empty.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := callTool(t, "read", `{"path":"empty.txt"}`, ctx)
	if err != nil {
		t.Fatalf("read error = %v", err)
	}
	if got != "" {
		t.Errorf("read = %q, want empty", got)
	}
}

func TestReadHugeLimitDoesNotPanic(t *testing.T) {
	ctx, dir := testWorkDir(t)
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, limit := range []string{"1e19", "9223372036854775807"} {
		got, err := callTool(t, "read", `{"path":"file.txt","limit":`+limit+`}`, ctx)
		if err != nil {
			t.Fatalf("read with limit %s error = %v", limit, err)
		}
		if got != "one\ntwo\nthree" {
			t.Errorf("read with limit %s = %q", limit, got)
		}
	}

	got, err := callTool(t, "read", `{"path":"file.txt","offset":2,"limit":9223372036854775807}`, ctx)
	if err != nil {
		t.Fatalf("read with offset and huge limit error = %v", err)
	}
	if got != "two\nthree" {
		t.Errorf("read = %q, want two\\nthree", got)
	}
}

func TestReadHugeOffsetDoesNotPanic(t *testing.T) {
	ctx, dir := testWorkDir(t)
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("one\ntwo"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := callTool(t, "read", `{"path":"file.txt","offset":1e19}`, ctx); err == nil {
		t.Fatal("read with a huge offset did not fail")
	} else if !strings.Contains(err.Error(), "beyond end of file") {
		t.Errorf("error = %v", err)
	}
}
