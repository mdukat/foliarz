package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	"github.com/mdukat/foliarz/internal/session"
)

var (
	thinkingStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	toolSuccessStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#225D48")).
				Foreground(lipgloss.Color("#FFFFFF")).
				Padding(0, 1)
	toolErrorStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#62331D")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1)
	toolPendingStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#3A3D46")).
				Foreground(lipgloss.Color("#FFFFFF")).
				Padding(0, 1)
	systemPromptStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#242A56")).
				Foreground(lipgloss.Color("#FFFFFF")).
				Padding(0, 1)
	approvalStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#5A4A12")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1)
)

const toolPreviewLines = 6

// chatCache caches rendered chat lines. The transcript and session info are
// expensive to render, so they are cached separately and reused while the model
// streams or the user scrolls.
type chatCache struct {
	lines           []string
	dirty           bool
	transcript      []string
	transcriptDirty bool
	info            []string
	infoDirty       bool
}

func (m *Model) invalidateChat() {
	if m.cache != nil {
		m.cache.dirty = true
	}
}

func (m *Model) invalidateTranscript() {
	if m.cache != nil {
		m.cache.dirty = true
		m.cache.transcriptDirty = true
	}
}

func (m *Model) invalidateInfo() {
	if m.cache != nil {
		m.cache.dirty = true
		m.cache.infoDirty = true
	}
}

// chatLines returns the rendered chat lines, rebuilding them only when the
// content changed. Scrolling reuses the cached lines.
func (m Model) chatLines() []string {
	if m.cache == nil {
		return nil
	}
	cache := m.cache
	if !cache.dirty {
		return cache.lines
	}

	if cache.transcriptDirty {
		cache.transcript = splitChatLines(m.renderTranscript())
		cache.transcriptDirty = false
	}
	if cache.infoDirty {
		cache.info = splitChatLines(m.renderInfo())
		cache.infoDirty = false
	}

	var lines []string
	add := func(block []string) {
		if len(block) == 0 {
			return
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, block...)
	}

	if m.showInfo {
		add(cache.info)
	}
	add(cache.transcript)
	if m.busy {
		add(splitChatLines(m.streamView()))
	}
	if m.pendingApproval != nil {
		add(splitChatLines(m.approvalView()))
	}
	if m.lastErr != "" {
		add(splitChatLines(errorStyle.Render("error: " + m.lastErr)))
	}

	cache.lines = lines
	cache.dirty = false
	return lines
}

