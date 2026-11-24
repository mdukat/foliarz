package tui

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/mdukat/foliarz/internal/session"
)

const sessionAutosaveInterval = 500 * time.Millisecond

var selectedSessionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))

// autosave writes the session to disk. When a response is streaming, the
// snapshot includes the partial streamed messages, so a crash keeps them.
func (m *Model) autosave(force bool) {
	if m.store == nil {
		return
	}
	if !force && time.Since(m.lastSave) < sessionAutosaveInterval {
		return
	}

	target := m.session
	if m.busy || len(m.streamItems) > 0 || m.streamContent != "" || m.streamThinking != "" {
		target = m.sessionSnapshot()
	}
	if len(target.Messages) == 0 {
		return
	}
	if err := m.store.Save(target); err != nil {
		slog.Error("session save failed", "session", target.ID, "error", err)
		m.lastErr = "save session: " + err.Error()
		m.invalidateChat()
		return
	}
	m.lastSave = time.Now()
}

// sessionSnapshot returns a copy of the session including streamed messages.
func (m Model) sessionSnapshot() *session.Session {
	snapshot := *m.session
	snapshot.Messages = append([]session.Message(nil), m.session.Messages...)
	snapshot.Messages = append(snapshot.Messages, m.streamedMessages()...)
	return &snapshot
}

// streamedMessages converts the current streaming timeline to session
// messages without modifying the model.
func (m Model) streamedMessages() []session.Message {
	var messages []session.Message
	for _, item := range m.streamItems {
		switch {
		case item.message != nil:
			if strings.TrimSpace(item.message.Content) != "" || strings.TrimSpace(item.message.Thinking) != "" {
				messages = append(messages, *item.message)
			}
		case item.tool != nil && item.tool.result != nil:
			messages = append(messages,
				session.Message{Role: "assistant", ToolCalls: []session.ToolCall{item.tool.call}},
				*item.tool.result,
			)
		}
	}

	content := strings.TrimRight(m.streamContent, "\n")
	thinking := strings.TrimRight(m.streamThinking, "\n")
	if strings.TrimSpace(content) != "" || strings.TrimSpace(thinking) != "" {
		messages = append(messages, session.Message{
			Role:     "assistant",
			Content:  content,
			Thinking: thinking,
		})
	}
	return messages
}

// openSessionPicker loads the saved sessions and shows the picker.
func (m *Model) openSessionPicker() {
	if m.busy {
		m.lastErr = "cannot switch sessions while a response is running"
		m.invalidateChat()
		return
	}
	if m.store == nil {
		m.lastErr = "session store is not configured"
		m.invalidateChat()
		return
	}

	m.autosave(true)
	sessions, err := m.store.List()
	if err != nil {
		m.lastErr = "list sessions: " + err.Error()
		m.invalidateChat()
		return
	}
	filtered := sessions[:0]
	for _, saved := range sessions {
		if len(saved.Messages) > 0 {
			filtered = append(filtered, saved)
		}
	}
	sessions = filtered
	sort.SliceStable(sessions, func(i, j int) bool {
		if !sessions[i].UpdatedAt.Equal(sessions[j].UpdatedAt) {
			return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
		}
		return sessions[i].ID < sessions[j].ID
	})

	m.picker = &sessionPicker{sessions: sessions}
	m.lastErr = ""
	m.invalidateChat()
}

func (m *Model) moveSessionSelection(delta int) {
	if m.picker == nil || len(m.picker.sessions) == 0 {
		return
	}
	count := len(m.picker.sessions)
	m.picker.selected = (m.picker.selected + delta + count) % count
}

func (m *Model) closeSessionPicker() {
	m.picker = nil
	m.invalidateChat()
}

