package tui

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mdukat/foliarz/internal/agent"
	"github.com/mdukat/foliarz/internal/session"
	"github.com/mdukat/foliarz/internal/thinking"
)

type fakeStore struct {
	sessions []*session.Session
	saved    []*session.Session
	loadErr  error
	listErr  error
	saveErr  error
}

func (f *fakeStore) Save(sess *session.Session) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	copySession := *sess
	copySession.Messages = append([]session.Message(nil), sess.Messages...)
	copySession.UpdatedAt = time.Now().UTC()
	f.saved = append(f.saved, &copySession)
	return nil
}

func (f *fakeStore) List() ([]*session.Session, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.sessions, nil
}

func (f *fakeStore) Load(id string) (*session.Session, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	for _, saved := range f.sessions {
		if saved.ID == id {
			copySession := *saved
			copySession.Messages = append([]session.Message(nil), saved.Messages...)
			copySession.Mode = session.ModeFolia
			return &copySession, nil
		}
	}
	return nil, errors.New("session not found")
}

func TestSessionAutosaveOnSubmitAndDone(t *testing.T) {
	store := &fakeStore{}
	fake := &fakeResponder{messages: []session.Message{{Role: "assistant", Content: "answer"}}}
	sess := session.New("/tmp/work")
	model := New(sess, WithSessionStore(store), WithResponder(fake))
	model.input.SetValue("question")

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if len(store.saved) == 0 {
		t.Fatal("session was not saved on submit")
	}
	if got := store.saved[len(store.saved)-1]; len(got.Messages) != 1 || got.Messages[0].Content != "question" {
		t.Errorf("saved session after submit = %+v", got.Messages)
	}

	updated, _ = model.Update(cmd())
	model = updated.(Model)
	for model.busy {
		msg := waitForStream(model.stream)()
		updated, _ = model.Update(msg)
		model = updated.(Model)
	}
	last := store.saved[len(store.saved)-1]
	if len(last.Messages) != 2 || last.Messages[1].Content != "answer" {
		t.Errorf("saved session after response = %+v", last.Messages)
	}
}

func TestSessionAutosavePreservesStreamedContent(t *testing.T) {
	store := &fakeStore{}
	sess := session.New("/tmp/work")
	sess.Messages = []session.Message{{Role: "user", Content: "question"}}
	model := New(sess, WithSessionStore(store))
	model.busy = true
	model.streamContent = "partial answer"

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = updated.(Model)

	if len(store.saved) == 0 {
		t.Fatal("session was not saved on ctrl+c")
	}
	last := store.saved[len(store.saved)-1]
	if len(last.Messages) != 2 || last.Messages[1].Content != "partial answer" {
		t.Errorf("saved session = %+v", last.Messages)
	}
}

func TestSessionPickerShowsSessions(t *testing.T) {
	olderDir := t.TempDir()
	newerDir := t.TempDir()
	older := session.New(olderDir)
	older.Messages = []session.Message{{Role: "user", Content: "older prompt"}}
	older.UpdatedAt = time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)
	newer := session.New(newerDir)
	newer.Messages = []session.Message{{Role: "user", Content: "newer prompt"}}
	newer.UpdatedAt = time.Date(2026, 2, 3, 4, 5, 0, 0, time.UTC)

	store := &fakeStore{sessions: []*session.Session{older, newer}}
	model := New(session.New("/tmp/work"), WithSessionStore(store))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = updated.(Model)
	model.input.SetValue("/session")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.picker == nil {
		t.Fatal("/session did not open the picker")
	}
	view := model.View().Content
	for _, want := range []string{"sessions", older.ID, newer.ID, olderDir, newerDir, "older prompt", "newer prompt", "2026-01-02", "2026-02-03"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker does not contain %q:\n%s", want, view)
		}
	}
	if strings.Index(view, newer.ID) > strings.Index(view, older.ID) {
		t.Errorf("sessions are not sorted newest first:\n%s", view)
	}
}

