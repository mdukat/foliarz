package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mdukat/foliarz/internal/agent"
	"github.com/mdukat/foliarz/internal/session"
	"github.com/mdukat/foliarz/internal/thinking"
	"github.com/mdukat/foliarz/internal/tools"
)

func TestShiftTabCyclesThinkingLevel(t *testing.T) {
	sess := session.New("/tmp/work")

	for _, want := range []string{thinking.Minimal, thinking.Low, thinking.Medium, thinking.High} {
		updated, _ := New(sess).Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		sess = updated.(Model).session
		if sess.ThinkingLevel != want {
			t.Fatalf("ThinkingLevel = %q, want %q", sess.ThinkingLevel, want)
		}
	}
}

func TestShiftTabWrapsThinkingLevel(t *testing.T) {
	sess := session.New("/tmp/work")
	sess.ThinkingLevel = thinking.Max

	updated, _ := New(sess).Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if got := updated.(Model).session.ThinkingLevel; got != thinking.Off {
		t.Errorf("ThinkingLevel = %q, want %q", got, thinking.Off)
	}
}

func TestShiftTabDoesNotEditInput(t *testing.T) {
	sess := session.New("/tmp/work")
	model := New(sess)
	model.input.SetValue("abc")
	model.input.CursorEnd()

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if got := updated.(Model).input.Value(); got != "abc" {
		t.Errorf("input value = %q, want %q", got, "abc")
	}
}

func TestCtrlDQuits(t *testing.T) {
	_, cmd := New(session.New("/tmp/work")).Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+d returned nil command, want quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("ctrl+d did not return a quit message")
	}
}

func TestCtrlCStopsResponseAndSwitchesToFolia(t *testing.T) {
	sess := session.New("/tmp/work")
	fake := &fakeResponder{messages: []session.Message{{Role: "assistant", Content: "answer"}}}
	model := New(sess, WithResponder(fake))
	model.input.SetValue("question")
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if !model.busy {
		t.Fatal("busy = false after submit")
	}

	// Simulate an in-flight response with streamed text.
	model.streamThinking = "partial"
	model.streamContent = "partial answer"
	model.session.Mode = session.ModeNormal

	updated, cmd := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = updated.(Model)

	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Error("ctrl+c quit the TUI")
		}
	}
	if model.busy {
		t.Error("busy = true after ctrl+c")
	}
	if model.streamThinking != "" || model.streamContent != "" {
		t.Errorf("stream buffers not cleared: %q / %q", model.streamThinking, model.streamContent)
	}
	if model.session.Mode != session.ModeFolia {
		t.Errorf("mode = %q, want FOLIA", model.session.Mode)
	}
	if len(sess.Messages) != 2 {
		t.Fatalf("messages = %+v, want the user message and the partial answer", sess.Messages)
	}
	if sess.Messages[0].Content != "question" {
		t.Errorf("user context was removed: %+v", sess.Messages)
	}
	if sess.Messages[1].Content != "partial answer" || sess.Messages[1].Thinking != "partial" {
		t.Errorf("partial answer was not preserved: %+v", sess.Messages[1])
	}
}

func TestShiftTabUsesModelThinkingLevels(t *testing.T) {
	sess := session.New("/tmp/work")
	levels := []string{thinking.Off, thinking.Low, thinking.High, thinking.Max}

	for _, want := range []string{thinking.Low, thinking.High, thinking.Max, thinking.Off} {
		updated, _ := New(sess, WithThinkingLevels(levels)).Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		sess = updated.(Model).session
		if sess.ThinkingLevel != want {
			t.Fatalf("ThinkingLevel = %q, want %q", sess.ThinkingLevel, want)
		}
	}
}

func TestShiftEnterInsertsNewline(t *testing.T) {
	sess := session.New("/tmp/work")
	model := New(sess)
	model.input.SetValue("line one")
	model.input.CursorEnd()

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	model = updated.(Model)

	if len(sess.Messages) != 0 {
		t.Fatalf("shift+enter submitted %d messages", len(sess.Messages))
	}
	if got := model.input.Value(); got != "line one\n" {
		t.Fatalf("input = %q, want a trailing newline", got)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 't', Text: "two"})
	model = updated.(Model)
	if got := model.input.Value(); got != "line one\ntwo" {
		t.Errorf("input = %q, want %q", got, "line one\ntwo")
	}
}

func TestEnterSubmitsInput(t *testing.T) {
	sess := session.New("/tmp/work")
	model := New(sess)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	model = updated.(Model)
	model.input.SetValue("hello\nworld")

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if len(sess.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(sess.Messages))
	}
	if got := sess.Messages[0]; got.Role != "user" || got.Content != "hello\nworld" {
		t.Errorf("message = %+v", got)
	}
	if got := model.input.Value(); got != "" {
		t.Errorf("input = %q, want empty", got)
	}
	view := model.View().Content
	if !strings.Contains(view, "hello") || !strings.Contains(view, "world") {
		t.Errorf("view does not show the submitted message:\n%s", view)
	}
}

func TestEnterWithEmptyInputDoesNothing(t *testing.T) {
	sess := session.New("/tmp/work")
	model := New(sess)
	model.input.SetValue("  \n ")

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if len(sess.Messages) != 0 {
		t.Errorf("len(Messages) = %d, want 0", len(sess.Messages))
	}
	if model.input.Value() == "" {
		t.Error("empty input was cleared")
	}
}

func TestInputWrapsLongLines(t *testing.T) {
	model := New(session.New("/tmp/work"))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 20, Height: 30})
	model = updated.(Model)
	model.input.SetValue(strings.Repeat("a", 45))

	if got := model.input.Height(); got < 3 {
		t.Errorf("input height = %d, want at least 3 wrapped rows", got)
	}
}

func TestMaxInputHeight(t *testing.T) {
	tests := map[int]int{
		0:  10,
		5:  1,
		12: 7,
		40: 10,
	}
	for termHeight, want := range tests {
		if got := maxInputHeight(termHeight); got != want {
			t.Errorf("maxInputHeight(%d) = %d, want %d", termHeight, got, want)
		}
	}
}

func TestNewlineFallbackKeys(t *testing.T) {
	keys := []tea.KeyPressMsg{
		{Code: tea.KeyEnter, Mod: tea.ModAlt},
		{Code: 'j', Mod: tea.ModCtrl},
	}
	for _, key := range keys {
		sess := session.New("/tmp/work")
		model := New(sess)
		model.input.SetValue("one")
		model.input.CursorEnd()

		updated, _ := model.Update(key)
		model = updated.(Model)

		if len(sess.Messages) != 0 {
			t.Errorf("%s submitted the message", key.String())
		}
		if got := model.input.Value(); got != "one\n" {
			t.Errorf("%s input = %q, want trailing newline", key.String(), got)
		}
	}
}

