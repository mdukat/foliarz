package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const defaultLsLimit = 500

type lsInput struct {
	Path  string   `json:"path"`
	Limit *float64 `json:"limit"`
}

func init() {
	Register(Tool{
		Name:        "ls",
		Description: "List directory contents. Returns entries sorted alphabetically, with '/' suffix for directories. Includes dotfiles. Output is truncated to 500 entries or 50KB (whichever is hit first).",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Directory to list (default: current directory)"},
				"limit": {"type": "number", "description": "Maximum number of entries to return (default: 500)"}
			},
			"additionalProperties": false
		}`),
		ReadOnly: true,
		Run:      runLs,
	})
}

func runLs(ctx context.Context, args json.RawMessage) (string, error) {
	var input lsInput
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("invalid ls arguments: %w", err)
	}

	path := input.Path
	if path == "" {
		path = "."
	}
	dirPath, err := resolvePathInWorkDir(ctx, path)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(dirPath)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("path not found: %s", dirPath)
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", dirPath)
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return "", fmt.Errorf("cannot read directory: %v", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		left := strings.ToLower(entries[i].Name())
		right := strings.ToLower(entries[j].Name())
		if left != right {
			return left < right
		}
		return entries[i].Name() < entries[j].Name()
	})

	limit := defaultLsLimit
	if input.Limit != nil {
		limit = int(*input.Limit)
		if limit < 1 {
			limit = 1
		}
	}

	results := make([]string, 0, min(len(entries), limit))
	entryLimitReached := false
	for _, entry := range entries {
		if len(results) >= limit {
			entryLimitReached = true
			break
		}
		entryInfo, err := entry.Info()
		if err != nil {
			continue
		}
		name := entry.Name()
		if entryInfo.IsDir() {
			name += "/"
		}
		results = append(results, name)
	}

	if len(results) == 0 {
		return "(empty directory)", nil
	}

	truncation := truncateHead(strings.Join(results, "\n"))
	output := truncation.content

	var notices []string
	if entryLimitReached {
		notices = append(notices, fmt.Sprintf("%d entries limit reached. Use limit=%d for more", limit, limit*2))
	}
	if truncation.truncated {
		notices = append(notices, formatSize(maxBytes)+" limit reached")
	}
	if len(notices) > 0 {
		output += "\n\n[" + strings.Join(notices, ". ") + "]"
	}
	return output, nil
}
