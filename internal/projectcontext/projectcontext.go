// Package projectcontext loads project instruction files such as AGENTS.md.
package projectcontext

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the project instruction file loaded from the working directory.
const FileName = "AGENTS.md"

// File is a loaded project instruction file.
type File struct {
	// Path is the absolute path of the file.
	Path string

	// Content is the file content without trailing newlines.
	Content string
}

// Load reads AGENTS.md from dir. found is false when the file does not exist
// or contains only whitespace.
func Load(dir string) (file File, found bool, err error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return File{}, false, nil
	}
	if err != nil {
		return File{}, false, fmt.Errorf("read %s: %w", path, err)
	}

	content := strings.TrimRight(string(data), "\n")
	if strings.TrimSpace(content) == "" {
		return File{}, false, nil
	}
	return File{Path: path, Content: content}, true, nil
}

// Prompt renders the file as project instructions for the system prompt.
func (f File) Prompt() string {
	return "Project-specific instructions and guidelines:\n\n" +
		"<project_instructions path=\"" + f.Path + "\">\n" +
		f.Content + "\n" +
		"</project_instructions>"
}