func TestCommandPaletteListsCommands(t *testing.T) {
	model := New(session.New("/tmp/work"))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model = updated.(Model)
	model.input.SetValue("/")

	view := model.View().Content
	for _, want := range []string{"/exit", "/new", "/quit"} {
		if !strings.Contains(view, want) {
			t.Errorf("view does not list %s:\n%s", want, view)
		}
	}
}

func TestCommandPaletteFiltersByPrefix(t *testing.T) {
	model := New(session.New("/tmp/work"))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model = updated.(Model)
	model.input.SetValue("/q")

	view := model.View().Content
	if !strings.Contains(view, "/quit") {
		t.Errorf("view does not list /quit:\n%s", view)
	}
	if strings.Contains(view, "/exit") {
		t.Errorf("view lists /exit for query /q:\n%s", view)
	}
}

func TestEnterRunsSlashCommand(t *testing.T) {
	sess := session.New("/tmp/work")
	model := New(sess)
	model.input.SetValue("/quit")

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if cmd == nil {
		t.Fatal("enter on /quit returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("enter on /quit did not quit")
	}
	if got := model.input.Value(); got != "" {
		t.Errorf("input = %q, want empty", got)
	}
	if len(sess.Messages) != 0 {
		t.Error("slash command was submitted as a message")
	}
}

func TestEnterRunsHighlightedCommand(t *testing.T) {
	old := session.New("/tmp/work")
	old.Messages = []session.Message{{Role: "user", Content: "hello"}}

	fresh := session.New("/tmp/other")
	model := New(old, WithNewSession(func(string) *session.Session { return fresh }))
	model.input.SetValue("/")

	// Commands are sorted: compact, continue, exit, folia, model, new, normal, quit, session, yolo.
	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = updated.(Model)
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("selected command was quit, want new")
		}
	}
	if model.session != fresh {
		t.Errorf("session was not replaced")
	}
	if len(model.session.Messages) != 0 {
		t.Errorf("new session has %d messages", len(model.session.Messages))
	}
	if got := model.input.Value(); got != "" {
		t.Errorf("input = %q, want empty", got)
	}
}

func TestUnknownSlashCommandIsNotSubmitted(t *testing.T) {
	sess := session.New("/tmp/work")
	model := New(sess)
	model.input.SetValue("/does-not-exist")

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if len(sess.Messages) != 0 {
		t.Error("unknown slash command was submitted as a message")
	}
	if got := model.input.Value(); got != "/does-not-exist" {
		t.Errorf("input = %q, want unchanged", got)
	}
}

func TestSlashCommandWithArgumentsIsSubmitted(t *testing.T) {
	sess := session.New("/tmp/work")
	model := New(sess)
	model.input.SetValue("/quit now")

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = updated

	if len(sess.Messages) != 1 || sess.Messages[0].Content != "/quit now" {
		t.Errorf("messages = %+v, want submitted text", sess.Messages)
	}
}

type fakeResponder struct {
	calls          int
	history        []session.Message
	level          string
	mode           string
	workDir        string
	projectContext string
	compaction     agent.CompactionContext
	approvalCall   *session.ToolCall
	approved       bool
	approvalErr    error
	stream         []agent.Event
	gate           chan struct{}
	messages       []session.Message
	tokens         int
	cost           float64
	err            error
}

func (f *fakeResponder) Respond(ctx context.Context, history []session.Message, thinkingLevel, mode, projectContext string, compaction agent.CompactionContext, steering *agent.MessageQueue, emit func(agent.Event), approve agent.Approver) ([]session.Message, agent.Usage, error) {
	f.calls++
	if f.gate != nil {
		<-f.gate
	}
	f.history = history
	f.level = thinkingLevel
	f.mode = mode
	f.workDir = tools.WorkDir(ctx)
	f.projectContext = projectContext
	f.compaction = compaction
	for _, event := range f.stream {
		if emit != nil {
			emit(event)
		}
	}
	if f.approvalCall != nil && approve != nil {
		f.approved, f.approvalErr = approve(ctx, *f.approvalCall)
	}

	// Simulate the agent draining queued steering messages before the final
	// assistant message.
	messages := f.messages
	if steering != nil {
		if drained := steering.Drain(); len(drained) > 0 && len(messages) > 0 {
			messages = append(append(append([]session.Message{}, messages[:len(messages)-1]...), drained...), messages[len(messages)-1])
		}
	}
	return messages, agent.Usage{Tokens: f.tokens, CostUSD: f.cost}, f.err
}

func TestResponderAddsAssistantMessage(t *testing.T) {
	sess := session.New("/tmp/work")
	sess.ThinkingLevel = thinking.High

	fake := &fakeResponder{
		messages: []session.Message{{Role: "assistant", Content: "the answer"}},
		tokens:   123,
	}
	model := New(sess, WithResponder(fake))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	model = updated.(Model)
	model.input.SetValue("question")

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if !model.busy {
		t.Error("busy = false after submit")
	}
	if cmd == nil {
		t.Fatal("submit returned no command")
	}

	updated, _ = model.Update(cmd())
	model = updated.(Model)

	if model.busy {
		t.Error("busy = true after response")
	}
	if fake.calls != 1 {
		t.Errorf("calls = %d, want 1", fake.calls)
	}
	if len(fake.history) != 1 || fake.history[0].Content != "question" {
		t.Errorf("history = %+v", fake.history)
	}
	if fake.level != thinking.High {
		t.Errorf("thinking level = %q, want high", fake.level)
	}
	if fake.mode != session.ModeFolia {
		t.Errorf("mode = %q, want %q", fake.mode, session.ModeFolia)
	}
	if fake.workDir != "/tmp/work" {
		t.Errorf("workDir = %q, want /tmp/work", fake.workDir)
	}
	if len(sess.Messages) != 2 || sess.Messages[1].Content != "the answer" {
		t.Errorf("messages = %+v", sess.Messages)
	}
	if sess.TokensUsed != 123 {
		t.Errorf("TokensUsed = %d, want 123", sess.TokensUsed)
	}
	if view := model.View().Content; !strings.Contains(view, "answer") {
		t.Errorf("view does not show the answer:\n%s", view)
	}
}

