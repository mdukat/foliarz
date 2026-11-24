package tools

import (
	"fmt"
	"strings"
)

const (
	// maxLines and maxBytes limit tool output, like in Pi Agent.
	maxLines = 2000
	maxBytes = 50 * 1024
)

// formatSize renders bytes as a human-readable size.
func formatSize(bytes int) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%dB", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	}
}

// splitLinesForCounting splits content into lines the way Pi Agent counts them:
// a trailing newline does not add an empty line.
func splitLinesForCounting(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if strings.HasSuffix(content, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

type truncation struct {
	content             string
	truncated           bool
	truncatedBy         string // "lines" or "bytes"
	totalLines          int
	outputLines         int
	firstLineExceedsMax bool
	lastLinePartial     bool
}

// truncateHead keeps the first lines/bytes of content, never partial lines.
func truncateHead(content string) truncation {
	lines := splitLinesForCounting(content)
	totalBytes := len(content)
	if len(lines) <= maxLines && totalBytes <= maxBytes {
		return truncation{content: content, totalLines: len(lines), outputLines: len(lines)}
	}
	if len(lines) > 0 && len(lines[0]) > maxBytes {
		return truncation{
			truncated:           true,
			truncatedBy:         "bytes",
			totalLines:          len(lines),
			firstLineExceedsMax: true,
		}
	}

	var kept []string
	bytes := 0
	by := "lines"
	for i, line := range lines {
		if i >= maxLines {
			break
		}
		lineBytes := len(line)
		if i > 0 {
			lineBytes++
		}
		if bytes+lineBytes > maxBytes {
			by = "bytes"
			break
		}
		kept = append(kept, line)
		bytes += lineBytes
	}
	if len(kept) >= maxLines && bytes <= maxBytes {
		by = "lines"
	}

	return truncation{
		content:     strings.Join(kept, "\n"),
		truncated:   true,
		truncatedBy: by,
		totalLines:  len(lines),
		outputLines: len(kept),
	}
}
