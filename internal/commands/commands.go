// Package commands implements the Foliarz slash commands.
//
// Every command lives in its own source file and registers itself in an init
// function, so adding a command means adding one file.
package commands

import (
	"fmt"
	"sort"
	"strings"
)

// Action is what a command asks the UI to do.
type Action int

const (
	// ActionNone means the command handled everything itself.
	ActionNone Action = iota

	// ActionQuit asks the UI to close Foliarz.
	ActionQuit

	// ActionNewSession asks the UI to start a new session.
	ActionNewSession

	// ActionSetFolia asks the UI to switch to FOLIA mode.
	ActionSetFolia

	// ActionSetNormal asks the UI to switch to NORMAL mode.
	ActionSetNormal

	// ActionContinue asks the UI to resume the current turn with the
	// existing context.
	ActionContinue

	// ActionSetYolo asks the UI to switch to YOLO mode.
	ActionSetYolo

	// ActionListSessions asks the UI to open the session picker.
	ActionListSessions

	// ActionCompact asks the UI to compact the conversation context.
	ActionCompact

	// ActionSelectModel asks the UI to open the model picker.
	ActionSelectModel
)

// Command is a slash command.
type Command struct {
	// Name is the command name without the leading slash.
	Name string

	// Description is shown in the command list.
	Description string

	// Run executes the command.
	Run func() Action
}

var registry = make(map[string]Command)

// Register adds a command to the registry. It is meant to be called from a
// command file's init function. Register panics on invalid or duplicate names.
func Register(cmd Command) {
	if cmd.Name == "" {
		panic("commands: empty command name")
	}
	if strings.HasPrefix(cmd.Name, "/") || strings.ContainsAny(cmd.Name, " \t\n") {
		panic(fmt.Sprintf("commands: invalid command name %q", cmd.Name))
	}
	if cmd.Run == nil {
		panic(fmt.Sprintf("commands: command %q has no Run function", cmd.Name))
	}
	if _, ok := registry[cmd.Name]; ok {
		panic(fmt.Sprintf("commands: duplicate command %q", cmd.Name))
	}
	registry[cmd.Name] = cmd
}

// All returns all commands sorted by name.
func All() []Command {
	cmds := make([]Command, 0, len(registry))
	for _, cmd := range registry {
		cmds = append(cmds, cmd)
	}
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
	return cmds
}

// Lookup returns the command with the given exact name.
func Lookup(name string) (Command, bool) {
	cmd, ok := registry[name]
	return cmd, ok
}

// Match returns the commands whose name starts with prefix,
// case-insensitively, sorted by name.
func Match(prefix string) []Command {
	prefix = strings.ToLower(prefix)
	var matches []Command
	for _, cmd := range All() {
		if strings.HasPrefix(strings.ToLower(cmd.Name), prefix) {
			matches = append(matches, cmd)
		}
	}
	return matches
}
