// Package tui implements the Foliarz terminal user interface.
package tui

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mdukat/foliarz/internal/agent"
	"github.com/mdukat/foliarz/internal/buildinfo"
	"github.com/mdukat/foliarz/internal/commands"
	"github.com/mdukat/foliarz/internal/session"
	"github.com/mdukat/foliarz/internal/skills"
	"github.com/mdukat/foliarz/internal/thinking"
	"github.com/mdukat/foliarz/internal/tools"
)

var (
	appNameStyle = lipgloss.NewStyle().Bold(true)
	versionStyle = lipgloss.NewStyle().Faint(true)
	roleStyle    = lipgloss.NewStyle().Bold(true)
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

// Responder turns session history into new messages produced by the model.
// Deltas are passed to emit while the model is answering; approve asks the
// user before a non-read-only tool call in NORMAL mode.
type Responder interface {
	Respond(ctx context.Context, history []session.Message, thinkingLevel, mode, projectContext string, compaction agent.CompactionContext, steering *agent.MessageQueue, emit func(agent.Event), approve agent.Approver) ([]session.Message, agent.Usage, error)
}

// approvalRequest asks the user to allow or deny a tool call.
type approvalRequest struct {
	call     session.ToolCall
	response chan bool
}

// Compactor summarizes older context.
type Compactor interface {
	Compact(ctx context.Context, input agent.CompactionInput) (agent.CompactionResult, error)
}

// CompactionSettings configures automatic context compaction.
type CompactionSettings struct {
	Enabled          bool
	ReserveTokens    int
	KeepRecentTokens int
}

const (
	defaultCompactionReserveTokens    = 16384
	defaultCompactionKeepRecentTokens = 20000
)

// ResponderFactory creates a responder for a provider and model. It also
// returns the thinking levels supported by that model.
type ResponderFactory func(providerID, modelID string) (Responder, []string, error)

// SessionStore persists and loads sessions.
type SessionStore interface {
	Save(*session.Session) error
	List() ([]*session.Session, error)
	Load(id string) (*session.Session, error)
}

// sessionPicker is the /session selection window.
type sessionPicker struct {
	sessions []*session.Session
	selected int
}

// streamTool is a tool call that is running or has finished during streaming.
type streamTool struct {
	call   session.ToolCall
	result *session.Message
}

// streamItem is one visible item of the streaming response, either an
// assistant thinking/answer block or a tool call. Items keep their order so
// thinking and tool calls are interleaved like in the final transcript.
type streamItem struct {
	message *session.Message
	tool    *streamTool
}

// streamMsg carries a streamed delta or the final result of a model request
// back to the TUI.
type streamMsg struct {
	requestID int
	event     agent.Event
	done      bool
	approval  *approvalRequest
	messages  []session.Message
	usage     agent.Usage
	err       error
}

// Model is the Bubble Tea model of the Foliarz TUI.
type Model struct {
	session           *session.Session
	input             textarea.Model
	thinkingLevels    []string
	newSession        func(workDir string) *session.Session
	responder         Responder
	responderFactory  ResponderFactory
	compactor         Compactor
	compaction        CompactionSettings
	models            []ModelChoice
	modelPicker       *modelPicker
	store             SessionStore
	projectLoader     func(dir string) (string, error)
	picker            *sessionPicker
	lastSave          time.Time
	settingsChanged   func(providerID, modelID, thinkingLevel string) error
	hideThinking      bool
	expandTools       bool
	follow            bool
	scrollTop         int
	cache             *chatCache
	systemPrompt      string
	tools             []tools.Tool
	skills            []skills.Skill
	showInfo          bool
	commandCursor     int
	requestID         int
	busy              bool
	lastErr           string
	stream            chan streamMsg
	cancel            context.CancelFunc
	streamThinking    string
	streamContent     string
	streamItems       []streamItem
	queued            []session.Message
	steering          *agent.MessageQueue
	pendingApproval   *approvalRequest
	pendingCompaction *agent.CompactionResult
	width             int
	height            int
}

// Option configures the TUI model.
type Option func(*Model)

// WithThinkingLevels limits thinking-level cycling to levels, ordered from
// least to most reasoning.
func WithThinkingLevels(levels []string) Option {
	return func(m *Model) {
		if len(levels) > 0 {
			m.thinkingLevels = slices.Clone(levels)
		}
	}
}

// WithNewSession sets the factory used by the /new command to create a new
// session in the given working directory.
func WithNewSession(factory func(workDir string) *session.Session) Option {
	return func(m *Model) {
		if factory != nil {
			m.newSession = factory
		}
	}
}

// WithResponderFactory sets the factory used to rebuild the responder when a
// session with another model or provider is opened.
func WithResponderFactory(factory ResponderFactory) Option {
	return func(m *Model) {
		m.responderFactory = factory
	}
}

// WithModels sets the models available to the /model command.
func WithModels(models []ModelChoice) Option {
	return func(m *Model) {
		m.models = models
	}
}

// WithCompactor sets the component used to summarize old context.
func WithCompactor(compactor Compactor) Option {
	return func(m *Model) {
		m.compactor = compactor
	}
}

// WithCompaction sets automatic compaction settings.
func WithCompaction(settings CompactionSettings) Option {
	return func(m *Model) {
		m.compaction = settings
	}
}

// WithSessionStore sets the store used to save, list and load sessions.
func WithSessionStore(store SessionStore) Option {
	return func(m *Model) {
		m.store = store
	}
}

// WithProjectContextLoader sets the loader used to reload AGENTS.md when a
// session is opened.
func WithProjectContextLoader(loader func(dir string) (string, error)) Option {
	return func(m *Model) {
		m.projectLoader = loader
	}
}

// WithSystemPrompt sets the system prompt shown when a session starts.
func WithSystemPrompt(prompt string) Option {
	return func(m *Model) {
		m.systemPrompt = prompt
	}
}

// WithSkills sets the skills advertised to the model and shown at startup.
func WithSkills(skillSet []skills.Skill) Option {
	return func(m *Model) {
		m.skills = skillSet
	}
}

// WithTools sets the tools shown when a session starts.
func WithTools(toolset []tools.Tool) Option {
	return func(m *Model) {
		m.tools = toolset
	}
}

// WithHideThinking hides the model's thinking from the transcript.
func WithHideThinking(hide bool) Option {
	return func(m *Model) {
		m.hideThinking = hide
	}
}

// WithSettingsChanged sets the callback invoked when the session's model or
// thinking level changes.
func WithSettingsChanged(onChange func(providerID, modelID, thinkingLevel string) error) Option {
	return func(m *Model) {
		m.settingsChanged = onChange
	}
}

// WithResponder sets the responder used to send messages to the model.
func WithResponder(responder Responder) Option {
	return func(m *Model) {
		m.responder = responder
	}
}

// New creates the TUI model for sess.
func New(sess *session.Session, opts ...Option) Model {
	input := textarea.New()
	input.Prompt = ""
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.DynamicHeight = true
	input.MinHeight = 1
	input.MaxHeight = maxInputHeight(0)
	input.MaxContentHeight = 1000
	input.SetStyles(inputStyles())
	// shift+enter needs terminal support for modified keys; ctrl+j and
	// alt+enter work on terminals that do not report modifiers.
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter", "ctrl+j"))
	input.SetValue("")
	input.Focus()

	m := Model{
		session:        sess,
		input:          input,
		thinkingLevels: thinking.Levels(),
		showInfo:       true,
		follow:         true,
		compaction:     CompactionSettings{Enabled: true, ReserveTokens: defaultCompactionReserveTokens, KeepRecentTokens: defaultCompactionKeepRecentTokens},
		cache:          &chatCache{dirty: true, transcriptDirty: true, infoDirty: true},
	}
	for _, opt := range opts {
		opt(&m)
	}
	if m.compactor == nil {
		if compactor, ok := m.responder.(Compactor); ok {
			m.compactor = compactor
		}
	}
	return m
}

// inputStyles returns plain styles so the input bar keeps the terminal's
// default background, like the Pi agent editor.
func inputStyles() textarea.Styles {
	styles := textarea.DefaultDarkStyles()
	plain := lipgloss.NewStyle()
	styles.Focused.Base = plain
	styles.Focused.CursorLine = plain
	styles.Focused.EndOfBuffer = plain
	styles.Focused.Prompt = plain
	styles.Focused.Text = plain
	return styles
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeInput()
		m.invalidateTranscript()
		m.invalidateInfo()
	case tea.MouseWheelMsg:
		if m.picker != nil {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				m.moveSessionSelection(-1)
			case tea.MouseWheelDown:
				m.moveSessionSelection(1)
			}
			return m, nil
		}
		if m.modelPicker != nil {
			switch msg.Mouse().Button {
			case tea.MouseWheelUp:
				m.moveModelSelection(-1)
			case tea.MouseWheelDown:
				m.moveModelSelection(1)
			}
			return m, nil
		}
		delta := 0
		switch msg.Mouse().Button {
		case tea.MouseWheelUp:
			delta = -3
		case tea.MouseWheelDown:
			delta = 3
		}
		if delta != 0 {
			m.scrollBy(delta)
		}
		return m, nil
	case streamMsg:
		if msg.requestID != m.requestID {
			return m, nil
		}
		if msg.approval != nil {
			m.pendingApproval = msg.approval
			m.invalidateChat()
			return m, waitForStream(m.stream)
		}
		if !msg.done {
			event := msg.event
			if event.Reset {
				m.archiveStream()
			}
			if event.ToolStart != nil {
				m.archiveStream()
				m.streamItems = append(m.streamItems, streamItem{
					tool: &streamTool{call: *event.ToolStart},
				})
			}
			if event.ToolResult != nil {
				for i := range m.streamItems {
					tool := m.streamItems[i].tool
					if tool != nil && tool.call.ID == event.ToolResult.ToolCallID {
						tool.result = event.ToolResult
					}
				}
			}
			if event.Compaction != nil {
				m.pendingCompaction = event.Compaction
			}
			m.streamThinking += event.Thinking
			m.streamContent += event.Content
			m.invalidateChat()
			m.autosave(event.ToolResult != nil)
			return m, waitForStream(m.stream)
		}
		m.stopResponse()
		m.busy = false
		m.stream = nil
		m.streamThinking = ""
		m.streamContent = ""
		m.streamItems = nil
		m.pendingApproval = nil
		m.queued = nil
		m.session.Messages = append(m.session.Messages, msg.messages...)
		if m.pendingCompaction != nil {
			compaction := *m.pendingCompaction
			if compaction.FirstKeptMessage > len(m.session.Messages) {
				compaction.FirstKeptMessage = len(m.session.Messages)
			}
			m.session.Summary = compaction.Summary
			m.session.FirstKeptMessage = compaction.FirstKeptMessage
			m.session.SummaryReadFiles = compaction.ReadFiles
			m.session.SummaryModifiedFiles = compaction.ModifiedFiles
			m.session.SummaryTokensBefore = compaction.TokensBefore
			m.session.CostUSD += compaction.CostUSD
			m.pendingCompaction = nil
			m.invalidateTranscript()
		}
		m.invalidateTranscript()
		if msg.usage.Tokens > 0 {
			m.session.TokensUsed = msg.usage.Tokens
		}
		m.session.CostUSD += msg.usage.CostUSD
		if msg.err != nil {
			slog.Error("model request failed", "session", m.session.ID, "error", msg.err)
			m.lastErr = msg.err.Error()
		} else {
			m.lastErr = ""
		}
		m.invalidateChat()
		m.autosave(true)
		m.maybeCompact()

		// A message queued as the response ended becomes a follow-up turn.
		if leftover := m.steering.Drain(); len(leftover) > 0 {
			m.session.Messages = append(m.session.Messages, leftover...)
			m.invalidateTranscript()
			m.steering = nil
			if m.responder != nil {
				m.steering = agent.NewMessageQueue()
				cmd := m.startResponse()
				return m, cmd
			}
		}
		m.steering = nil
		return m, nil
	case tea.KeyPressMsg:
		if m.picker != nil {
			switch msg.String() {
			case "esc":
				m.closeSessionPicker()
				return m, nil
			case "up":
				m.moveSessionSelection(-1)
				return m, nil
			case "down":
				m.moveSessionSelection(1)
				return m, nil
			case "enter":
				m.openSelectedSession()
				return m, nil
			case "ctrl+c":
				m.closeSessionPicker()
				return m, nil
			case "ctrl+d":
				m.closeSessionPicker()
			default:
				return m, nil
			}
		}
		if m.modelPicker != nil {
			switch msg.String() {
			case "esc":
				m.closeModelPicker()
				return m, nil
			case "up":
				m.moveModelSelection(-1)
				return m, nil
			case "down":
				m.moveModelSelection(1)
				return m, nil
			case "enter":
				m.openSelectedModel()
				return m, nil
			case "ctrl+c":
				m.closeModelPicker()
				return m, nil
			case "ctrl+d":
				m.closeModelPicker()
			default:
				return m, nil
			}
		}
		if m.pendingApproval != nil {
			switch msg.String() {
			case "y", "Y":
				m.decideApproval(true)
				return m, nil
			case "n", "N":
				m.decideApproval(false)
				return m, nil
			case "ctrl+c", "ctrl+d":
				m.decideApproval(false)
			default:
				return m, nil
			}
		}
		switch msg.String() {
		case "ctrl+c":
			// Do not exit: stop the current response (and any tool it is
			// running, including shell commands) and return to FOLIA mode.
			m.stopResponse()
			m.requestID++
			m.busy = false
			m.stream = nil
			m.steering = nil
			m.queued = nil
			m.preserveStreamedContext()
			m.session.Mode = session.ModeFolia
			m.invalidateChat()
			m.autosave(true)
			return m, nil
		case "ctrl+d":
			m.stopResponse()
			m.autosave(true)
			return m, tea.Quit
		case "ctrl+end":
			m.follow = true
			return m, nil
		case "ctrl+o":
			m.expandTools = !m.expandTools
			m.invalidateTranscript()
			m.invalidateChat()
			return m, nil
		case "shift+tab":
			m.session.ThinkingLevel = thinking.NextIn(m.session.ThinkingLevel, m.thinkingLevels)
			if m.settingsChanged != nil {
				if err := m.settingsChanged(m.session.Provider, m.session.Model, m.session.ThinkingLevel); err != nil {
					m.lastErr = "save settings: " + err.Error()
					m.invalidateChat()
				} else if strings.HasPrefix(m.lastErr, "save settings: ") {
					m.lastErr = ""
					m.invalidateChat()
				}
			}
			m.autosave(true)
			return m, nil
		case "up", "down":
			if _, ok := m.commandQuery(); ok {
				delta := 1
				if msg.String() == "up" {
					delta = -1
				}
				m.moveCommandCursor(delta)
				return m, nil
			}
		case "enter":
			if handled, cmd := m.handleSkillCommand(); handled {
				return m, cmd
			}
			if query, ok := m.commandQuery(); ok {
				return m.runCommand(query)
			}
			if m.busy {
				m.queueMessage()
				return m, nil
			}
			if m.submit() {
				if m.responder == nil {
					return m, nil
				}
				cmd := m.startResponse()
				return m, cmd
			}
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m Model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion

	if m.width == 0 || m.height == 0 {
		return v
	}

	inputBar := m.inputBar()
	palette := m.commandPalette()
	chatHeight := m.chatHeight()

	parts := []string{m.header(), m.viewChat(chatHeight)}
	if palette != "" {
		parts = append(parts, palette)
	}
	parts = append(parts, inputBar, m.statusBar())
	v.SetContent(lipgloss.JoinVertical(lipgloss.Left, parts...))
	return v
}

// chatHeight returns the number of rows available for the chat area.
func (m Model) chatHeight() int {
	height := m.height - lipgloss.Height(m.inputBar()) - 2 // header and status bar
	if palette := m.commandPalette(); palette != "" {
		height -= lipgloss.Height(palette)
	}
	if height < 0 {
		return 0
	}
	return height
}

// visibleChatHeight returns the number of chat rows available for content,
// reserving the last row for the scroll hint when not following.
func (m Model) visibleChatHeight() int {
	height := m.chatHeight()
	if !m.follow && height > 1 {
		height--
	}
	return height
}

// scrollBy scrolls the chat and disables following the newest content.
func (m *Model) scrollBy(delta int) {
	lines := len(m.chatLines())
	if m.follow {
		m.scrollTop = max(0, lines-m.chatHeight())
		m.follow = false
	}
	maxTop := max(0, lines-m.visibleChatHeight())
	m.scrollTop += delta
	if m.scrollTop < 0 {
		m.scrollTop = 0
	}
	if m.scrollTop >= maxTop {
		m.scrollTop = maxTop
		m.follow = true
	}
}

// availableTools returns the tools available in the current mode.
func (m Model) availableTools() []tools.Tool {
	if m.mode() != session.ModeFolia {
		return m.tools
	}
	available := make([]tools.Tool, 0, len(m.tools))
	for _, tool := range m.tools {
		if tool.ReadOnly {
			available = append(available, tool)
		}
	}
	return available
}

// mode returns the current tool mode, defaulting to FOLIA.
func (m Model) mode() string {
	switch m.session.Mode {
	case session.ModeNormal:
		return session.ModeNormal
	case session.ModeYolo:
		return session.ModeYolo
	default:
		return session.ModeFolia
	}
}

// decideApproval answers a pending tool approval request.
func (m *Model) decideApproval(allowed bool) {
	if m.pendingApproval == nil {
		return
	}
	select {
	case m.pendingApproval.response <- allowed:
	default:
	}
	m.pendingApproval = nil
	m.invalidateChat()
}

// header renders the top line with the application name and build version.
func (m Model) header() string {
	return appNameStyle.Render("Foliarz") + " " + versionStyle.Render(buildinfo.String())
}

// runCommand executes the highlighted command matching query.
func (m Model) runCommand(query string) (tea.Model, tea.Cmd) {
	matches := commands.Match(query)
	index := m.commandIndex(matches)
	if index < 0 {
		return m, nil
	}

	command := matches[index]
	m.input.Reset()
	m.commandCursor = 0

	action := command.Run()
	if m.busy {
		switch action {
		case commands.ActionSetFolia:
			// /folia is a hard read-only switch: stop the run before dropping
			// to FOLIA so no further mutating tool call can execute.
			m.stopResponse()
			m.requestID++
			m.busy = false
			m.stream = nil
			m.steering = nil
			m.queued = nil
			m.preserveStreamedContext()
			m.session.Mode = session.ModeFolia
			m.invalidateInfo()
			m.invalidateChat()
			m.autosave(true)
			return m, nil
		case commands.ActionSetNormal, commands.ActionSetYolo:
			m.lastErr = "cannot start " + actionName(action) + " mode while a response is running; press ctrl+c or /folia to stop it first"
			m.invalidateChat()
			return m, nil
		}
	}

	switch action {
	case commands.ActionQuit:
		m.stopResponse()
		m.autosave(true)
		return m, tea.Quit
	case commands.ActionSetFolia:
		m.session.Mode = session.ModeFolia
		m.invalidateInfo()
		m.autosave(true)
	case commands.ActionSetNormal:
		m.session.Mode = session.ModeNormal
		m.invalidateInfo()
		m.autosave(true)
	case commands.ActionSetYolo:
		m.session.Mode = session.ModeYolo
		m.invalidateInfo()
		m.autosave(true)
	case commands.ActionContinue:
		if m.busy || m.responder == nil || len(m.session.Messages) == 0 {
			return m, nil
		}
		m.lastErr = ""
		m.maybeCompact()
		cmd := m.startResponse()
		return m, cmd
	case commands.ActionListSessions:
		m.openSessionPicker()
		return m, nil
	case commands.ActionSelectModel:
		m.openModelPicker()
		return m, nil
	case commands.ActionCompact:
		if m.busy {
			m.lastErr = "cannot compact while a response is running"
			m.invalidateChat()
			return m, nil
		}
		if err := m.compactContext(""); err != nil {
			slog.Error("manual compaction failed", "session", m.session.ID, "error", err)
			m.lastErr = "compact context: " + err.Error()
			m.invalidateChat()
		}
		return m, nil
	case commands.ActionNewSession:
		m.stopResponse()
		if err := m.startNewSession(); err != nil {
			m.lastErr = "new session: " + err.Error()
			m.busy = false
			m.invalidateChat()
			return m, nil
		}
		m.busy = false
		m.lastErr = ""
		m.stream = nil
		m.streamThinking = ""
		m.streamContent = ""
		m.streamItems = nil
		m.queued = nil
		m.steering = nil
		m.pendingApproval = nil
		m.pendingCompaction = nil
		m.showInfo = true
		m.follow = true
		m.invalidateTranscript()
		m.invalidateInfo()
		m.autosave(true)
		m.requestID++
	}
	return m, nil
}

// startNewSession replaces the current session with a fresh one.
func (m *Model) startNewSession() error {
	var fresh *session.Session
	if m.newSession != nil {
		fresh = m.newSession(m.session.WorkDir)
	} else {
		old := m.session
		fresh = session.New(old.WorkDir)
		fresh.Provider = old.Provider
		fresh.Model = old.Model
		fresh.ThinkingLevel = old.ThinkingLevel
		fresh.ContextWindow = old.ContextWindow
	}
	if m.projectLoader != nil {
		projectContext, err := m.projectLoader(fresh.WorkDir)
		if err != nil {
			return fmt.Errorf("load project context: %w", err)
		}
		fresh.ProjectContext = projectContext
	}
	if err := m.bindSession(fresh); err != nil {
		return err
	}
	m.session = fresh
	return nil
}

// bindSession switches the responder and thinking levels to the session model.
func (m *Model) bindSession(sess *session.Session) error {
	if m.responderFactory == nil {
		return nil
	}
	responder, levels, err := m.responderFactory(sess.Provider, sess.Model)
	if err != nil {
		return err
	}
	if responder == nil {
		return fmt.Errorf("no responder for %s/%s", sess.Provider, sess.Model)
	}
	m.responder = responder
	if compactor, ok := responder.(Compactor); ok {
		m.compactor = compactor
	} else {
		m.compactor = nil
	}
	if len(levels) > 0 {
		m.thinkingLevels = slices.Clone(levels)
	}
	// The new model may not support the current level; fall back to its first
	// supported level so reasoning is never silently disabled.
	if !slices.Contains(m.thinkingLevels, sess.ThinkingLevel) {
		sess.ThinkingLevel = m.thinkingLevels[0]
	}
	return nil
}

// startResponse starts a streaming model request and returns a command that
// waits for its first message.
func (m *Model) startResponse() tea.Cmd {
	if m.cancel != nil {
		m.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	ctx = tools.WithWorkDir(ctx, m.session.WorkDir)
	ctx = agent.WithSessionID(ctx, m.session.ID)
	m.cancel = cancel

	m.requestID++
	m.busy = true
	m.lastErr = ""
	m.streamThinking = ""
	m.streamContent = ""
	m.streamItems = nil
	m.queued = nil
	m.pendingApproval = nil
	m.pendingCompaction = nil
	m.stream = make(chan streamMsg, 64)
	if m.steering == nil {
		m.steering = agent.NewMessageQueue()
	}
	m.invalidateChat()

	requestID := m.requestID
	responder := m.responder
	history := slices.Clone(m.session.Messages)
	thinkingLevel := m.session.ThinkingLevel
	mode := m.mode()
	projectContext := m.session.ProjectContext
	steering := m.steering
	stream := m.stream

	approve := func(_ context.Context, call session.ToolCall) (bool, error) {
		request := &approvalRequest{call: call, response: make(chan bool, 1)}
		select {
		case stream <- streamMsg{requestID: requestID, approval: request}:
		case <-ctx.Done():
			return false, ctx.Err()
		}
		select {
		case allowed := <-request.response:
			return allowed, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}

	go func() {
		defer close(stream)

		messages, usage, err := responder.Respond(ctx, history, thinkingLevel, mode, projectContext, agent.CompactionContext{
			Summary:          m.session.Summary,
			FirstKeptMessage: m.session.FirstKeptMessage,
			ReadFiles:        slices.Clone(m.session.SummaryReadFiles),
			ModifiedFiles:    slices.Clone(m.session.SummaryModifiedFiles),
			ContextWindow:    m.session.ContextWindow,
		}, steering, func(event agent.Event) {
			select {
			case stream <- streamMsg{requestID: requestID, event: event}:
			case <-ctx.Done():
			}
		}, approve)

		select {
		case stream <- streamMsg{
			requestID: requestID,
			done:      true,
			messages:  messages,
			usage:     usage,
			err:       err,
		}:
		case <-ctx.Done():
		}
	}()

	return waitForStream(stream)
}

// waitForStream waits for the next message of a streaming response.
func waitForStream(stream <-chan streamMsg) tea.Cmd {
	if stream == nil {
		return nil
	}
	return func() tea.Msg {
		message, ok := <-stream
		if !ok {
			return nil
		}
		return message
	}
}

// stopResponse cancels an in-flight model request.
func (m *Model) stopResponse() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

// preserveStreamedContext appends the completed streamed steps and the partial
// current answer to the session, so ctrl+c stops the response without losing
// the context built so far.
func (m *Model) preserveStreamedContext() {
	m.session.Messages = append(m.session.Messages, m.streamedMessages()...)
	m.streamThinking = ""
	m.streamContent = ""
	m.streamItems = nil
	m.invalidateTranscript()
}

// archiveStream moves the currently streamed assistant message into the
// archive, so earlier thinking stays visible while a tool step runs.
func (m *Model) archiveStream() {
	thinking := strings.TrimRight(m.streamThinking, "\n")
	content := strings.TrimRight(m.streamContent, "\n")
	if thinking != "" || content != "" {
		message := session.Message{
			Role:     "assistant",
			Thinking: thinking,
			Content:  content,
		}
		m.streamItems = append(m.streamItems, streamItem{message: &message})
	}
	m.streamThinking = ""
	m.streamContent = ""
}

// submit appends the input to the session transcript and clears the input.
func (m *Model) submit() bool {
	text := strings.TrimRight(m.input.Value(), "\n")
	if strings.TrimSpace(text) == "" {
		return false
	}
	m.maybeCompact()
	m.session.Messages = append(m.session.Messages, session.Message{Role: "user", Content: text})
	m.input.Reset()
	m.lastErr = ""
	m.showInfo = false
	m.invalidateTranscript()
	m.invalidateChat()
	m.autosave(true)
	return true
}

// resizeInput fits the input bar to the terminal.
func (m *Model) resizeInput() {
	if m.width > 0 {
		m.input.SetWidth(m.width)
	}
	if m.height > 0 {
		m.input.MaxHeight = maxInputHeight(m.height)
		if m.input.Height() > m.input.MaxHeight {
			m.input.SetHeight(m.input.MaxHeight)
		}
	}
}

// maxInputHeight returns how many rows the input may occupy for a terminal
// with the given height, leaving room for the header, the rules, the status bar
// and at least one row of chat.
func maxInputHeight(termHeight int) int {
	const maxRows = 10
	if termHeight <= 0 {
		return maxRows
	}
	rows := termHeight - 5
	if rows < 1 {
		rows = 1
	}
	if rows > maxRows {
		rows = maxRows
	}
	return rows
}

// actionName returns the mode name a mode action selects.
func actionName(action commands.Action) string {
	if action == commands.ActionSetYolo {
		return "YOLO"
	}
	return "NORMAL"
}

// queueMessage queues the input as a steering message for the running response.
func (m *Model) queueMessage() {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return
	}
	if m.steering == nil {
		m.steering = agent.NewMessageQueue()
	}
	message := session.Message{Role: "user", Content: text}
	m.steering.Push(message)
	m.queued = append(m.queued, message)
	m.input.Reset()
	m.invalidateChat()
	m.autosave(true)
}
