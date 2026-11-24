package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type writeInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func init() {
	Register(Tool{
		Name:        "write",
		Description: "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Path to the file to write (relative or absolute)"},
				"content": {"type": "string", "description": "Content to write to the file"}
			},
			"required": ["path", "content"],
			"additionalProperties": false
		}`),
		Run: runWrite,
	})
}

func runWrite(ctx context.Context, args json.RawMessage) (string, error) {
	var input writeInput
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("invalid write arguments: %w", err)
	}
	if input.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	absolutePath, err := resolvePathInWorkDir(ctx, input.Path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		return "", fmt.Errorf("create parent directories: %w", err)
	}
	if err := os.WriteFile(absolutePath, []byte(input.Content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("Successfully wrote to %s", input.Path), nil
}
