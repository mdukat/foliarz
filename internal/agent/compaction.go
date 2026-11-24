package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mdukat/foliarz/internal/llm"
	"github.com/mdukat/foliarz/internal/session"
)

const (
	compactionSummaryPrefix = "The conversation history before this point was compacted into the following summary:\n\n<summary>\n"
	compactionSummarySuffix = "\n</summary>"
	toolResultMaxChars      = 2000
)

const summarizationSystemPrompt = `You are a context summarization assistant. Your task is to read a conversation between a user and an AI assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`

const summarizationPrompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

const updateSummarizationPrompt = `The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.

Update the existing structured summary with new information. RULES:
- PRESERVE all existing information from the previous summary
- ADD new progress, decisions, and context from the new messages
- UPDATE the Progress section: move items from "In Progress" to "Done" when completed
- UPDATE "Next Steps" based on what was accomplished
- PRESERVE exact file paths, function names, and error messages
- If something is no longer relevant, you may remove it

Use this EXACT format:

## Goal
[Preserve existing goals, add new ones if the task expanded]

## Constraints & Preferences
- [Preserve existing, add new ones discovered]

## Progress
### Done
- [x] [Include previously done items AND newly completed items]

### In Progress
- [ ] [Current work - update based on progress]

### Blocked
- [Current blockers - remove if resolved]

## Key Decisions
- **[Decision]**: [Brief rationale] (preserve all previous, add new)

## Next Steps
1. [Update based on current state]

## Critical Context
- [Preserve important context, add new if needed]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

// CompactionInput describes a compaction request.
type CompactionInput struct {
	History          []session.Message
	Summary          string
	ReadFiles        []string
	ModifiedFiles    []string
	FirstKept        int
	KeepRecentTokens int
	ThinkingLevel    string
	Instructions     string
}

// CompactionResult is the outcome of a compaction.
type CompactionResult struct {
	Summary              string
	FirstKeptMessage     int
	TokensBefore         int
	EstimatedTokensAfter int
	ReadFiles            []string
	ModifiedFiles        []string
	// CostUSD is the spend of the summarization request.
	CostUSD float64
}

// Compact summarizes older messages and returns the retained boundary.
func (a *Agent) Compact(ctx context.Context, input CompactionInput) (CompactionResult, error) {
	firstKept := input.FirstKept
	if firstKept < 0 {
		firstKept = 0
	}
	if firstKept > len(input.History) {
		firstKept = len(input.History)
	}
	cut := findCompactionCut(input.History, firstKept, input.KeepRecentTokens)
	if cut <= firstKept {
		return CompactionResult{}, fmt.Errorf("nothing to compact")
	}

	messagesToSummarize := input.History[firstKept:cut]
	tokensBefore := estimateContextTokens(input.History, input.Summary, firstKept)
	files := newFileOperations(input.ReadFiles, input.ModifiedFiles)
	trackFileOperations(messagesToSummarize, files)

	previousSummary := stripFileOperations(input.Summary)
	summary, summaryCost, err := a.generateSummary(ctx, messagesToSummarize, previousSummary, input.Instructions, input.ThinkingLevel)
	if err != nil {
		return CompactionResult{}, err
	}

	readFiles, modifiedFiles := files.lists()
	summary += formatFileOperations(readFiles, modifiedFiles)

	estimatedAfter := estimateTextTokens(summary)
	for _, message := range input.History[cut:] {
		estimatedAfter += estimateMessageTokens(message)
	}

	return CompactionResult{
		Summary:              summary,
		FirstKeptMessage:     cut,
		TokensBefore:         tokensBefore,
		EstimatedTokensAfter: estimatedAfter,
		ReadFiles:            readFiles,
		ModifiedFiles:        modifiedFiles,
		CostUSD:              summaryCost,
	}, nil
}

// compactionContextMessage renders a summary as a user context message, like
// Pi Agent.
func compactionContextMessage(summary string) llm.Message {
	return llm.Message{
		Role:    "user",
		Content: compactionSummaryPrefix + summary + compactionSummarySuffix,
	}
}

func (a *Agent) generateSummary(ctx context.Context, messages []session.Message, previousSummary, instructions, thinkingLevel string) (string, float64, error) {
	basePrompt := summarizationPrompt
	if previousSummary != "" {
		basePrompt = updateSummarizationPrompt
	}
	if instructions != "" {
		basePrompt += "\n\nAdditional focus: " + instructions
	}

	var prompt strings.Builder
	prompt.WriteString("<conversation>\n")
	prompt.WriteString(serializeConversation(messages))
	prompt.WriteString("\n</conversation>\n\n")
	if previousSummary != "" {
		prompt.WriteString("<previous-summary>\n")
		prompt.WriteString(previousSummary)
		prompt.WriteString("\n</previous-summary>\n\n")
	}
	prompt.WriteString(basePrompt)

	extra := cloneParameters(a.model.SamplingParams)
	if extra == nil {
		extra = make(map[string]any)
	}
	reasoning := a.reasoningFor(thinkingLevel, extra)
	if len(extra) == 0 {
		extra = nil
	}

	response, err := a.client.ChatStream(ctx, llm.Request{
		Model: a.model.ID,
		Messages: []llm.Message{
			{Role: "system", Content: summarizationSystemPrompt},
			{Role: "user", Content: prompt.String()},
		},
		Reasoning: reasoning,
		Provider:  a.providerRouting(),
		MaxTokens: a.model.MaxTokens,
		Extra:     extra,
		Headers:   a.sessionAffinityHeaders(ctx),
	}, nil)
	if err != nil {
		return "", 0, fmt.Errorf("summarize context: %w", err)
	}
	cost := a.requestCost(response.Usage)
	choice := response.Choices[0]
	if len(choice.Message.ToolCalls) > 0 {
		return "", cost, fmt.Errorf("summarization attempted to call a tool")
	}
	text := strings.TrimSpace(choice.Message.Content)
	if text == "" {
		return "", cost, fmt.Errorf("summarization returned an empty summary")
	}
	return text, cost, nil
}

func findCompactionCut(messages []session.Message, start, keepRecentTokens int) int {
	var cutPoints []int
	for i := start; i < len(messages); i++ {
		if messages[i].Role == "user" || messages[i].Role == "assistant" {
			cutPoints = append(cutPoints, i)
		}
	}
	if len(cutPoints) == 0 {
		return start
	}

	accumulated := 0
	cut := start
	reached := false
	for i := len(messages) - 1; i >= start; i-- {
		accumulated += estimateMessageTokens(messages[i])
		if accumulated < keepRecentTokens {
			continue
		}
		for _, candidate := range cutPoints {
			if candidate >= i {
				cut = candidate
				reached = true
				break
			}
		}
		if !reached {
			cut = cutPoints[len(cutPoints)-1]
			reached = true
		}
		break
	}
	if !reached {
		return start
	}
	return cut
}

func estimateContextTokens(messages []session.Message, summary string, firstKept int) int {
	tokens := estimateTextTokens(summary)
	for i := firstKept; i < len(messages); i++ {
		tokens += estimateMessageTokens(messages[i])
	}
	return tokens
}

func estimateMessageTokens(message session.Message) int {
	chars := len(message.Content) + len(message.Thinking)
	for _, detail := range message.ReasoningDetails {
		chars += len(detail)
	}
	for _, call := range message.ToolCalls {
		chars += len(call.Name) + len(call.Arguments)
	}
	return estimateCharsTokens(chars)
}

func estimateTextTokens(text string) int {
	return estimateCharsTokens(len(text))
}

func estimateCharsTokens(chars int) int {
	return (chars + 3) / 4
}

func serializeConversation(messages []session.Message) string {
	var parts []string
	for _, message := range messages {
		switch message.Role {
		case "user":
			content := strings.TrimSpace(message.Content)
			if content != "" {
				parts = append(parts, "[User]: "+content)
			}
		case "assistant":
			content := strings.TrimSpace(message.Content)
			if content != "" {
				parts = append(parts, "[Assistant]: "+content)
			}
			var calls []string
			for _, call := range message.ToolCalls {
				calls = append(calls, formatToolCall(call))
			}
			if len(calls) > 0 {
				parts = append(parts, "[Assistant tool calls]: "+strings.Join(calls, "; "))
			}
		case "tool":
			content := strings.TrimSpace(message.Content)
			if content != "" {
				parts = append(parts, "[Tool result]: "+truncateForSummary(content, toolResultMaxChars))
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func formatToolCall(call session.ToolCall) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil || len(args) == 0 {
		return call.Name + "()"
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		encoded, _ := json.Marshal(args[key])
		parts = append(parts, key+"="+string(encoded))
	}
	return call.Name + "(" + strings.Join(parts, ", ") + ")"
}

func truncateForSummary(text string, maxChars int) string {
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	return string(runes[:maxChars]) + fmt.Sprintf("\n\n[... %d more characters truncated]", len(runes)-maxChars)
}

type fileOperations struct {
	read     map[string]bool
	modified map[string]bool
}

func newFileOperations(readFiles, modifiedFiles []string) *fileOperations {
	ops := &fileOperations{read: make(map[string]bool), modified: make(map[string]bool)}
	for _, file := range readFiles {
		ops.read[file] = true
	}
	for _, file := range modifiedFiles {
		ops.modified[file] = true
	}
	return ops
}

func trackFileOperations(messages []session.Message, ops *fileOperations) {
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			var args struct {
				Path string `json:"path"`
			}
			if json.Unmarshal([]byte(call.Arguments), &args) != nil || args.Path == "" {
				continue
			}
			switch call.Name {
			case "read":
				ops.read[args.Path] = true
			case "write", "edit":
				ops.modified[args.Path] = true
			}
		}
	}
}

func (f *fileOperations) lists() (readFiles, modifiedFiles []string) {
	for file := range f.modified {
		modifiedFiles = append(modifiedFiles, file)
		delete(f.read, file)
	}
	for file := range f.read {
		readFiles = append(readFiles, file)
	}
	sort.Strings(readFiles)
	sort.Strings(modifiedFiles)
	return readFiles, modifiedFiles
}

func formatFileOperations(readFiles, modifiedFiles []string) string {
	var sections []string
	if len(readFiles) > 0 {
		sections = append(sections, "<read-files>\n"+strings.Join(readFiles, "\n")+"\n</read-files>")
	}
	if len(modifiedFiles) > 0 {
		sections = append(sections, "<modified-files>\n"+strings.Join(modifiedFiles, "\n")+"\n</modified-files>")
	}
	if len(sections) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(sections, "\n\n")
}

func stripFileOperations(summary string) string {
	for _, marker := range []string{"<read-files>", "<modified-files>"} {
		if index := strings.Index(summary, marker); index >= 0 {
			summary = summary[:index]
		}
	}
	return strings.TrimRight(summary, "\n ")
}