func TestBusyQueuesNewMessage(t *testing.T) {
	sess := session.New("/tmp/work")
	fake := &fakeResponder{messages: []session.Message{{Role: "assistant", Content: "answer"}}}
	model := New(sess, WithResponder(fake))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	model = updated.(Model)
	// Simulate a response in flight without racing the responder goroutine.
	sess.Messages = []session.Message{{Role: "user", Content: "first"}}
	model.busy = true
	model.steering = agent.NewMessageQueue()

	model.input.SetValue("second")
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if cmd != nil {
		t.Error("enter while busy returned a command")
	}
	if len(sess.Messages) != 1 || sess.Messages[0].Content != "first" {
		t.Errorf("messages = %+v, want only the first message", sess.Messages)
	}
	if model.steering.Len() != 1 {
		t.Errorf("queued messages = %d, want 1", model.steering.Len())
	}
	if len(model.queued) != 1 || model.queued[0].Content != "second" {
		t.Errorf("queued = %+v", model.queued)
	}
	if got := model.input.Value(); got != "" {
		t.Errorf("input = %q, want empty after queueing", got)
	}
	if view := model.View().Content; !strings.Contains(view, "(queued)") || !strings.Contains(view, "second") {
		t.Errorf("queued message is not shown:\n%s", view)
	}
}

func TestResponderErrorIsShown(t *testing.T) {
	sess := session.New("/tmp/work")
	fake := &fakeResponder{err: errors.New("boom")}
	model := New(sess, WithResponder(fake))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	model = updated.(Model)
	model.input.SetValue("question")

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(cmd())
	model = updated.(Model)

	if model.busy {
		t.Error("busy = true after error")
	}
	if model.lastErr != "boom" {
		t.Errorf("lastErr = %q", model.lastErr)
	}
	if view := model.View().Content; !strings.Contains(view, "error: boom") {
		t.Errorf("view does not show the error:\n%s", view)
	}
}

func TestNewSessionIgnoresPendingResponse(t *testing.T) {
	sess := session.New("/tmp/work")
	fake := &fakeResponder{messages: []session.Message{{Role: "assistant", Content: "stale"}}}
	model := New(sess, WithResponder(fake))
	model.input.SetValue("question")
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Start a new session while the response is still pending.
	model.input.SetValue("/new")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	updated, _ = model.Update(cmd())
	model = updated.(Model)

	if len(model.session.Messages) != 0 {
		t.Errorf("messages = %+v, want none after new session", model.session.Messages)
	}
	if model.busy {
		t.Error("busy = true after new session")
	}
}

func TestShiftTabSavesSettings(t *testing.T) {
	sess := session.New("/tmp/work")
	sess.Provider = "openrouter"
	sess.Model = "deepseek/deepseek-v4.1-flash"

	var got []string
	model := New(sess,
		WithThinkingLevels([]string{thinking.Off, thinking.Low, thinking.High, thinking.Max}),
		WithSettingsChanged(func(providerID, modelID, thinkingLevel string) error {
			got = []string{providerID, modelID, thinkingLevel}
			return nil
		}),
	)

	updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = updated.(Model)

	want := []string{"openrouter", "deepseek/deepseek-v4.1-flash", thinking.Low}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("saved settings = %v, want %v", got, want)
	}
	if model.lastErr != "" {
		t.Errorf("lastErr = %q", model.lastErr)
	}
}

func TestShiftTabSaveErrorIsShown(t *testing.T) {
	model := New(session.New("/tmp/work"),
		WithThinkingLevels([]string{thinking.Off, thinking.Low}),
		WithSettingsChanged(func(providerID, modelID, thinkingLevel string) error {
			return errors.New("disk full")
		}),
	)

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	model = updated.(Model)
	_ = model.View()

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	model = updated.(Model)

	if !strings.Contains(model.lastErr, "save settings: disk full") {
		t.Errorf("lastErr = %q", model.lastErr)
	}
	if view := model.View().Content; !strings.Contains(view, "save settings: disk full") {
		t.Errorf("save error is not rendered:\n%s", view)
	}
}

func TestTranscriptShowsFoliarzThinkingAndMarkdown(t *testing.T) {
	sess := session.New("/tmp/work")
	sess.Messages = []session.Message{{
		Role:     "assistant",
		Thinking: "Pondering the question.",
		Content:  "The answer is **four**.",
	}}
	model := New(sess)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	model = updated.(Model)

	view := model.View().Content
	if !strings.Contains(view, "foliarz") {
		t.Errorf("view does not show the foliarz role:\n%s", view)
	}
	if strings.Contains(view, "assistant") {
		t.Errorf("view still shows the assistant role:\n%s", view)
	}
	if !strings.Contains(view, "Pondering the question.") {
		t.Errorf("view does not show the thinking:\n%s", view)
	}
	if !strings.Contains(view, "four") || strings.Contains(view, "**four**") {
		t.Errorf("view did not render markdown:\n%s", view)
	}
}

func TestHideThinking(t *testing.T) {
	sess := session.New("/tmp/work")
	sess.Messages = []session.Message{{
		Role:     "assistant",
		Thinking: "secret reasoning",
		Content:  "the answer",
	}}
	model := New(sess, WithHideThinking(true))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	model = updated.(Model)

	if view := model.View().Content; strings.Contains(view, "secret reasoning") {
		t.Errorf("view shows hidden thinking:\n%s", view)
	}
}

func TestStreamingDeltasAreShown(t *testing.T) {
	sess := session.New("/tmp/work")
	fake := &fakeResponder{
		stream: []agent.Event{
			{Thinking: "checking..."},
			{Content: "Hel"},
			{Content: "lo"},
		},
		messages: []session.Message{{
			Role:     "assistant",
			Thinking: "checking...",
			Content:  "Hello",
		}},
		tokens: 30,
	}
	model := New(sess, WithResponder(fake))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	model = updated.(Model)
	model.input.SetValue("hi")

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("submit returned no command")
	}

	updated, _ = model.Update(cmd())
	model = updated.(Model)
	if model.streamThinking != "checking..." {
		t.Errorf("streamThinking = %q", model.streamThinking)
	}
	if view := model.View().Content; !strings.Contains(view, "checking...") || !strings.Contains(view, "foliarz") {
		t.Errorf("view does not show the partial answer:\n%s", view)
	}

	for model.busy {
		msg := waitForStream(model.stream)()
		updated, _ = model.Update(msg)
		model = updated.(Model)
	}

	if model.streamThinking != "" || model.streamContent != "" {
		t.Errorf("stream buffers not cleared: %q / %q", model.streamThinking, model.streamContent)
	}
	if len(sess.Messages) != 2 || sess.Messages[1].Content != "Hello" {
		t.Errorf("messages = %+v", sess.Messages)
	}
	if sess.TokensUsed != 30 {
		t.Errorf("TokensUsed = %d, want 30", sess.TokensUsed)
	}
}

func TestStatusBarShowsMode(t *testing.T) {
	sess := session.New("/tmp/work")
	model := New(sess)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	model = updated.(Model)

	if bar := model.statusBar(); !strings.Contains(bar, session.ModeFolia) {
		t.Errorf("status bar does not show FOLIA: %q", bar)
	}

	sess.Mode = session.ModeNormal
	if bar := model.statusBar(); !strings.Contains(bar, session.ModeNormal) {
		t.Errorf("status bar does not show NORMAL: %q", bar)
	}

	sess.Mode = session.ModeYolo
	if bar := model.statusBar(); !strings.Contains(bar, session.ModeYolo) {
		t.Errorf("status bar does not show YOLO: %q", bar)
	}
}