func TestSessionPickerEscapeCancels(t *testing.T) {
	store := &fakeStore{sessions: []*session.Session{session.New("/tmp/other")}}
	model := New(session.New("/tmp/work"), WithSessionStore(store))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(Model)
	model.input.SetValue("/session")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.picker == nil {
		t.Fatal("picker did not open")
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = updated.(Model)
	if model.picker != nil {
		t.Error("esc did not close the picker")
	}
	if view := model.View().Content; strings.Contains(view, "esc cancel") {
		t.Errorf("picker is still visible:\n%s", view)
	}
}

func TestSessionPickerOpensSelectedSession(t *testing.T) {
	t.Chdir(t.TempDir())
	targetDir := t.TempDir()
	target := session.New(targetDir)
	target.Messages = []session.Message{
		{Role: "user", Content: "target question"},
		{Role: "assistant", Content: "target answer"},
	}
	target.UpdatedAt = time.Now().UTC()

	store := &fakeStore{sessions: []*session.Session{target}}
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	var newSessionDir string
	model := New(session.New(originalDir),
		WithSessionStore(store),
		WithProjectContextLoader(func(dir string) (string, error) {
			return "context for " + dir, nil
		}),
		WithNewSession(func(dir string) *session.Session {
			newSessionDir = dir
			return session.New(dir)
		}),
	)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(Model)
	model.input.SetValue("/session")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.session.ID != target.ID {
		t.Errorf("session ID = %q, want %q", model.session.ID, target.ID)
	}
	if model.session.WorkDir != targetDir {
		t.Errorf("work dir = %q, want %q", model.session.WorkDir, targetDir)
	}
	if model.session.Mode != session.ModeFolia {
		t.Errorf("mode = %q, want FOLIA", model.session.Mode)
	}
	if model.session.ProjectContext != "context for "+targetDir {
		t.Errorf("project context = %q", model.session.ProjectContext)
	}
	if cwd, _ := os.Getwd(); cwd != targetDir {
		t.Errorf("process cwd = %q, want %q", cwd, targetDir)
	}
	if view := stripANSI(model.View().Content); !strings.Contains(view, "target answer") {
		t.Errorf("opened session transcript is not shown:\n%s", view)
	}

	// A new session after switching uses the opened session's directory.
	model.input.SetValue("/new")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if newSessionDir != targetDir {
		t.Errorf("new session dir = %q, want %q", newSessionDir, targetDir)
	}
}

func TestSessionPickerBlockedWhileBusy(t *testing.T) {
	store := &fakeStore{}
	model := New(session.New("/tmp/work"), WithSessionStore(store))
	model.busy = true
	model.input.SetValue("/session")
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.picker != nil {
		t.Error("picker opened while a response was running")
	}
	if !strings.Contains(model.lastErr, "cannot switch sessions") {
		t.Errorf("lastErr = %q", model.lastErr)
	}
}

func TestSessionPickerLoadError(t *testing.T) {
	t.Chdir(t.TempDir())
	dir := t.TempDir()
	target := session.New(dir)
	target.Messages = []session.Message{{Role: "user", Content: "question"}}
	target.UpdatedAt = time.Now().UTC()
	store := &fakeStore{sessions: []*session.Session{target}, loadErr: errors.New("boom")}
	model := New(session.New("/tmp/work"), WithSessionStore(store))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(Model)
	model.input.SetValue("/session")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.picker != nil {
		t.Error("picker did not close on load error")
	}
	if !strings.Contains(model.lastErr, "load session") {
		t.Errorf("lastErr = %q", model.lastErr)
	}
}

func TestSessionSwitchRebindsResponder(t *testing.T) {
	t.Chdir(t.TempDir())
	targetDir := t.TempDir()
	target := session.New(targetDir)
	target.Provider = "other-provider"
	target.Model = "other-model"
	target.Messages = []session.Message{{Role: "user", Content: "question"}}
	target.UpdatedAt = time.Now().UTC()
	store := &fakeStore{sessions: []*session.Session{target}}

	rebound := &fakeResponder{}
	var factoryProvider, factoryModel string
	model := New(session.New(t.TempDir()),
		WithSessionStore(store),
		WithProjectContextLoader(func(string) (string, error) { return "", nil }),
		WithResponderFactory(func(providerID, modelID string) (Responder, []string, error) {
			factoryProvider = providerID
			factoryModel = modelID
			return rebound, []string{thinking.Off, thinking.Low}, nil
		}),
	)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(Model)
	model.input.SetValue("/session")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if factoryProvider != "other-provider" || factoryModel != "other-model" {
		t.Errorf("factory called with %q/%q", factoryProvider, factoryModel)
	}
	if model.responder != rebound {
		t.Error("responder was not rebound to the selected session model")
	}
	if len(model.thinkingLevels) != 2 || model.thinkingLevels[1] != thinking.Low {
		t.Errorf("thinking levels = %v", model.thinkingLevels)
	}
}

func TestSessionSwitchFactoryErrorAborts(t *testing.T) {
	t.Chdir(t.TempDir())
	targetDir := t.TempDir()
	target := session.New(targetDir)
	target.Messages = []session.Message{{Role: "user", Content: "question"}}
	target.UpdatedAt = time.Now().UTC()
	store := &fakeStore{sessions: []*session.Session{target}}

	current := session.New(t.TempDir())
	model := New(current,
		WithSessionStore(store),
		WithProjectContextLoader(func(string) (string, error) { return "", nil }),
		WithResponderFactory(func(providerID, modelID string) (Responder, []string, error) {
			return nil, nil, errors.New("model unavailable")
		}),
	)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(Model)
	model.input.SetValue("/session")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.session.ID != current.ID {
		t.Error("session switched despite factory error")
	}
	if !strings.Contains(model.lastErr, "model unavailable") {
		t.Errorf("lastErr = %q", model.lastErr)
	}
}

func TestNewSessionProjectContextErrorKeepsSession(t *testing.T) {
	current := session.New(t.TempDir())
	model := New(current,
		WithNewSession(func(dir string) *session.Session { return session.New(dir) }),
		WithProjectContextLoader(func(string) (string, error) { return "", errors.New("bad AGENTS.md") }),
	)
	model.input.SetValue("/new")
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.session.ID != current.ID {
		t.Error("session was replaced after a project context error")
	}
	if !strings.Contains(model.lastErr, "bad AGENTS.md") {
		t.Errorf("lastErr = %q", model.lastErr)
	}
}

func TestModelPickerSwitch(t *testing.T) {
	sess := session.New(t.TempDir())
	sess.Provider = "openrouter"
	sess.Model = "deepseek/deepseek-v4.1-flash"
	sess.ContextWindow = 1048576

	switched := &fakeResponder{}
	var gotProvider, gotModel, gotLevel string
	model := New(sess,
		WithModels([]ModelChoice{
			{Provider: "openrouter", ID: "deepseek/deepseek-v4.1-flash", Name: "Flash", ContextWindow: 1048576},
			{Provider: "openrouter", ID: "deepseek/deepseek-v4.1-flash-1017", Name: "Flash 1017", ContextWindow: 1048576},
		}),
		WithResponderFactory(func(providerID, modelID string) (Responder, []string, error) {
			gotProvider = providerID
			gotModel = modelID
			return switched, []string{thinking.Off, thinking.High}, nil
		}),
		WithSettingsChanged(func(providerID, modelID, level string) error {
			gotProvider, gotModel, gotLevel = providerID, modelID, level
			return nil
		}),
	)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = updated.(Model)
	model.input.SetValue("/model")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.modelPicker == nil {
		t.Fatal("/model did not open the picker")
	}
	view := model.View().Content
	if !strings.Contains(view, "deepseek/deepseek-v4.1-flash") || !strings.Contains(view, "deepseek/deepseek-v4.1-flash-1017") {
		t.Fatalf("model picker does not list models:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.session.Model != "deepseek/deepseek-v4.1-flash-1017" {
		t.Errorf("session model = %q", model.session.Model)
	}
	if model.responder != switched {
		t.Error("responder was not switched")
	}
	if gotProvider != "openrouter" || gotModel != "deepseek/deepseek-v4.1-flash-1017" {
		t.Errorf("settings callback = %q/%q", gotProvider, gotModel)
	}
	if gotLevel != sess.ThinkingLevel {
		t.Errorf("settings level = %q", gotLevel)
	}
	if model.modelPicker != nil {
		t.Error("model picker did not close")
	}
}

func TestModelPickerEscapeCancels(t *testing.T) {
	sess := session.New(t.TempDir())
	sess.Provider = "openrouter"
	sess.Model = "deepseek/deepseek-v4.1-flash"
	model := New(sess, WithModels([]ModelChoice{{Provider: "openrouter", ID: "deepseek/deepseek-v4.1-flash"}}))
	model.input.SetValue("/model")
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if model.modelPicker == nil {
		t.Fatal("model picker did not open")
	}
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	model = updated.(Model)
	if model.modelPicker != nil {
		t.Error("esc did not close the model picker")
	}
}

func TestModelPickerFactoryErrorRollsBack(t *testing.T) {
	sess := session.New(t.TempDir())
	sess.Provider = "openrouter"
	sess.Model = "deepseek/deepseek-v4.1-flash"
	model := New(sess,
		WithModels([]ModelChoice{
			{Provider: "openrouter", ID: "deepseek/deepseek-v4.1-flash"},
			{Provider: "openrouter", ID: "other"},
		}),
		WithResponderFactory(func(providerID, modelID string) (Responder, []string, error) {
			return nil, nil, errors.New("model unavailable")
		}),
	)
	model.input.SetValue("/model")
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.session.Model != "deepseek/deepseek-v4.1-flash" {
		t.Errorf("model changed despite factory error: %q", model.session.Model)
	}
	if !strings.Contains(model.lastErr, "model unavailable") {
		t.Errorf("lastErr = %q", model.lastErr)
	}
}

func TestSessionSwitchKeepsSettingsError(t *testing.T) {
	t.Chdir(t.TempDir())
	targetDir := t.TempDir()
	target := session.New(targetDir)
	target.Messages = []session.Message{{Role: "user", Content: "question"}}
	target.UpdatedAt = time.Now().UTC()
	store := &fakeStore{sessions: []*session.Session{target}}

	model := New(session.New(t.TempDir()),
		WithSessionStore(store),
		WithProjectContextLoader(func(string) (string, error) { return "", nil }),
		WithSettingsChanged(func(string, string, string) error { return errors.New("disk full") }),
	)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(Model)
	model.input.SetValue("/session")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.session.ID != target.ID {
		t.Error("session did not switch")
	}
	if !strings.Contains(model.lastErr, "save settings: disk full") {
		t.Errorf("lastErr = %q", model.lastErr)
	}
}

type responderWithCompactor struct {
	*fakeResponder
	compactor *fakeCompactor
}

func (r *responderWithCompactor) Compact(ctx context.Context, input agent.CompactionInput) (agent.CompactionResult, error) {
	return r.compactor.Compact(ctx, input)
}

func TestSessionSwitchDirectoryFailureRestoresCompactor(t *testing.T) {
	t.Chdir(t.TempDir())
	targetDir := t.TempDir()
	target := session.New(targetDir)
	target.Messages = []session.Message{{Role: "user", Content: "question"}}
	target.UpdatedAt = time.Now().UTC()
	if err := os.RemoveAll(targetDir); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{sessions: []*session.Session{target}}

	current := session.New(t.TempDir())
	oldCompactor := &fakeCompactor{}
	oldResponder := &fakeResponder{}
	newCompactor := &fakeCompactor{}
	newResponder := &responderWithCompactor{fakeResponder: &fakeResponder{}, compactor: newCompactor}

	model := New(current,
		WithSessionStore(store),
		WithProjectContextLoader(func(string) (string, error) { return "", nil }),
		WithResponder(oldResponder),
		WithCompactor(oldCompactor),
		WithResponderFactory(func(providerID, modelID string) (Responder, []string, error) {
			return newResponder, nil, nil
		}),
	)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(Model)
	model.input.SetValue("/session")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.session.ID != current.ID {
		t.Error("session switched despite directory error")
	}
	if model.compactor != oldCompactor {
		t.Error("compactor was not restored after directory error")
	}
	if model.responder != oldResponder {
		t.Error("responder was not restored after directory error")
	}
	if !strings.Contains(model.lastErr, "change directory") {
		t.Errorf("lastErr = %q", model.lastErr)
	}
}

func TestModelSwitchClampsThinkingLevel(t *testing.T) {
	sess := session.New(t.TempDir())
	sess.Provider = "openrouter"
	sess.Model = "wide"
	sess.ThinkingLevel = thinking.Max

	var savedLevel string
	model := New(sess,
		WithModels([]ModelChoice{
			{Provider: "openrouter", ID: "wide"},
			{Provider: "openrouter", ID: "narrow"},
		}),
		WithResponderFactory(func(providerID, modelID string) (Responder, []string, error) {
			return &fakeResponder{}, []string{thinking.Off, thinking.Low}, nil
		}),
		WithSettingsChanged(func(providerID, modelID, level string) error {
			savedLevel = level
			return nil
		}),
	)
	model.input.SetValue("/model")
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.session.ThinkingLevel != thinking.Off {
		t.Errorf("ThinkingLevel = %q, want off", model.session.ThinkingLevel)
	}
	if savedLevel != thinking.Off {
		t.Errorf("saved level = %q, want off", savedLevel)
	}
}

func TestSessionLoadClampsThinkingLevel(t *testing.T) {
	t.Chdir(t.TempDir())
	dir := t.TempDir()
	target := session.New(dir)
	target.Provider = "openrouter"
	target.Model = "narrow"
	target.ThinkingLevel = thinking.Max
	target.Messages = []session.Message{{Role: "user", Content: "question"}}
	target.UpdatedAt = time.Now().UTC()
	store := &fakeStore{sessions: []*session.Session{target}}

	model := New(session.New(t.TempDir()),
		WithSessionStore(store),
		WithProjectContextLoader(func(string) (string, error) { return "", nil }),
		WithResponderFactory(func(providerID, modelID string) (Responder, []string, error) {
			return &fakeResponder{}, []string{thinking.Off, thinking.Low}, nil
		}),
	)
	model.input.SetValue("/session")
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.session.ThinkingLevel != thinking.Off {
		t.Errorf("ThinkingLevel = %q, want off", model.session.ThinkingLevel)
	}
}