// openSelectedSession loads the selected session, changes the working
// directory and replaces the current session.
func (m *Model) openSelectedSession() {
	if m.picker == nil || len(m.picker.sessions) == 0 {
		return
	}
	selected := m.picker.sessions[m.picker.selected]

	m.autosave(true)
	loaded, err := m.store.Load(selected.ID)
	if err != nil {
		slog.Error("session load failed", "session", selected.ID, "error", err)
		m.closeSessionPicker()
		m.lastErr = "load session: " + err.Error()
		m.invalidateChat()
		return
	}

	if m.projectLoader != nil {
		projectContext, err := m.projectLoader(loaded.WorkDir)
		if err != nil {
			slog.Error("project context load failed", "session", loaded.ID, "error", err)
			m.closeSessionPicker()
			m.lastErr = "load project context: " + err.Error()
			m.invalidateChat()
			return
		}
		loaded.ProjectContext = projectContext
	}

	previousResponder := m.responder
	previousLevels := m.thinkingLevels
	previousCompactor := m.compactor
	if err := m.bindSession(loaded); err != nil {
		slog.Error("session model binding failed", "session", loaded.ID, "error", err)
		m.closeSessionPicker()
		m.lastErr = "switch model: " + err.Error()
		m.invalidateChat()
		return
	}

	if err := os.Chdir(loaded.WorkDir); err != nil {
		slog.Error("session directory change failed", "session", loaded.ID, "workDir", loaded.WorkDir, "error", err)
		m.responder = previousResponder
		m.thinkingLevels = previousLevels
		m.compactor = previousCompactor
		m.closeSessionPicker()
		m.lastErr = "change directory: " + err.Error()
		m.invalidateChat()
		return
	}

	m.stopResponse()
	m.requestID++
	m.busy = false
	m.stream = nil
	m.streamThinking = ""
	m.streamContent = ""
	m.streamItems = nil
	m.pendingApproval = nil
	m.pendingCompaction = nil
	m.session = loaded
	m.lastErr = ""
	if m.settingsChanged != nil {
		if err := m.settingsChanged(loaded.Provider, loaded.Model, loaded.ThinkingLevel); err != nil {
			slog.Error("save last-used settings failed", "error", err)
			m.lastErr = "save settings: " + err.Error()
		}
	}
	slog.Info("session opened", "session", loaded.ID, "workDir", loaded.WorkDir, "model", loaded.Model)
	m.picker = nil
	m.showInfo = false
	m.follow = true
	m.scrollTop = 0
	m.lastSave = time.Now()
	m.input.Reset()
	m.invalidateTranscript()
	m.invalidateInfo()
	m.invalidateChat()
}

// viewSessionPicker renders the session selection window.
func (m Model) viewSessionPicker(height int) string {
	if height <= 0 {
		return ""
	}

	lines := []string{
		truncate(roleStyle.Render("sessions")+" "+versionStyle.Render("esc cancel · enter open"), m.width),
		"",
	}
	if m.picker == nil || len(m.picker.sessions) == 0 {
		lines = append(lines, versionStyle.Render("no saved sessions"))
		return lipgloss.Place(m.width, height, lipgloss.Left, lipgloss.Top, strings.Join(lines, "\n"))
	}

	perPage := (height - 2) / 2
	if perPage < 1 {
		perPage = 1
	}
	start := 0
	if m.picker.selected >= perPage {
		start = m.picker.selected - perPage + 1
	}

	for i := start; i < len(m.picker.sessions) && i < start+perPage; i++ {
		saved := m.picker.sessions[i]
		prefix := "  "
		if i == m.picker.selected {
			prefix = "> "
		}
		header := fmt.Sprintf("%s  %s", saved.UpdatedAt.Local().Format("2006-01-02 15:04"), saved.ID)
		details := abbreviateHome(saved.WorkDir) + " — " + sessionFirstPrompt(saved)
		header = truncate(prefix+header, m.width)
		details = truncate("  "+details, m.width)
		if i == m.picker.selected {
			header = selectedSessionStyle.Render(header)
			details = selectedSessionStyle.Render(details)
		}
		lines = append(lines, header, details)
	}

	return lipgloss.Place(m.width, height, lipgloss.Left, lipgloss.Top, strings.Join(lines, "\n"))
}

// sessionFirstPrompt returns the beginning of the first user message.
func sessionFirstPrompt(sess *session.Session) string {
	for _, message := range sess.Messages {
		if message.Role != "user" {
			continue
		}
		text := strings.TrimSpace(message.Content)
		if index := strings.IndexByte(text, '\n'); index >= 0 {
			text = text[:index]
		}
		if text == "" {
			return "(empty prompt)"
		}
		return text
	}
	return "(no prompt)"
}