func TestModeCommands(t *testing.T) {
	sess := session.New("/tmp/work")
	model := New(sess)

	run := func(command string) {
		model.input.SetValue(command)
		updated, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		model = updated.(Model)
	}

	run("/normal")
	if sess.Mode != session.ModeNormal {
		t.Errorf("Mode = %q after /normal, want NORMAL", sess.Mode)
	}

	run("/folia")
	if sess.Mode != session.ModeFolia {
		t.Errorf("Mode = %q after /folia, want FOLIA", sess.Mode)
	}

	run("/normal")
	run("/yolo")
	if sess.Mode != session.ModeYolo {
		t.Errorf("Mode = %q after /yolo, want YOLO", sess.Mode)
	}

	run("/new")
	if model.session.Mode != session.ModeFolia {
		t.Errorf("new session mode = %q, want FOLIA", model.session.Mode)
	}
}

func testTools() []tools.Tool {
	return []tools.Tool{
		{Name: "datetime", Description: "current time", ReadOnly: true},
		{Name: "read", Description: "read files", ReadOnly: true},
		{Name: "bash", Description: "run shell commands"},
	}
}

func TestStartupInfoShowsPromptAndTools(t *testing.T) {
	sess := session.New("/tmp/work")
	model := New(sess,
		WithSystemPrompt("SYSTEM PROMPT TEXT"),
		WithTools(testTools()),
	)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	model = updated.(Model)

	view := model.View().Content
	if !strings.Contains(view, "SYSTEM PROMPT TEXT") {
		t.Errorf("view does not show the system prompt:\n%s", view)
	}
	if !strings.Contains(view, "datetime") || !strings.Contains(view, "read") {
		t.Errorf("view does not show the read-only tools:\n%s", view)
	}
	if strings.Contains(view, "run shell commands") {
		t.Errorf("FOLIA info shows a mutating tool:\n%s", view)
	}
	if !strings.Contains(view, "FOLIA") {
		t.Errorf("view does not show the mode:\n%s", view)
	}

	model.input.SetValue("/normal")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if view := model.View().Content; !strings.Contains(view, "run shell commands") {
		t.Errorf("NORMAL info does not show bash:\n%s", view)
	}

	model.input.SetValue("/yolo")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if view := model.View().Content; !strings.Contains(view, "run shell commands") {
		t.Errorf("YOLO info does not show bash:\n%s", view)
	}
}

func TestInfoHiddenAfterFirstMessage(t *testing.T) {
	sess := session.New("/tmp/work")
	model := New(sess, WithSystemPrompt("SYSTEM PROMPT TEXT"), WithTools(testTools()))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	model = updated.(Model)
	model.input.SetValue("hello")

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if view := model.View().Content; strings.Contains(view, "SYSTEM PROMPT TEXT") {
		t.Errorf("startup info is still shown after the first message:\n%s", view)
	}
}

func TestNewSessionShowsInfoAgain(t *testing.T) {
	sess := session.New("/tmp/work")
	model := New(sess, WithSystemPrompt("SYSTEM PROMPT TEXT"), WithTools(testTools()))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	model = updated.(Model)
	model.input.SetValue("hello")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	model.input.SetValue("/new")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if view := model.View().Content; !strings.Contains(view, "SYSTEM PROMPT TEXT") {
		t.Errorf("new session does not show the startup info:\n%s", view)
	}
}

