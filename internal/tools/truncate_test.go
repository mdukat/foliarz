package tools

import (
	"strings"
	"testing"
)

func TestTruncateHeadNoTruncation(t *testing.T) {
	content := "one\ntwo\nthree"
	result := truncateHead(content)
	if result.truncated || result.content != content || result.totalLines != 3 {
		t.Errorf("result = %+v", result)
	}
}

func TestTruncateHeadLineLimit(t *testing.T) {
	content := strings.TrimSuffix(strings.Repeat("line\n", 2500), "\n")
	result := truncateHead(content)
	if !result.truncated || result.truncatedBy != "lines" {
		t.Fatalf("result = %+v", result)
	}
	if result.outputLines != 2000 || result.totalLines != 2500 {
		t.Errorf("lines = %d/%d, want 2000/2500", result.outputLines, result.totalLines)
	}
}

func TestTruncateHeadByteLimit(t *testing.T) {
	line := strings.Repeat("x", 200)
	content := strings.TrimSuffix(strings.Repeat(line+"\n", 500), "\n")
	result := truncateHead(content)
	if !result.truncated || result.truncatedBy != "bytes" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.content) > maxBytes {
		t.Errorf("content bytes = %d, want <= %d", len(result.content), maxBytes)
	}
}

func TestTruncateHeadFirstLineExceedsLimit(t *testing.T) {
	content := strings.Repeat("x", maxBytes+1)
	result := truncateHead(content)
	if !result.firstLineExceedsMax || result.content != "" {
		t.Errorf("result = %+v", result)
	}
}
