package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditReplacesText(t *testing.T) {
	ctx, dir := testWorkDir(t)
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := callTool(t, "edit", `{"path":"file.txt","edits":[{"oldText":"world","newText":"there"}]}`, ctx)
	if err != nil {
		t.Fatalf("edit error = %v", err)
	}
	if got != "Successfully replaced 1 block(s) in file.txt" {
		t.Errorf("edit = %q", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello there" {
		t.Errorf("content = %q", data)
	}
}

func TestEditMultipleReplacements(t *testing.T) {
	ctx, dir := testWorkDir(t)
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := callTool(t, "edit", `{"path":"file.txt","edits":[{"oldText":"b","newText":"B"},{"oldText":"c","newText":"C"}]}`, ctx)
	if err != nil {
		t.Fatalf("edit error = %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "a\nB\nC\n" {
		t.Errorf("content = %q", data)
	}
}

func TestEditErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		args    string
		want    string
	}{
		{
			name:    "duplicate",
			content: "x x",
			args:    `{"path":"file.txt","edits":[{"oldText":"x","newText":"y"}]}`,
			want:    "occurrences",
		},
		{
			name:    "not found",
			content: "abc",
			args:    `{"path":"file.txt","edits":[{"oldText":"zzz","newText":"y"}]}`,
			want:    "could not find the exact text",
		},
		{
			name:    "empty oldText",
			content: "abc",
			args:    `{"path":"file.txt","edits":[{"oldText":"","newText":"y"}]}`,
			want:    "oldText must not be empty",
		},
		{
			name:    "overlap",
			content: "abc",
			args:    `{"path":"file.txt","edits":[{"oldText":"ab","newText":"X"},{"oldText":"bc","newText":"Y"}]}`,
			want:    "overlap",
		},
		{
			name:    "no change",
			content: "abc",
			args:    `{"path":"file.txt","edits":[{"oldText":"b","newText":"b"}]}`,
			want:    "no changes made",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, dir := testWorkDir(t)
			if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(test.content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := callTool(t, "edit", test.args, ctx)
			if err == nil {
				t.Fatal("edit error = nil, want error")
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.want)) {
				t.Errorf("error = %q, want %q", err, test.want)
			}
		})
	}
}

func TestEditPreservesCRLFAndBOM(t *testing.T) {
	ctx, dir := testWorkDir(t)
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("\uFEFFone\r\ntwo\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := callTool(t, "edit", `{"path":"file.txt","edits":[{"oldText":"two","newText":"three"}]}`, ctx)
	if err != nil {
		t.Fatalf("edit error = %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "\uFEFFone\r\nthree\r\n" {
		t.Errorf("content = %q", data)
	}
}
