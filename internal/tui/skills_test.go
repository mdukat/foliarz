package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mdukat/foliarz/internal/session"
	"github.com/mdukat/foliarz/internal/skills"
)

func TestShellSkillBlockedInFolia(t *testing.T) {
	skill := skills.Skill{
		Name:          "web-search",
		Description:   "Search the web.",
		Body:          "Run the bundled script.",
		RequiresShell: true,
	}
	sess := session.New("/tmp/work")
	model := New(sess, WithSkills([]skills.Skill{skill}))
	model.input.SetValue("/skill:web-search find something")

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if cmd != nil {
		t.Error("blocked skill started a response")
	}
	if len(sess.Messages) != 0 {
		t.Errorf("blocked skill added messages: %+v", sess.Messages)
	}
	if !strings.Contains(model.lastErr, "requires shell and cannot run in FOLIA") {
		t.Errorf("lastErr = %q", model.lastErr)
	}
}

func TestSkillCommandRunsInYolo(t *testing.T) {
	skill := skills.Skill{
		Name:          "web-search",
		Description:   "Search the web.",
		Path:          "/skills/web-search",
		Body:          "Run the bundled script.",
		RequiresShell: true,
	}
	sess := session.New("/tmp/work")
	sess.Mode = session.ModeYolo
	fake := &fakeResponder{messages: []session.Message{{Role: "assistant", Content: "done"}}}
	model := New(sess, WithSkills([]skills.Skill{skill}), WithResponder(fake))
	model.input.SetValue("/skill:web-search find something")

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("skill command did not start a response")
	}
	if len(sess.Messages) != 1 {
		t.Fatalf("messages = %+v", sess.Messages)
	}
	content := sess.Messages[0].Content
	if !strings.Contains(content, "Run the bundled script.") || !strings.Contains(content, "User request: find something") {
		t.Errorf("skill prompt = %q", content)
	}

	updated, _ = model.Update(cmd())
	model = updated.(Model)
	for model.busy {
		msg := waitForStream(model.stream)()
		updated, _ = model.Update(msg)
		model = updated.(Model)
	}
	if len(sess.Messages) != 2 || sess.Messages[1].Content != "done" {
		t.Errorf("messages after response = %+v", sess.Messages)
	}
}

func TestReadOnlySkillAllowedInFolia(t *testing.T) {
	skill := skills.Skill{
		Name:        "notes",
		Description: "Read-only instructions.",
		Body:        "Explain the code.",
	}
	sess := session.New("/tmp/work")
	fake := &fakeResponder{messages: []session.Message{{Role: "assistant", Content: "explained"}}}
	model := New(sess, WithSkills([]skills.Skill{skill}), WithResponder(fake))
	model.input.SetValue("/skill:notes explain main.go")

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("read-only skill did not start a response")
	}
	if model.lastErr != "" {
		t.Errorf("lastErr = %q", model.lastErr)
	}
	if len(sess.Messages) != 1 || !strings.Contains(sess.Messages[0].Content, "Explain the code.") {
		t.Errorf("messages = %+v", sess.Messages)
	}
}

func TestUnknownSkill(t *testing.T) {
	model := New(session.New("/tmp/work"), WithSkills(nil))
	model.input.SetValue("/skill:missing")
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if cmd != nil {
		t.Error("unknown skill started a response")
	}
	if !strings.Contains(model.lastErr, "unknown skill") {
		t.Errorf("lastErr = %q", model.lastErr)
	}
}

func TestStartupInfoShowsSkills(t *testing.T) {
	skill := skills.Skill{Name: "notes", Description: "Read-only instructions.", RequiresShell: false}
	model := New(session.New("/tmp/work"), WithSkills([]skills.Skill{skill}))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	model = updated.(Model)

	view := model.View().Content
	if !strings.Contains(view, "skills") || !strings.Contains(view, "notes") {
		t.Errorf("startup info does not show skills:\n%s", view)
	}
}
