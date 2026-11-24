package tui

import (
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/mdukat/foliarz/internal/session"
)

var (
	statusBarStyle  = lipgloss.NewStyle().Faint(true)
	foliaModeStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	normalModeStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	yoloModeStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
)

// statusBar renders the bottom line of the TUI: token usage on the left, and
// working directory, model and thinking level on the right.
func (m Model) statusBar() string {
	mode := m.mode()
	modeStyle := foliaModeStyle
	switch mode {
	case session.ModeNormal:
		modeStyle = normalModeStyle
	case session.ModeYolo:
		modeStyle = yoloModeStyle
	}

	left := modeStyle.Render(mode) + "  tokens " + formatTokens(m.session.TokensUsed)
	if m.session.ContextWindow > 0 {
		left += "/" + formatTokens(m.session.ContextWindow)
	}
	if m.session.CostUSD > 0 {
		left += "  " + formatCost(m.session.CostUSD)
	}

	right := strings.Join([]string{
		abbreviateHome(m.session.WorkDir),
		"model " + m.session.Model,
		"thinking " + m.session.ThinkingLevel,
	}, " · ")

	line := left
	if gap := m.width - lipgloss.Width(left) - lipgloss.Width(right); gap > 0 {
		line += strings.Repeat(" ", gap) + right
	} else {
		line += " · " + right
	}
	return statusBarStyle.Render(truncate(line, m.width))
}

// formatCost renders spend in US dollars with three decimals, like Pi Agent.
func formatCost(cost float64) string {
	return fmt.Sprintf("$%.3f", cost)
}

// formatTokens renders a token count in a compact human-readable form.
func formatTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return trimZero(float64(n)/1_000_000) + "M"
	case n >= 1_000:
		return trimZero(float64(n)/1_000) + "k"
	default:
		return fmt.Sprintf("%d", n)
	}
}

func trimZero(f float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", f), ".0")
}

// abbreviateHome replaces the user's home directory prefix with "~".
func abbreviateHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// truncate cuts s to at most width terminal cells.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}
