package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
)

type readInput struct {
	Path   string   `json:"path"`
	Offset *float64 `json:"offset"`
	Limit  *float64 `json:"limit"`
}

func init() {
	Register(Tool{
		Name:        "read",
		Description: "Read file contents",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Path to the file to read (relative or absolute)"},
				"offset": {"type": "number", "minimum": 1, "description": "Line number to start reading from (1-indexed)"},
				"limit": {"type": "number", "minimum": 0, "description": "Maximum number of lines to read"}
			},
			"required": ["path"],
			"additionalProperties": false
		}`),
		ReadOnly: true,
		Run:      runRead,
	})
}

func runRead(ctx context.Context, args json.RawMessage) (string, error) {
	var input readInput
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("invalid read arguments: %w", err)
	}
	if input.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	absolutePath, err := resolvePathInWorkDir(ctx, input.Path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(absolutePath)
	if err != nil {
		return "", err
	}

	if len(data) == 0 {
		return "", nil
	}

	// Count lines Pi-style: a trailing newline does not add an empty line.
	allLines := splitLinesForCounting(string(data))
	startLine := 0
	if input.Offset != nil {
		offset := *input.Offset
		if math.IsNaN(offset) || math.IsInf(offset, 0) {
			return "", fmt.Errorf("invalid offset %v", offset)
		}
		if offset > float64(len(allLines)) {
			return "", fmt.Errorf("offset %d is beyond end of file (%d lines total)", int64(offset), len(allLines))
		}
		startLine = int(offset) - 1
		if startLine < 0 {
			startLine = 0
		}
	}
	if startLine >= len(allLines) {
		return "", fmt.Errorf("offset %d is beyond end of file (%d lines total)", startLine+1, len(allLines))
	}

	endLine := len(allLines)
	if input.Limit != nil && *input.Limit >= 0 {
		limit := *input.Limit
		if math.IsNaN(limit) || math.IsInf(limit, 0) {
			return "", fmt.Errorf("invalid limit %v", limit)
		}
		// Clamp against the remaining lines before converting to int, so a huge
		// model-supplied limit cannot overflow or produce a negative slice bound.
		remaining := len(allLines) - startLine
		if limit < float64(remaining) {
			endLine = startLine + int(limit)
		}
	}

	selected := strings.Join(allLines[startLine:endLine], "\n")
	result := truncateHead(selected)
	startDisplay := startLine + 1

	var output string
	switch {
	case result.firstLineExceedsMax:
		output = fmt.Sprintf("[Line %d is %s, exceeds %s limit. Use a smaller offset or limit.]",
			startDisplay, formatSize(len(allLines[startLine])), formatSize(maxBytes))
	case result.truncated:
		endDisplay := startDisplay + result.outputLines - 1
		nextOffset := endDisplay + 1
		output = result.content
		if result.truncatedBy == "lines" {
			output += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]",
				startDisplay, endDisplay, len(allLines), nextOffset)
		} else {
			output += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]",
				startDisplay, endDisplay, len(allLines), formatSize(maxBytes), nextOffset)
		}
	case input.Limit != nil && endLine < len(allLines):
		remaining := len(allLines) - endLine
		output = result.content + fmt.Sprintf("\n\n[%d more lines in file. Use offset=%d to continue.]", remaining, endLine+1)
	default:
		output = result.content
	}
	return output, nil
}