func TestToolCallsAreRendered(t *testing.T) {
	sess := session.New("/tmp/work")
	sess.Messages = []session.Message{
		{
			Role: "assistant",
			ToolCalls: []session.ToolCall{
				{ID: "call-1", Name: "read", Arguments: `{"path":"a.txt"}`},
				{ID: "call-2", Name: "bash", Arguments: `{"command":"false"}`},
			},
		},
		{Role: "tool", Content: "file contents", ToolCallID: "call-1"},
		{Role: "tool", Content: "error: exit status 1", ToolCallID: "call-2", IsError: true},
	}
	model := New(sess)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	model = updated.(Model)

	view := model.View().Content
	for _, want := range []string{"read a.txt", "file contents", "$ false", "error: exit status 1"} {
		if !strings.Contains(view, want) {
			t.Errorf("view does not contain %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "assistant") {
		t.Errorf("view shows the assistant role:\n%s", view)
	}
}

func TestSystemPromptHasBackground(t *testing.T) {
	model := New(session.New("/tmp/work"), WithSystemPrompt("SYSTEM PROMPT TEXT"), WithTools(testTools()))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	model = updated.(Model)

	view := model.View().Content
	if !strings.Contains(view, "SYSTEM PROMPT TEXT") {
		t.Fatalf("view does not show the system prompt:\n%s", view)
	}
	if !strings.Contains(view, "\x1b[48;") {
		t.Errorf("system prompt has no background:\n%q", view)
	}
}

func TestMouseModeEnabled(t *testing.T) {
	if got := New(session.New("/tmp/work")).View().MouseMode; got != tea.MouseModeCellMotion {
		t.Errorf("MouseMode = %v, want cell motion", got)
	}
}

func TestMouseWheelScrollsChat(t *testing.T) {
	sess := session.New("/tmp/work")
	for i := 0; i < 40; i++ {
		sess.Messages = append(sess.Messages, session.Message{
			Role:    "user",
			Content: fmt.Sprintf("message %02d", i),
		})
	}
	model := New(sess)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	model = updated.(Model)

	bottom := model.View().Content
	if strings.Contains(bottom, "message 00") {
		t.Fatalf("bottom view unexpectedly shows the first message")
	}
	if !strings.Contains(bottom, "message 39") {
		t.Fatalf("bottom view does not show the last message:\n%s", bottom)
	}

	for i := 0; i < 200; i++ {
		updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
		model = updated.(Model)
	}
	top := model.View().Content
	if !strings.Contains(top, "message 00") {
		t.Errorf("scrolled view does not show the first message:\n%s", top)
	}
	if !strings.Contains(top, "ctrl+end") {
		t.Errorf("scrolled view does not show the jump hint:\n%s", top)
	}

	for i := 0; i < 200; i++ {
		updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		model = updated.(Model)
	}
	if !model.follow {
		t.Errorf("follow = false after scrolling down, want true")
	}
	if strings.Contains(model.View().Content, "ctrl+end") {
		t.Error("jump hint is still shown after scrolling to the bottom")
	}
}

func TestScrollDoesNotAutoFollowWhileStreaming(t *testing.T) {
	sess := session.New("/tmp/work")
	for i := 0; i < 40; i++ {
		sess.Messages = append(sess.Messages, session.Message{
			Role:    "user",
			Content: fmt.Sprintf("message %02d", i),
		})
	}
	model := New(sess)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	model = updated.(Model)
	_ = model.View()

	if model.cache.dirty {
		t.Fatal("cache is dirty after rendering")
	}

	for i := 0; i < 200; i++ {
		updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
		model = updated.(Model)
	}
	if model.cache.dirty {
		t.Error("scrolling invalidated the chat cache")
	}

	model.busy = true
	updated, _ = model.Update(streamMsg{event: agent.Event{Content: "NEW STREAMING CONTENT"}})
	model = updated.(Model)

	view := model.View().Content
	if strings.Contains(view, "NEW STREAMING CONTENT") {
		t.Errorf("view jumped to new streamed content while scrolled:\n%s", view)
	}
	if !strings.Contains(view, "message 00") {
		t.Errorf("view did not stay at the scrolled position:\n%s", view)
	}
	if !strings.Contains(view, "ctrl+end") {
		t.Errorf("scrolled view does not show the jump hint:\n%s", view)
	}
	if model.follow {
		t.Error("follow = true while scrolled")
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	model = updated.(Model)
	if !model.follow {
		t.Error("ctrl+end did not follow the newest content")
	}
	if view := model.View().Content; !strings.Contains(view, "NEW STREAMING CONTENT") {
		t.Errorf("ctrl+end did not jump to the newest content:\n%s", view)
	}
}

func TestStreamingShowsAllThinking(t *testing.T) {
	model := New(session.New("/tmp/work"))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	model = updated.(Model)
	model.busy = true

	model.streamThinking = "first thinking"
	updated, _ = model.Update(streamMsg{event: agent.Event{Reset: true}})
	model = updated.(Model)
	updated, _ = model.Update(streamMsg{event: agent.Event{Thinking: "second thinking"}})
	model = updated.(Model)

	view := model.View().Content
	if !strings.Contains(view, "first thinking") {
		t.Errorf("earlier thinking is missing during streaming:\n%s", view)
	}
	if !strings.Contains(view, "second thinking") {
		t.Errorf("current thinking is missing during streaming:\n%s", view)
	}
}

func TestCompletedResponseKeepsAllThinking(t *testing.T) {
	sess := session.New("/tmp/work")
	fake := &fakeResponder{
		stream: []agent.Event{
			{Thinking: "first thinking"},
			{Reset: true},
			{Thinking: "second thinking"},
		},
		messages: []session.Message{
			{Role: "assistant", Thinking: "first thinking", ToolCalls: []session.ToolCall{{ID: "call-1", Name: "read", Arguments: `{}`}}},
			{Role: "tool", Content: "result", ToolCallID: "call-1"},
			{Role: "assistant", Thinking: "second thinking", Content: "answer"},
		},
	}
	model := New(sess, WithResponder(fake))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	model = updated.(Model)
	model.input.SetValue("question")

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(cmd())
	model = updated.(Model)
	for model.busy {
		msg := waitForStream(model.stream)()
		updated, _ = model.Update(msg)
		model = updated.(Model)
	}

	view := model.View().Content
	if !strings.Contains(view, "first thinking") {
		t.Errorf("first thinking is missing after completion:\n%s", view)
	}
	if !strings.Contains(view, "second thinking") {
		t.Errorf("second thinking is missing after completion:\n%s", view)
	}
}

func TestToolCallShownWhileRunning(t *testing.T) {
	model := New(session.New("/tmp/work"))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	model = updated.(Model)
	model.busy = true

	call := session.ToolCall{ID: "call-1", Name: "bash", Arguments: `{"command":"sleep 5"}`}
	updated, _ = model.Update(streamMsg{event: agent.Event{ToolStart: &call}})
	model = updated.(Model)

	view := model.View().Content
	if !strings.Contains(view, "$ sleep 5") {
		t.Errorf("running tool call is not shown:\n%s", view)
	}
	if !strings.Contains(view, "running…") {
		t.Errorf("running tool state is not shown:\n%s", view)
	}

	result := session.Message{Role: "tool", ToolCallID: "call-1", Content: "finished"}
	updated, _ = model.Update(streamMsg{event: agent.Event{ToolResult: &result}})
	model = updated.(Model)

	view = model.View().Content
	if !strings.Contains(view, "finished") {
		t.Errorf("tool result is not shown while streaming:\n%s", view)
	}
	if strings.Contains(view, "running…") {
		t.Errorf("tool still shows as running:\n%s", view)
	}
}

func TestSubmitKeepsScrollPosition(t *testing.T) {
	sess := session.New("/tmp/work")
	for i := 0; i < 40; i++ {
		sess.Messages = append(sess.Messages, session.Message{
			Role:    "user",
			Content: fmt.Sprintf("message %02d", i),
		})
	}
	model := New(sess)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	model = updated.(Model)
	for i := 0; i < 200; i++ {
		updated, _ = model.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
		model = updated.(Model)
	}
	if model.follow {
		t.Fatal("follow = true after scrolling up")
	}

	model.input.SetValue("next")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.follow {
		t.Error("submitting moved the view to the newest content")
	}
	if view := model.View().Content; !strings.Contains(view, "message 00") {
		t.Errorf("view did not stay scrolled after submitting:\n%s", view)
	}
}

func TestStreamingInterleavesThinkingAndToolCalls(t *testing.T) {
	model := New(session.New("/tmp/work"))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 90, Height: 40})
	model = updated.(Model)
	model.busy = true

	firstCall := session.ToolCall{ID: "call-1", Name: "bash", Arguments: `{"command":"first command"}`}
	firstResult := session.Message{Role: "tool", ToolCallID: "call-1", Content: "first result"}
	secondCall := session.ToolCall{ID: "call-2", Name: "bash", Arguments: `{"command":"second command"}`}
	secondResult := session.Message{Role: "tool", ToolCallID: "call-2", Content: "second result"}

	events := []agent.Event{
		{Thinking: "thinking one"},
		{ToolStart: &firstCall},
		{ToolResult: &firstResult},
		{Thinking: "thinking two"},
		{ToolStart: &secondCall},
		{ToolResult: &secondResult},
		{Thinking: "thinking three"},
	}
	for _, event := range events {
		updated, _ = model.Update(streamMsg{event: event})
		model = updated.(Model)
	}

	view := model.View().Content
	order := []string{"thinking one", "$ first command", "first result", "thinking two", "$ second command", "second result", "thinking three"}
	last := -1
	for _, want := range order {
		index := strings.Index(view, want)
		if index == -1 {
			t.Fatalf("view does not contain %q:\n%s", want, view)
		}
		if index < last {
			t.Errorf("%q appears out of order:\n%s", want, view)
		}
		last = index
	}
}

func TestContinueCommandResumesContext(t *testing.T) {
	sess := session.New("/tmp/work")
	sess.Messages = []session.Message{{Role: "user", Content: "previous question"}}
	fake := &fakeResponder{messages: []session.Message{{Role: "assistant", Content: "continued answer"}}}
	model := New(sess, WithResponder(fake))
	model.input.SetValue("/continue")

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("/continue returned no command")
	}
	if !model.busy {
		t.Error("/continue did not start a response")
	}
	if len(sess.Messages) != 1 {
		t.Errorf("/continue added a message: %+v", sess.Messages)
	}

	updated, _ = model.Update(cmd())
	model = updated.(Model)
	for model.busy {
		msg := waitForStream(model.stream)()
		updated, _ = model.Update(msg)
		model = updated.(Model)
	}

	if fake.calls != 1 {
		t.Errorf("calls = %d, want 1", fake.calls)
	}
	if len(fake.history) != 1 || fake.history[0].Content != "previous question" {
		t.Errorf("history = %+v", fake.history)
	}
	if len(sess.Messages) != 2 || sess.Messages[1].Content != "continued answer" {
		t.Errorf("messages = %+v", sess.Messages)
	}
}

