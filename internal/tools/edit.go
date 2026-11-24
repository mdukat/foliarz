package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type editReplacement struct {
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

type editInput struct {
	Path  string            `json:"path"`
	Edits []editReplacement `json:"edits"`
}

func init() {
	Register(Tool{
		Name:        "edit",
		Description: "Edit a single file using exact text replacement. Every edits[].oldText must match a unique, non-overlapping region of the original file. If two changes affect the same block or nearby lines, merge them into one edit instead of emitting overlapping edits.",
		Parameters: json.RawMessage(`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "Path to the file to edit (relative or absolute)"},
				"edits": {
					"type": "array",
					"description": "One or more targeted replacements. Each edit is matched against the original file, not incrementally. Do not include overlapping or nested edits.",
					"items": {
						"type": "object",
						"properties": {
							"oldText": {"type": "string", "description": "Exact text for one targeted replacement. It must be unique in the original file and must not overlap with any other edits[].oldText in the same call."},
							"newText": {"type": "string", "description": "Replacement text for this targeted edit."}
						},
						"required": ["oldText", "newText"],
						"additionalProperties": false
					}
				}
			},
			"required": ["path", "edits"],
			"additionalProperties": false
		}`),
		Run: runEdit,
	})
}

type editMatch struct {
	editIndex int
	index     int
	length    int
	newText   string
}

func runEdit(ctx context.Context, args json.RawMessage) (string, error) {
	var input editInput
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("invalid edit arguments: %w", err)
	}
	if input.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if len(input.Edits) == 0 {
		return "", fmt.Errorf("edit tool input is invalid. edits must contain at least one replacement")
	}

	absolutePath, err := resolvePathInWorkDir(ctx, input.Path)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(absolutePath)
	if err != nil {
		return "", fmt.Errorf("could not edit file: %s. %v.", input.Path, err)
	}
	content := string(raw)
	bom := ""
	if strings.HasPrefix(content, "\uFEFF") {
		bom = "\uFEFF"
		content = strings.TrimPrefix(content, "\uFEFF")
	}

	normalized := normalizeToLF(content)
	total := len(input.Edits)
	matches := make([]editMatch, 0, total)

	for i, edit := range input.Edits {
		oldText := normalizeToLF(edit.OldText)
		newText := normalizeToLF(edit.NewText)
		if oldText == "" {
			if total == 1 {
				return "", fmt.Errorf("oldText must not be empty in %s", input.Path)
			}
			return "", fmt.Errorf("edits[%d].oldText must not be empty in %s", i, input.Path)
		}

		occurrences := strings.Count(normalized, oldText)
		switch {
		case occurrences == 0:
			if total == 1 {
				return "", fmt.Errorf("could not find the exact text in %s. The old text must match exactly including all whitespace and newlines", input.Path)
			}
			return "", fmt.Errorf("could not find edits[%d] in %s. The oldText must match exactly including all whitespace and newlines", i, input.Path)
		case occurrences > 1:
			if total == 1 {
				return "", fmt.Errorf("found %d occurrences of the text in %s. The text must be unique. Please provide more context to make it unique", occurrences, input.Path)
			}
			return "", fmt.Errorf("found %d occurrences of edits[%d] in %s. Each oldText must be unique. Please provide more context to make it unique", occurrences, i, input.Path)
		}

		matches = append(matches, editMatch{
			editIndex: i,
			index:     strings.Index(normalized, oldText),
			length:    len(oldText),
			newText:   newText,
		})
	}

	sort.Slice(matches, func(i, j int) bool { return matches[i].index < matches[j].index })
	for i := 1; i < len(matches); i++ {
		previous := matches[i-1]
		current := matches[i]
		if previous.index+previous.length > current.index {
			return "", fmt.Errorf("edits[%d] and edits[%d] overlap in %s. Merge them into one edit or target disjoint regions",
				previous.editIndex, current.editIndex, input.Path)
		}
	}

	var builder strings.Builder
	position := 0
	for _, match := range matches {
		builder.WriteString(normalized[position:match.index])
		builder.WriteString(match.newText)
		position = match.index + match.length
	}
	builder.WriteString(normalized[position:])
	newContent := builder.String()

	if newContent == normalized {
		if total == 1 {
			return "", fmt.Errorf("no changes made to %s. The replacement produced identical content. This might indicate an issue with special characters or the text not existing as expected", input.Path)
		}
		return "", fmt.Errorf("no changes made to %s. The replacements produced identical content", input.Path)
	}

	// Preserve CRLF files. Mixed line endings are normalized to LF.
	if usesCRLF(content) {
		newContent = strings.ReplaceAll(newContent, "\n", "\r\n")
	}
	if err := os.WriteFile(absolutePath, []byte(bom+newContent), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("Successfully replaced %d block(s) in %s", len(input.Edits), input.Path), nil
}

func normalizeToLF(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

func usesCRLF(content string) bool {
	return strings.Contains(content, "\r\n") && !strings.Contains(strings.ReplaceAll(content, "\r\n", ""), "\n")
}
