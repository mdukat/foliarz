package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/mdukat/foliarz/internal/commands"
)

var (
	selectedCommandStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	noCommandsStyle      = lipgloss.NewStyle().Faint(true)
)

// commandQuery returns the command name being typed at the start of the input,
// without the leading slash. ok is false when the input is not a slash command
// or already contains arguments or newlines.
func (m Model) commandQuery() (string, bool) {
	value := m.input.Value()
	if strings.HasPrefix(value, "/skill:") {
		return "", false
	}
	if !strings.HasPrefix(value, "/") {
		return "", false
	}
	query := strings.TrimPrefix(value, "/")
	if strings.ContainsAny(query, " \t\n") {
		return "", false
	}
	return query, true
}

// commandMatches returns the commands matching the query being typed.
func (m Model) commandMatches() []commands.Command {
	query, ok := m.commandQuery()
	if !ok {
		return nil
	}
	return commands.Match(query)
}

// commandIndex returns the highlighted index, clamped to the number of
// matches. It returns -1 when there is no match.
func (m Model) commandIndex(matches []commands.Command) int {
	if len(matches) == 0 {
		return -1
	}
	if m.commandCursor <= 0 {
		return 0
	}
	if m.commandCursor >= len(matches) {
		return len(matches) - 1
	}
	return m.commandCursor
}

// moveCommandCursor moves the highlighted command, wrapping around.
func (m *Model) moveCommandCursor(delta int) {
	matches := m.commandMatches()
	if len(matches) == 0 {
		m.commandCursor = 0
		return
	}
	index := m.commandIndex(matches)
	m.commandCursor = (index + delta + len(matches)) % len(matches)
}

// commandPalette renders the list of matching commands, or an empty string
// when the input is not a slash command.
func (m Model) commandPalette() string {
	query, ok := m.commandQuery()
	if !ok {
		return ""
	}

	matches := commands.Match(query)
	if len(matches) == 0 {
		return noCommandsStyle.Render(truncate("no matching commands", m.width))
	}

	nameWidth := 0
	for _, command := range matches {
		if len(command.Name) > nameWidth {
			nameWidth = len(command.Name)
		}
	}

	selected := m.commandIndex(matches)
	lines := make([]string, 0, len(matches))
	for i, command := range matches {
		line := fmt.Sprintf("/%-*s  %s", nameWidth, command.Name, command.Description)
		if i == selected {
			line = selectedCommandStyle.Render(line)
		}
		lines = append(lines, truncate(line, m.width))
	}
	return strings.Join(lines, "\n")
}
