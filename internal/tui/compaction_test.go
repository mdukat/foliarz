package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mdukat/foliarz/internal/agent"
	"github.com/mdukat/foliarz/internal/session"
)

type fakeCompactor struct {
	calls  int
	input  agent.CompactionInput
	result agent.CompactionResult
	err    error
}

func (f *fakeCompactor) Compact(ctx context.Context, input agent.CompactionInput) (agent.CompactionResult, error) {
	f.calls++
	f.input = input
	return f.result, f.err
}

func TestAutoCompactionTriggersOverThreshold(t *testing.T) {
	compactor := &fakeCompactor{result: agent.CompactionResult{
		Summary:              "SUMMARY",
		FirstKeptMessage:     1,
		TokensBefore:         50000,
		EstimatedTokensAfter: 100,
	}}
	sess := session.New("/tmp/work")
	sess.Messages = []session.Message{{Role: "user", Content: "old question"}}
	sess.ContextWindow = 60000
	sess.TokensUsed = 50000
	model := New(sess,
		WithCompactor(compactor),
		WithCompaction(CompactionSettings{Enabled: true, ReserveTokens: 16384, KeepRecentTokens: 20000}),
	)
	model.input.SetValue("new question")

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if compactor.calls != 1 {
		t.Fatalf("compaction calls = %d, want 1", compactor.calls)
	}
	if model.session.Summary != "SUMMARY" {
		t.Errorf("Summary = %q", model.session.Summary)
	}
	if model.session.FirstKeptMessage != 1 {
		t.Errorf("FirstKeptMessage = %d", model.session.FirstKeptMessage)
	}
	if model.session.TokensUsed != 100 {
		t.Errorf("TokensUsed = %d, want 100", model.session.TokensUsed)
	}
	if len(model.session.Messages) != 2 || model.session.Messages[1].Content != "new question" {
		t.Errorf("messages = %+v", model.session.Messages)
	}
	if model.lastErr != "" {
		t.Errorf("lastErr = %q", model.lastErr)
	}
}

func TestAutoCompactionSkipsBelowThreshold(t *testing.T) {
	compactor := &fakeCompactor{}
	sess := session.New("/tmp/work")
	sess.ContextWindow = 60000
	sess.TokensUsed = 30000
	model := New(sess,
		WithCompactor(compactor),
		WithCompaction(CompactionSettings{Enabled: true, ReserveTokens: 16384, KeepRecentTokens: 20000}),
	)
	model.input.SetValue("question")
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if compactor.calls != 0 {
		t.Errorf("compaction calls = %d, want 0", compactor.calls)
	}
}

func TestAutoCompactionDisabled(t *testing.T) {
	compactor := &fakeCompactor{}
	sess := session.New("/tmp/work")
	sess.ContextWindow = 60000
	sess.TokensUsed = 50000
	model := New(sess,
		WithCompactor(compactor),
		WithCompaction(CompactionSettings{Enabled: false, ReserveTokens: 16384, KeepRecentTokens: 20000}),
	)
	model.input.SetValue("question")
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if compactor.calls != 0 {
		t.Errorf("compaction calls = %d, want 0", compactor.calls)
	}
}

func TestCompactCommand(t *testing.T) {
	compactor := &fakeCompactor{result: agent.CompactionResult{
		Summary:              "MANUAL SUMMARY",
		FirstKeptMessage:     1,
		EstimatedTokensAfter: 50,
	}}
	sess := session.New("/tmp/work")
	sess.Messages = []session.Message{{Role: "user", Content: "question"}}
	model := New(sess, WithCompactor(compactor))
	model.input.SetValue("/compact")
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if compactor.calls != 1 {
		t.Fatalf("compaction calls = %d, want 1", compactor.calls)
	}
	if model.session.Summary != "MANUAL SUMMARY" {
		t.Errorf("Summary = %q", model.session.Summary)
	}
	if model.lastErr != "" {
		t.Errorf("lastErr = %q", model.lastErr)
	}
}
