package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// ruleStyle colors the horizontal rules framing the input bar.
var ruleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))

// inputBar renders the text input as a bar framed by horizontal rules, like the
// Pi agent's editor.
func (m Model) inputBar() string {
	if m.width <= 0 {
		return ""
	}

	rule := ruleStyle.Render(strings.Repeat("─", m.width))
	return lipgloss.JoinVertical(lipgloss.Left, rule, m.input.View(), rule)
}