func TestCtrlCPreservesCompletedToolContext(t *testing.T) {
	sess := session.New("/tmp/work")
	sess.Messages = []session.Message{{Role: "user", Content: "question"}}
	model := New(sess)
	model.busy = true
	model.session.Mode = session.ModeNormal

	call := session.ToolCall{ID: "call-1", Name: "read", Arguments: `{"path":"a.txt"}`}
	result := session.Message{Role: "tool", ToolCallID: "call-1", Content: "file contents"}
	model.streamItems = []streamItem{
		{message: &session.Message{Role: "assistant", Content: "reading the file"}},
		{tool: &streamTool{call: call, result: &result}},
	}
	model.streamContent = "partial answer"

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = updated.(Model)

	if len(sess.Messages) != 5 {
		t.Fatalf("messages = %+v, want user, assistant, tool call, tool result and partial answer", sess.Messages)
	}
	if sess.Messages[1].Content != "reading the file" {
		t.Errorf("assistant content missing: %+v", sess.Messages[1])
	}
	if len(sess.Messages[2].ToolCalls) != 1 || sess.Messages[2].ToolCalls[0].ID != "call-1" {
		t.Errorf("tool call missing: %+v", sess.Messages[2])
	}
	if sess.Messages[3].Content != "file contents" {
		t.Errorf("tool result missing: %+v", sess.Messages[3])
	}
	if sess.Messages[4].Content != "partial answer" {
		t.Errorf("partial answer missing: %+v", sess.Messages[4])
	}
}

