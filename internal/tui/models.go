package tui

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
)

// ModelChoice is a selectable provider/model pair.
type ModelChoice struct {
	Provider      string
	ID            string
	Name          string
	ContextWindow int
}

type modelPicker struct {
	models   []ModelChoice
	selected int
}

// openModelPicker shows the model selection window.
func (m *Model) openModelPicker() {
	if m.busy {
		m.lastErr = "cannot switch models while a response is running"
		m.invalidateChat()
		return
	}
	if len(m.models) == 0 {
		m.lastErr = "no models are configured"
		m.invalidateChat()
		return
	}

	selected := 0
	for i, choice := range m.models {
		if choice.Provider == m.session.Provider && choice.ID == m.session.Model {
			selected = i
			break
		}
	}
	m.modelPicker = &modelPicker{models: slices.Clone(m.models), selected: selected}
	m.lastErr = ""
	m.invalidateChat()
}

func (m *Model) moveModelSelection(delta int) {
	if m.modelPicker == nil || len(m.modelPicker.models) == 0 {
		return
	}
	count := len(m.modelPicker.models)
	m.modelPicker.selected = (m.modelPicker.selected + delta + count) % count
}

func (m *Model) closeModelPicker() {
	m.modelPicker = nil
	m.invalidateChat()
}

// openSelectedModel switches the current session to the selected model.
func (m *Model) openSelectedModel() {
	if m.modelPicker == nil || len(m.modelPicker.models) == 0 {
		return
	}
	choice := m.modelPicker.models[m.modelPicker.selected]

	oldProvider := m.session.Provider
	oldModel := m.session.Model
	oldWindow := m.session.ContextWindow
	oldResponder := m.responder
	oldLevels := m.thinkingLevels
	oldCompactor := m.compactor

	m.session.Provider = choice.Provider
	m.session.Model = choice.ID
	m.session.ContextWindow = choice.ContextWindow
	if err := m.bindSession(m.session); err != nil {
		m.session.Provider = oldProvider
		m.session.Model = oldModel
		m.session.ContextWindow = oldWindow
		m.responder = oldResponder
		m.thinkingLevels = oldLevels
		m.compactor = oldCompactor
		slog.Error("model switch failed", "session", m.session.ID, "provider", choice.Provider, "model", choice.ID, "error", err)
		m.closeModelPicker()
		m.lastErr = "switch model: " + err.Error()
		m.invalidateChat()
		return
	}

	m.modelPicker = nil
	m.pendingCompaction = nil
	if m.settingsChanged != nil {
		if err := m.settingsChanged(choice.Provider, choice.ID, m.session.ThinkingLevel); err != nil {
			slog.Error("save last-used settings failed", "error", err)
			m.lastErr = "save settings: " + err.Error()
		}
	}
	slog.Info("model switched", "session", m.session.ID, "provider", choice.Provider, "model", choice.ID)
	m.autosave(true)
	m.invalidateInfo()
	m.invalidateChat()
}

// viewModelPicker renders the model selection window.
func (m Model) viewModelPicker(height int) string {
	if height <= 0 {
		return ""
	}
	lines := []string{
		truncate(roleStyle.Render("models")+" "+versionStyle.Render("esc cancel · enter select"), m.width),
		"",
	}
	if m.modelPicker == nil || len(m.modelPicker.models) == 0 {
		lines = append(lines, versionStyle.Render("no models configured"))
		return lipgloss.Place(m.width, height, lipgloss.Left, lipgloss.Top, strings.Join(lines, "\n"))
	}

	perPage := height - 2
	if perPage < 1 {
		perPage = 1
	}
	start := 0
	if m.modelPicker.selected >= perPage {
		start = m.modelPicker.selected - perPage + 1
	}

	for i := start; i < len(m.modelPicker.models) && i < start+perPage; i++ {
		choice := m.modelPicker.models[i]
		prefix := "  "
		if i == m.modelPicker.selected {
			prefix = "> "
		}
		line := fmt.Sprintf("%s/%s", choice.Provider, choice.ID)
		if choice.Name != "" && choice.Name != choice.ID {
			line += " — " + choice.Name
		}
		if choice.ContextWindow > 0 {
			line += " · " + formatTokens(choice.ContextWindow) + " context"
		}
		if choice.Provider == m.session.Provider && choice.ID == m.session.Model {
			line += " (current)"
		}
		line = truncate(prefix+line, m.width)
		if i == m.modelPicker.selected {
			line = selectedSessionStyle.Render(line)
		}
		lines = append(lines, line)
	}

	return lipgloss.Place(m.width, height, lipgloss.Left, lipgloss.Top, strings.Join(lines, "\n"))
}
