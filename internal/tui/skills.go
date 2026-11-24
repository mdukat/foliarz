package tui

import (
	"log/slog"
	"strings"

	"charm.land/bubbletea/v2"

	"github.com/mdukat/foliarz/internal/session"
	"github.com/mdukat/foliarz/internal/skills"
)

// handleSkillCommand handles /skill:<name> [request] input.
func (m *Model) handleSkillCommand() (bool, tea.Cmd) {
	value := strings.TrimSpace(m.input.Value())
	if !strings.HasPrefix(value, "/skill:") {
		return false, nil
	}

	if m.busy {
		m.lastErr = "cannot run a skill while a response is running"
		m.invalidateChat()
		return true, nil
	}

	rest := strings.TrimPrefix(value, "/skill:")
	name, arguments, _ := strings.Cut(rest, " ")
	name = strings.TrimSpace(name)
	arguments = strings.TrimSpace(arguments)

	var selected *skills.Skill
	for i := range m.skills {
		if m.skills[i].Name == name {
			selected = &m.skills[i]
			break
		}
	}
	if selected == nil {
		m.lastErr = "unknown skill: " + name
		m.input.Reset()
		m.invalidateChat()
		return true, nil
	}

	if m.mode() == session.ModeFolia && selected.RequiresShell {
		slog.Warn("skill blocked in FOLIA mode", "skill", name)
		m.lastErr = "skill " + name + " requires shell and cannot run in FOLIA mode; switch to NORMAL or YOLO with /normal or /yolo"
		m.input.Reset()
		m.invalidateChat()
		return true, nil
	}

	prompt := selected.InvocationPrompt(arguments)
	m.maybeCompact()
	m.session.Messages = append(m.session.Messages, session.Message{Role: "user", Content: prompt})
	m.input.Reset()
	m.lastErr = ""
	m.showInfo = false
	m.invalidateTranscript()
	m.invalidateChat()
	m.autosave(true)

	if m.responder == nil {
		return true, nil
	}
	return true, m.startResponse()
}