func splitChatLines(content string) []string {
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

// viewChat renders the visible part of the chat area.
func (m Model) viewChat(height int) string {
	if height <= 0 {
		return ""
	}
	if m.picker != nil {
		return m.viewSessionPicker(height)
	}
	if m.modelPicker != nil {
		return m.viewModelPicker(height)
	}
	lines := m.chatLines()
	if len(lines) == 0 {
		return lipgloss.Place(m.width, height, lipgloss.Left, lipgloss.Bottom, "")
	}

	contentHeight := height
	hint := ""
	if !m.follow && height > 1 {
		contentHeight = height - 1
		hint = m.scrollHint()
	}

	maxTop := len(lines) - contentHeight
	if maxTop < 0 {
		maxTop = 0
	}
	start := maxTop
	if !m.follow {
		start = m.scrollTop
		if start < 0 {
			start = 0
		}
		if start > maxTop {
			start = maxTop
		}
	}
	end := start + contentHeight
	if end > len(lines) {
		end = len(lines)
	}

	body := strings.Join(lines[start:end], "\n")
	if hint != "" {
		body += "\n" + hint
	}
	return lipgloss.Place(m.width, height, lipgloss.Left, lipgloss.Bottom, body)
}

// scrollHint tells the user how to return to the newest content.
func (m Model) scrollHint() string {
	return versionStyle.
		Width(m.width).
		Align(lipgloss.Right).
		Render("↑ scrolled · ctrl+end to jump to newest")
}

// renderInfo renders the session start information: the mode, the available
// tools and the system prompt.
func (m Model) renderInfo() string {
	var builder strings.Builder
	prompt := strings.TrimSpace(m.systemPrompt)
	if projectContext := strings.TrimSpace(m.session.ProjectContext); projectContext != "" {
		if prompt != "" {
			prompt += "\n\n"
		}
		prompt += projectContext
	}
	if prompt == "" {
		prompt = "(empty)"
	}
	promptWidth := m.width - 2
	if promptWidth < 1 {
		promptWidth = 1
	}
	builder.WriteString(systemPromptStyle.Width(promptWidth).Render("system prompt\n" + prompt))

	builder.WriteString("\n\n" + roleStyle.Render("tools") + versionStyle.Render(" · "+m.mode()))
	available := m.availableTools()
	if len(available) == 0 {
		builder.WriteString("\n  (none)")
	}
	for _, tool := range available {
		builder.WriteString("\n  " + tool.Name + " — " + tool.Description)
	}

	builder.WriteString("\n" + roleStyle.Render("skills"))
	if len(m.skills) == 0 {
		builder.WriteString("\n  (none)")
	}
	for _, skill := range m.skills {
		line := "  " + skill.Name
		if skill.RequiresShell {
			line += " [shell]"
		}
		if skill.DisableModelInvocation {
			line += " [manual]"
		}
		builder.WriteString("\n" + truncate(line, m.width))
	}
	return builder.String()
}

// approvalView renders the NORMAL-mode tool approval prompt.
func (m Model) approvalView() string {
	call := m.pendingApproval.call
	arguments := strings.TrimSpace(call.Arguments)
	if arguments == "" {
		arguments = "{}"
	}
	width := m.width - 2
	if width < 1 {
		width = 1
	}
	return approvalStyle.Width(width).Render(
		"allow tool call? NORMAL mode\n" +
			call.Name + " " + arguments + "\n" +
			"press y to allow, n to deny",
	)
}

// streamView renders all assistant messages streamed so far, including the
// thinking of completed tool steps and the message currently being streamed.
func (m Model) streamView() string {
	var blocks []string
	for _, item := range m.streamItems {
		var block string
		switch {
		case item.message != nil:
			block = m.streamBlock(item.message.Thinking, item.message.Content)
		case item.tool != nil:
			block = m.toolBlock(item.tool.call, item.tool.result)
		}
		if block != "" {
			blocks = append(blocks, block)
		}
	}
	if block := m.streamBlock(m.streamThinking, m.streamContent); block != "" {
		blocks = append(blocks, block)
	}
	for _, message := range m.queued {
		blocks = append(blocks, m.queuedBlock(message))
	}
	if len(blocks) == 0 {
		return versionStyle.Render("waiting for the model…")
	}
	return strings.Join(blocks, "\n\n")
}

// queuedBlock renders a user message waiting to steer the running response.
func (m Model) queuedBlock(message session.Message) string {
	width := m.width
	if width < 1 {
		width = 1
	}
	return roleStyle.Render("user") + versionStyle.Render(" (queued)") + "\n" +
		lipgloss.NewStyle().Width(width).Render(message.Content)
}

func (m Model) streamBlock(thinking, content string) string {
	thinking = strings.TrimRight(thinking, "\n")
	if m.hideThinking {
		thinking = ""
	}
	content = strings.TrimRight(content, "\n")
	if thinking == "" && content == "" {
		return ""
	}

	block := roleStyle.Render("foliarz")
	if thinking != "" {
		block += "\n" + thinkingStyle.Width(m.width).Render(thinking)
	}
	if content != "" {
		block += "\n" + lipgloss.NewStyle().Width(m.width).Render(content)
	}
	return block
}

// renderTranscript renders all session messages wrapped to the terminal width.
func (m Model) renderTranscript() string {
	if len(m.session.Messages) == 0 {
		return ""
	}

	results := make(map[string]*session.Message, len(m.session.Messages))
	for i := range m.session.Messages {
		message := &m.session.Messages[i]
		if message.Role == "tool" {
			results[message.ToolCallID] = message
		}
	}

	var blocks []string
	for _, message := range m.session.Messages {
		if message.Role == "tool" {
			continue
		}

		content := strings.TrimRight(message.Content, "\n")
		thinking := strings.TrimSpace(message.Thinking)
		if m.hideThinking {
			thinking = ""
		}
		hasContent := strings.TrimSpace(content) != ""
		if !hasContent && thinking == "" && len(message.ToolCalls) == 0 {
			continue
		}

		var parts []string
		if hasContent || thinking != "" {
			parts = append(parts, roleStyle.Render(displayRole(message.Role)))
		}
		if thinking != "" {
			parts = append(parts, thinkingStyle.Width(m.width).Render(thinking))
		}
		if hasContent {
			parts = append(parts, m.formatContent(message.Role, content))
		}
		if len(message.ToolCalls) > 0 {
			toolBlocks := make([]string, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				toolBlocks = append(toolBlocks, m.toolBlock(call, results[call.ID]))
			}
			parts = append(parts, strings.Join(toolBlocks, "\n\n"))
		}
		if len(parts) > 0 {
			blocks = append(blocks, strings.Join(parts, "\n"))
		}
	}
	return strings.Join(blocks, "\n\n")
}

// toolBlock renders a tool call and its result on a green (success) or red
// (error) background, like Pi Agent.
func (m Model) toolBlock(call session.ToolCall, result *session.Message) string {
	style := toolSuccessStyle
	switch {
	case result == nil:
		style = toolPendingStyle
	case result.IsError || strings.HasPrefix(strings.TrimSpace(result.Content), "error: "):
		style = toolErrorStyle
	}

	lines := []string{toolCallTitle(call)}
	if result == nil {
		lines = append(lines, "running…")
	} else {
		output := strings.TrimRight(result.Content, "\n")
		if output == "" {
			output = "(no output)"
		}
		outputLines := strings.Split(output, "\n")
		if !m.expandTools && len(outputLines) > toolPreviewLines {
			hidden := len(outputLines) - toolPreviewLines
			outputLines = append(outputLines[:toolPreviewLines], fmt.Sprintf("… (%d more lines)", hidden))
		}
		lines = append(lines, outputLines...)
		if call.Name == "bash" && result.DurationMs != nil {
			lines = append(lines, "", "Took "+formatDuration(*result.DurationMs))
		}
	}

	width := m.width - 2
	if width < 1 {
		width = 1
	}
	return style.Width(width).Render(strings.Join(lines, "\n"))
}

// toolCallTitle summarizes the arguments of a tool call for display.
func toolCallTitle(call session.ToolCall) string {
	var args map[string]any
	_ = json.Unmarshal([]byte(call.Arguments), &args)

	switch call.Name {
	case "bash":
		if command, _ := args["command"].(string); command != "" {
			title := "$ " + command
			if timeout, ok := args["timeout"].(float64); ok && timeout > 0 {
				title += " (timeout " + formatTimeout(timeout) + "s)"
			}
			return title
		}
	case "read", "write", "edit", "ls":
		if path, _ := args["path"].(string); path != "" {
			return call.Name + " " + path
		}
	}
	trimmed := strings.TrimSpace(call.Arguments)
	if trimmed != "" && trimmed != "{}" {
		return call.Name + " " + trimmed
	}
	return call.Name
}

// formatDuration formats a duration in milliseconds like Pi Agent.
func formatDuration(ms int64) string {
	seconds := float64(ms) / 1000
	if seconds < 60 {
		return fmt.Sprintf("%.1fs", seconds)
	}
	totalSeconds := int64(seconds)
	minutes := totalSeconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm %ds", minutes, totalSeconds%60)
	}
	return fmt.Sprintf("%dh %dm %ds", minutes/60, minutes%60, totalSeconds%60)
}

// formatTimeout formats a timeout in seconds without trailing zeros.
func formatTimeout(seconds float64) string {
	if seconds == float64(int64(seconds)) {
		return fmt.Sprintf("%d", int64(seconds))
	}
	return fmt.Sprintf("%g", seconds)
}

// displayRole maps a message role to the name shown in the transcript.
func displayRole(role string) string {
	switch role {
	case "assistant":
		return "foliarz"
	case "":
		return "message"
	default:
		return role
	}
}

// formatContent renders a message body. Assistant answers are rendered as
// markdown; other messages are wrapped as plain text.
func (m Model) formatContent(role, content string) string {
	if role != "assistant" {
		return lipgloss.NewStyle().Width(m.width).Render(content)
	}

	wrapWidth := m.width - 4
	if wrapWidth < 20 {
		return lipgloss.NewStyle().Width(m.width).Render(content)
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(wrapWidth),
	)
	if err != nil {
		return lipgloss.NewStyle().Width(m.width).Render(content)
	}
	rendered, err := renderer.Render(content)
	if err != nil {
		return lipgloss.NewStyle().Width(m.width).Render(content)
	}
	return strings.Trim(rendered, "\n")
}
