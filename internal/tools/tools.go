// Package tools implements the tools the agent can call.
//
// Every tool lives in its own source file and registers itself in an init
// function, so adding a tool means adding one file.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Tool is a function the agent can call.
type Tool struct {
	// Name is the function name sent to the model.
	Name string

	// Description tells the model what the tool does.
	Description string

	// Parameters is the JSON schema of the tool arguments.
	Parameters json.RawMessage

	// ReadOnly marks tools that do not modify files or execute commands.
	// FOLIA mode only exposes read-only tools.
	ReadOnly bool

	// Run executes the tool and returns its result as text.
	Run func(ctx context.Context, args json.RawMessage) (string, error)
}

var registry = make(map[string]Tool)

// Register adds a tool to the registry. It is meant to be called from a tool
// file's init function. Register panics on invalid or duplicate names.
func Register(tool Tool) {
	if tool.Name == "" {
		panic("tools: empty tool name")
	}
	if strings.ContainsAny(tool.Name, " \t\n/") {
		panic(fmt.Sprintf("tools: invalid tool name %q", tool.Name))
	}
	if tool.Description == "" {
		panic(fmt.Sprintf("tools: tool %q has no description", tool.Name))
	}
	if len(tool.Parameters) == 0 || !json.Valid(tool.Parameters) {
		panic(fmt.Sprintf("tools: tool %q has invalid parameters schema", tool.Name))
	}
	if tool.Run == nil {
		panic(fmt.Sprintf("tools: tool %q has no Run function", tool.Name))
	}
	if _, ok := registry[tool.Name]; ok {
		panic(fmt.Sprintf("tools: duplicate tool %q", tool.Name))
	}
	registry[tool.Name] = tool
}

// All returns all tools sorted by name.
func All() []Tool {
	all := make([]Tool, 0, len(registry))
	for _, tool := range registry {
		all = append(all, tool)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all
}

// Lookup returns the tool with the given name.
func Lookup(name string) (Tool, bool) {
	tool, ok := registry[name]
	return tool, ok
}

type contextKey int

const workDirKey contextKey = iota

// WithWorkDir attaches the working directory tools resolve paths against.
func WithWorkDir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, workDirKey, dir)
}

// WorkDir returns the working directory stored in ctx, falling back to the
// process working directory.
func WorkDir(ctx context.Context) string {
	if dir, ok := ctx.Value(workDirKey).(string); ok && dir != "" {
		return dir
	}
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return "."
}