func TestCtrlOTogglesToolOutput(t *testing.T) {
	sess := session.New("/tmp/work")
	sess.Messages = []session.Message{
		{Role: "assistant", ToolCalls: []session.ToolCall{{ID: "call-1", Name: "read", Arguments: `{"path":"a.txt"}`}}},
		{Role: "tool", ToolCallID: "call-1", Content: "line 01\nline 02\nline 03\nline 04\nline 05\nline 06\nline 07\nline 08\nline 09\nline 10"},
	}
	model := New(sess)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	model = updated.(Model)

	view := model.View().Content
	if !strings.Contains(view, "… (4 more lines)") {
		t.Fatalf("collapsed tool output has no hidden-lines hint:\n%s", view)
	}
	if strings.Contains(view, "line 10") {
		t.Fatalf("collapsed tool output shows all lines:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	model = updated.(Model)
	view = model.View().Content
	if strings.Contains(view, "more lines") {
		t.Errorf("expanded tool output still hides lines:\n%s", view)
	}
	if !strings.Contains(view, "line 10") {
		t.Errorf("expanded tool output does not show all lines:\n%s", view)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	model = updated.(Model)
	if view := model.View().Content; !strings.Contains(view, "… (4 more lines)") {
		t.Errorf("tool output did not collapse again:\n%s", view)
	}
}

func TestCtrlCPreservesThinkingOnlyContext(t *testing.T) {
	sess := session.New("/tmp/work")
	sess.Messages = []session.Message{{Role: "user", Content: "question"}}
	model := New(sess)
	model.busy = true
	model.streamThinking = "still reasoning"
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	model = updated.(Model)
	model.busy = true
	model.streamThinking = "still reasoning"

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = updated.(Model)

	if len(sess.Messages) != 2 || sess.Messages[1].Thinking != "still reasoning" {
		t.Fatalf("thinking was not preserved: %+v", sess.Messages)
	}
	if view := model.View().Content; !strings.Contains(view, "still reasoning") {
		t.Errorf("preserved thinking is not shown:\n%s", view)
	}
}

func TestBashToolShowsTimeoutAndDuration(t *testing.T) {
	duration := int64(1234)
	sess := session.New("/tmp/work")
	sess.Messages = []session.Message{
		{
			Role: "assistant",
			ToolCalls: []session.ToolCall{{
				ID:        "call-1",
				Name:      "bash",
				Arguments: `{"command":"echo hi","timeout":30}`,
			}},
		},
		{
			Role:       "tool",
			ToolCallID: "call-1",
			Content:    "hi",
			DurationMs: &duration,
		},
	}
	model := New(sess)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	model = updated.(Model)

	view := model.View().Content
	if !strings.Contains(view, "(timeout 30s)") {
		t.Errorf("tool call does not show the timeout:\n%s", view)
	}
	if !strings.Contains(view, "Took 1.2s") {
		t.Errorf("tool call does not show the duration:\n%s", view)
	}
}

func TestToolApprovalPrompt(t *testing.T) {
	model := New(session.New("/tmp/work"))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	model = updated.(Model)

	request := &approvalRequest{
		call:     session.ToolCall{Name: "bash", Arguments: `{"command":"rm -rf /tmp/x","timeout":30}`},
		response: make(chan bool, 1),
	}
	updated, _ = model.Update(streamMsg{approval: request})
	model = updated.(Model)

	view := model.View().Content
	for _, want := range []string{"allow tool call", "bash", "rm -rf /tmp/x", "press y to allow", "NORMAL"} {
		if !strings.Contains(view, want) {
			t.Errorf("approval prompt does not contain %q:\n%s", want, view)
		}
	}

	// Keys other than y/n are ignored while waiting.
	updated, _ = model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	model = updated.(Model)
	if model.pendingApproval == nil {
		t.Fatal("approval prompt was dismissed by an unrelated key")
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)
	if len(request.response) != 1 || <-request.response != true {
		t.Error("y did not allow the tool call")
	}
	if model.pendingApproval != nil {
		t.Error("approval prompt still pending after y")
	}
}

func TestToolApprovalDeny(t *testing.T) {
	model := New(session.New("/tmp/work"))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	model = updated.(Model)
	request := &approvalRequest{
		call:     session.ToolCall{Name: "bash", Arguments: `{"command":"echo hi","timeout":10}`},
		response: make(chan bool, 1),
	}
	updated, _ = model.Update(streamMsg{approval: request})
	model = updated.(Model)

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	model = updated.(Model)
	if len(request.response) != 1 || <-request.response != false {
		t.Error("n did not deny the tool call")
	}
	if model.pendingApproval != nil {
		t.Error("approval prompt still pending after n")
	}
}

func TestToolApprovalFlow(t *testing.T) {
	sess := session.New("/tmp/work")
	call := session.ToolCall{Name: "bash", Arguments: `{"command":"echo hi","timeout":10}`}
	fake := &fakeResponder{
		approvalCall: &call,
		messages:     []session.Message{{Role: "assistant", Content: "finished"}},
	}
	model := New(sess, WithResponder(fake))
	model.input.SetValue("run it")

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("submit returned no command")
	}

	updated, _ = model.Update(cmd())
	model = updated.(Model)
	if model.pendingApproval == nil {
		t.Fatal("approval prompt is not shown")
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	model = updated.(Model)

	for model.busy {
		msg := waitForStream(model.stream)()
		updated, _ = model.Update(msg)
		model = updated.(Model)
	}
	if !fake.approved {
		t.Error("approval was not delivered to the agent")
	}
	if len(sess.Messages) != 2 || sess.Messages[1].Content != "finished" {
		t.Errorf("messages = %+v", sess.Messages)
	}
}

func TestProjectContextIsShownAndSent(t *testing.T) {
	sess := session.New("/tmp/work")
	sess.ProjectContext = "PROJECT RULES"
	fake := &fakeResponder{messages: []session.Message{{Role: "assistant", Content: "answer"}}}
	model := New(sess, WithSystemPrompt("SYSTEM PROMPT"), WithTools(testTools()), WithResponder(fake))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	model = updated.(Model)

	if view := model.View().Content; !strings.Contains(view, "PROJECT RULES") {
		t.Errorf("startup info does not show the project context:\n%s", view)
	}

	model.input.SetValue("question")
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(cmd())
	model = updated.(Model)
	for model.busy {
		msg := waitForStream(model.stream)()
		updated, _ = model.Update(msg)
		model = updated.(Model)
	}

	if fake.projectContext != "PROJECT RULES" {
		t.Errorf("project context = %q", fake.projectContext)
	}
}

func TestConsecutiveToolOutputsAreSeparated(t *testing.T) {
	sess := session.New("/tmp/work")
	sess.Messages = []session.Message{
		{
			Role: "assistant",
			ToolCalls: []session.ToolCall{
				{ID: "call-1", Name: "bash", Arguments: `{"command":"first","timeout":10}`},
				{ID: "call-2", Name: "bash", Arguments: `{"command":"second","timeout":10}`},
			},
		},
		{Role: "tool", ToolCallID: "call-1", Content: "first result"},
		{Role: "tool", ToolCallID: "call-2", Content: "second result"},
	}
	model := New(sess)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	model = updated.(Model)

	view := model.View().Content
	lines := strings.Split(view, "\n")
	secondIndex := -1
	for i, line := range lines {
		if strings.Contains(line, "$ second") {
			secondIndex = i
			break
		}
	}
	if secondIndex <= 0 {
		t.Fatalf("second tool call not found:\n%s", view)
	}

	separator := lines[secondIndex-1]
	if strings.Contains(separator, "\x1b[48;") {
		t.Errorf("separator between tool outputs has a background: %q", separator)
	}
	if strings.TrimSpace(stripANSI(separator)) != "" {
		t.Errorf("separator between tool outputs is not empty: %q", separator)
	}
}

func stripANSI(s string) string {
	return regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(s, "")
}

func TestSkillInputDoesNotShowCommandPalette(t *testing.T) {
	model := New(session.New("/tmp/work"))
	model.input.SetValue("/skill:web-search")
	if palette := model.commandPalette(); palette != "" {
		t.Errorf("command palette shown for skill input: %q", palette)
	}
	if view := model.View().Content; strings.Contains(view, "no matching commands") {
		t.Errorf("view shows no-matching-commands for skill input:\n%s", view)
	}
}

func TestPendingCompactionAppliedAfterResponse(t *testing.T) {
	sess := session.New("/tmp/work")
	sess.Messages = []session.Message{{Role: "user", Content: "question"}}
	model := New(sess)
	model.busy = true
	model.requestID = 1

	compaction := &agent.CompactionResult{
		Summary:          "SUMMARY",
		FirstKeptMessage: 1,
		TokensBefore:     50000,
		ReadFiles:        []string{"a.txt"},
		ModifiedFiles:    []string{"b.txt"},
	}
	updated, _ := model.Update(streamMsg{requestID: 1, event: agent.Event{Compaction: compaction}})
	model = updated.(Model)
	if model.pendingCompaction == nil {
		t.Fatal("compaction was not queued")
	}

	updated, _ = model.Update(streamMsg{
		requestID: 1,
		done:      true,
		messages:  []session.Message{{Role: "assistant", Content: "answer"}},
		usage:     agent.Usage{Tokens: 123},
	})
	model = updated.(Model)

	if sess.Summary != "SUMMARY" {
		t.Errorf("Summary = %q", sess.Summary)
	}
	if sess.FirstKeptMessage != 1 {
		t.Errorf("FirstKeptMessage = %d", sess.FirstKeptMessage)
	}
	if len(sess.SummaryReadFiles) != 1 || len(sess.SummaryModifiedFiles) != 1 {
		t.Errorf("file lists = %v / %v", sess.SummaryReadFiles, sess.SummaryModifiedFiles)
	}
	if sess.SummaryTokensBefore != 50000 {
		t.Errorf("SummaryTokensBefore = %d", sess.SummaryTokensBefore)
	}
	if sess.TokensUsed != 123 {
		t.Errorf("TokensUsed = %d", sess.TokensUsed)
	}
	if model.pendingCompaction != nil {
		t.Error("compaction still pending")
	}
}

func TestCompactionContextIncludesSessionState(t *testing.T) {
	sess := session.New("/tmp/work")
	sess.Messages = []session.Message{{Role: "user", Content: "question"}}
	sess.Summary = "EARLIER SUMMARY"
	sess.FirstKeptMessage = 1
	sess.SummaryReadFiles = []string{"read.txt"}
	sess.SummaryModifiedFiles = []string{"modified.txt"}
	sess.ContextWindow = 50000

	fake := &fakeResponder{messages: []session.Message{{Role: "assistant", Content: "answer"}}}
	model := New(sess, WithResponder(fake))
	model.input.SetValue("next")

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(cmd())
	model = updated.(Model)
	for model.busy {
		msg := waitForStream(model.stream)()
		updated, _ = model.Update(msg)
		model = updated.(Model)
	}

	got := fake.compaction
	if got.Summary != "EARLIER SUMMARY" || got.FirstKeptMessage != 1 {
		t.Errorf("compaction context = %+v", got)
	}
	if len(got.ReadFiles) != 1 || got.ReadFiles[0] != "read.txt" {
		t.Errorf("read files = %v", got.ReadFiles)
	}
	if len(got.ModifiedFiles) != 1 || got.ModifiedFiles[0] != "modified.txt" {
		t.Errorf("modified files = %v", got.ModifiedFiles)
	}
	if got.ContextWindow != 50000 {
		t.Errorf("context window = %d", got.ContextWindow)
	}
}

func TestSteeringMessageDeliveredByAgent(t *testing.T) {
	sess := session.New("/tmp/work")
	gate := make(chan struct{})
	fake := &fakeResponder{
		gate: gate,
		messages: []session.Message{
			{Role: "assistant", Content: "first"},
			{Role: "assistant", Content: "steered"},
		},
	}
	model := New(sess, WithResponder(fake))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	model = updated.(Model)
	model.input.SetValue("start")
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	// Queue a steering message before the responder starts working.
	model.input.SetValue("steer me")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	close(gate)

	updated, _ = model.Update(cmd())
	model = updated.(Model)
	for model.busy {
		msg := waitForStream(model.stream)()
		updated, _ = model.Update(msg)
		model = updated.(Model)
	}

	if len(sess.Messages) != 4 {
		t.Fatalf("messages = %+v", sess.Messages)
	}
	if sess.Messages[2].Content != "steer me" || sess.Messages[3].Content != "steered" {
		t.Errorf("steering order = %+v", sess.Messages)
	}
	if len(model.queued) != 0 {
		t.Errorf("queued messages were not cleared: %+v", model.queued)
	}
	if view := model.View().Content; strings.Contains(view, "(queued)") {
		t.Errorf("queued marker still shown:\n%s", view)
	}
}

func TestLeftoverQueuedMessageStartsFollowUp(t *testing.T) {
	sess := session.New("/tmp/work")
	sess.Messages = []session.Message{{Role: "user", Content: "start"}}
	fake := &fakeResponder{messages: []session.Message{{Role: "assistant", Content: "first answer"}}}
	model := New(sess, WithResponder(fake))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	model = updated.(Model)
	model.busy = true
	model.requestID = 1
	model.steering = agent.NewMessageQueue()
	model.queued = []session.Message{{Role: "user", Content: "late message"}}
	// The agent finished before draining the queue.
	model.steering.Push(session.Message{Role: "user", Content: "late message"})

	updated, cmd := model.Update(streamMsg{
		requestID: 1,
		done:      true,
		messages:  []session.Message{{Role: "assistant", Content: "first answer"}},
	})
	model = updated.(Model)

	if cmd == nil {
		t.Fatal("leftover queued message did not start a follow-up")
	}
	if !model.busy {
		t.Error("follow-up response is not running")
	}
	if len(sess.Messages) != 3 || sess.Messages[2].Content != "late message" {
		t.Errorf("messages = %+v", sess.Messages)
	}
}

func TestCtrlCClearsQueuedMessages(t *testing.T) {
	sess := session.New("/tmp/work")
	model := New(sess, WithResponder(&fakeResponder{}))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	model = updated.(Model)
	model.input.SetValue("start")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model.input.SetValue("queued message")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = updated.(Model)

	if model.steering != nil || len(model.queued) != 0 {
		t.Errorf("queue was not cleared: %+v", model.queued)
	}
}

func TestFoliaCommandStopsRunningResponse(t *testing.T) {
	sess := session.New("/tmp/work")
	fake := &fakeResponder{messages: []session.Message{{Role: "assistant", Content: "answer"}}}
	model := New(sess, WithResponder(fake))
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	model = updated.(Model)
	model.input.SetValue("question")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	model.session.Mode = session.ModeYolo
	model.streamContent = "partial"

	model.input.SetValue("/folia")
	updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)

	if model.busy {
		t.Error("response still running after /folia")
	}
	if model.session.Mode != session.ModeFolia {
		t.Errorf("mode = %q, want FOLIA", model.session.Mode)
	}
	if len(sess.Messages) != 2 || sess.Messages[1].Content != "partial" {
		t.Errorf("partial context was not preserved: %+v", sess.Messages)
	}
}

func TestModeCommandsRejectedWhileBusy(t *testing.T) {
	for _, command := range []string{"/normal", "/yolo"} {
		sess := session.New("/tmp/work")
		model := New(sess, WithResponder(&fakeResponder{}))
		updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
		model = updated.(Model)
		model.input.SetValue("question")
		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		model = updated.(Model)

		model.input.SetValue(command)
		updated, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		model = updated.(Model)

		if model.session.Mode != session.ModeFolia {
			t.Errorf("%s changed the mode while busy: %q", command, model.session.Mode)
		}
		if !strings.Contains(model.lastErr, "while a response is running") {
			t.Errorf("%s lastErr = %q", command, model.lastErr)
		}
	}
}

func TestStatusBarShowsCost(t *testing.T) {
	sess := session.New("/tmp/work")
	sess.TokensUsed = 1200
	sess.ContextWindow = 100000
	model := New(sess)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	model = updated.(Model)

	if bar := model.statusBar(); strings.Contains(bar, "$") {
		t.Errorf("status bar shows a cost of zero: %q", bar)
	}

	sess.CostUSD = 0.01234
	bar := model.statusBar()
	if !strings.Contains(bar, "$0.012") {
		t.Errorf("status bar does not show the cost: %q", bar)
	}
	if strings.Index(bar, "$0.012") < strings.Index(bar, "tokens") {
		t.Errorf("cost is not next to the tokens: %q", bar)
	}
}

func TestResponseCostAccumulates(t *testing.T) {
	sess := session.New("/tmp/work")
	fake := &fakeResponder{
		messages: []session.Message{{Role: "assistant", Content: "answer"}},
		tokens:   500,
		cost:     0.0025,
	}
	model := New(sess, WithResponder(fake))
	model.input.SetValue("question")
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = updated.(Model)
	updated, _ = model.Update(cmd())
	model = updated.(Model)
	for model.busy {
		msg := waitForStream(model.stream)()
		updated, _ = model.Update(msg)
		model = updated.(Model)
	}
	if sess.CostUSD != 0.0025 {
		t.Errorf("CostUSD = %v, want 0.0025", sess.CostUSD)
	}
}
