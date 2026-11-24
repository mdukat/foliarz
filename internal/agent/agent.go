// Package agent coordinates model requests and tool calls.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mdukat/foliarz/internal/config"
	"github.com/mdukat/foliarz/internal/llm"
	"github.com/mdukat/foliarz/internal/session"
	"github.com/mdukat/foliarz/internal/skills"
	"github.com/mdukat/foliarz/internal/tools"
)

type contextKey int

const sessionIDKey contextKey = iota

// WithSessionID attaches the current session ID to ctx.
func WithSessionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionIDKey, id)
}

func sessionIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(sessionIDKey).(string)
	return id
}

// CompactionContext identifies the summarized prefix of the conversation.
type CompactionContext struct {
	Summary          string
	FirstKeptMessage int
	ReadFiles        []string
	ModifiedFiles    []string
	ContextWindow    int
}

// Usage is the token and cost usage of a response.
type Usage struct {
	// Tokens is the context size of the last request.
	Tokens int
	// CostUSD is the spend of all requests made during the response.
	CostUSD float64
}

// Approver asks the user whether a tool call may run.
type Approver func(ctx context.Context, call session.ToolCall) (bool, error)

// Event is an incremental update produced while the model is answering.
type Event struct {
	// Reset archives the streamed answer and thinking, e.g. before a tool call
	// or a new model step.
	Reset bool

	// Thinking is a reasoning delta.
	Thinking string

	// Content is an answer delta.
	Content string

	// ToolStart is set when a tool is about to run.
	ToolStart *session.ToolCall

	// ToolResult is set when a tool has finished.
	ToolResult *session.Message

	// Compaction is set when older context was compacted between tool steps.
	Compaction *CompactionResult
}

// Option configures an Agent.
type Option func(*Agent)

// WithCompaction enables mid-turn context compaction.
func WithCompaction(enabled bool, reserveTokens, keepRecentTokens int) Option {
	return func(a *Agent) {
		a.compactionEnabled = enabled
		a.compactionReserveTokens = reserveTokens
		a.compactionKeepRecentTokens = keepRecentTokens
	}
}

// Agent turns session history into model requests and executes tool calls.
type Agent struct {
	client       *llm.Client
	model        *config.Model
	systemPrompt string
	tools        []tools.Tool
	skillSection string

	compactionEnabled          bool
	compactionReserveTokens    int
	compactionKeepRecentTokens int
}

// New creates an agent for the given model and tool set.
func New(client *llm.Client, model *config.Model, systemPrompt string, toolset []tools.Tool, skillSet []skills.Skill, opts ...Option) *Agent {
	agent := &Agent{
		client:       client,
		model:        model,
		systemPrompt: systemPrompt,
		tools:        toolset,
		skillSection: skills.PromptSection(skillSet),
	}
	for _, opt := range opts {
		opt(agent)
	}
	return agent
}

// Respond asks the model for an answer, running tool calls until the model
// returns text. Deltas are passed to emit while the model streams. It returns
// the new messages produced by the model and the context token usage of the
// last request.
func (a *Agent) Respond(ctx context.Context, history []session.Message, thinkingLevel, mode, projectContext string, compaction CompactionContext, steering *MessageQueue, emit func(Event), approve Approver) ([]session.Message, Usage, error) {
	system := a.systemPrompt
	if strings.TrimSpace(projectContext) != "" {
		system += "\n\n" + projectContext
	}
	if a.skillSection != "" {
		system += "\n\n" + a.skillSection
	}
	system += "\n\n" + modeInstruction(mode)

	firstKept := compaction.FirstKeptMessage
	if firstKept < 0 {
		firstKept = 0
	}
	if firstKept > len(history) {
		firstKept = len(history)
	}

	summary := compaction.Summary
	readFiles := append([]string(nil), compaction.ReadFiles...)
	modifiedFiles := append([]string(nil), compaction.ModifiedFiles...)
	local := append([]session.Message(nil), history[firstKept:]...)
	fullStart := firstKept
	replay := a.reasoningReplay()

	buildMessages := func() []llm.Message {
		messages := make([]llm.Message, 0, len(local)+2)
		if strings.TrimSpace(system) != "" {
			messages = append(messages, llm.Message{Role: "system", Content: system})
		}
		if strings.TrimSpace(summary) != "" {
			messages = append(messages, compactionContextMessage(summary))
		}
		for _, message := range local {
			// Assistant blocks with no content, no tool calls and no replayable
			// reasoning carry nothing the provider can use.
			if message.Role == "assistant" &&
				strings.TrimSpace(message.Content) == "" &&
				len(message.ToolCalls) == 0 &&
				!replay.usable(message) {
				continue
			}
			messages = append(messages, toLLMMessage(message, replay))
		}
		return messages
	}

	availableTools := a.toolsForMode(mode)
	toolDefinitions := a.toolDefinitions(availableTools)
	extra := cloneParameters(a.model.SamplingParams)
	if extra == nil {
		extra = make(map[string]any)
	}
	reasoning := a.reasoningFor(thinkingLevel, extra)
	if len(extra) == 0 {
		extra = nil
	}
	routing := a.providerRouting()
	headers := a.sessionAffinityHeaders(ctx)

	produced := make([]session.Message, 0, 2)
	usage := Usage{}
	usageTokens := 0
	trailingStart := 0

	for {
		// Steering: user messages queued while the model was working are
		// injected right after the previous assistant turn and its tool calls.
		for _, message := range steering.Drain() {
			local = append(local, message)
			produced = append(produced, message)
			usageTokens = 0
			trailingStart = 0
		}

		contextWindow := compaction.ContextWindow
		if contextWindow <= 0 {
			contextWindow = a.model.ContextWindow
		}
		projectedTokens := usageTokens
		if usageTokens == 0 {
			projectedTokens = estimateTextTokens(system) + estimateTextTokens(summary)
			for _, message := range local {
				projectedTokens += estimateMessageTokens(message)
			}
		} else {
			for i := trailingStart; i < len(local) && i >= 0; i++ {
				projectedTokens += estimateMessageTokens(local[i])
			}
		}
		if a.shouldCompactContext(projectedTokens, contextWindow) {
			result, err := a.Compact(ctx, CompactionInput{
				History:          local,
				Summary:          summary,
				ReadFiles:        readFiles,
				ModifiedFiles:    modifiedFiles,
				FirstKept:        0,
				KeepRecentTokens: a.compactionKeepRecentTokens,
				ThinkingLevel:    thinkingLevel,
			})
			if err != nil {
				slog.Warn("mid-turn context compaction failed", "error", err)
			} else if result.FirstKeptMessage > 0 {
				summary = result.Summary
				readFiles = result.ReadFiles
				modifiedFiles = result.ModifiedFiles
				local = local[result.FirstKeptMessage:]
				fullStart += result.FirstKeptMessage
				usageTokens = 0
				trailingStart = 0
				result.FirstKeptMessage = fullStart
				slog.Info("context compacted between tool steps",
					"firstKeptMessage", fullStart,
					"tokensBefore", result.TokensBefore,
					"tokensAfter", result.EstimatedTokensAfter,
				)
				if emit != nil {
					compactionResult := result
					emit(Event{Compaction: &compactionResult})
				}
			}
		}

		requestStart := len(local)
		response, err := a.client.ChatStream(ctx, llm.Request{
			Model:     a.model.ID,
			Messages:  buildMessages(),
			Tools:     toolDefinitions,
			Reasoning: reasoning,
			Provider:  routing,
			MaxTokens: a.model.MaxTokens,
			Extra:     extra,
			Headers:   headers,
		}, func(delta llm.Delta) {
			if emit != nil {
				emit(Event{Thinking: delta.Reasoning, Content: delta.Content})
			}
		})
		if err != nil {
			return produced, usage, err
		}
		usage.Tokens = response.Usage.TotalTokens
		usage.CostUSD += a.requestCost(response.Usage)
		usageTokens = usage.Tokens
		trailingStart = requestStart

		choice := response.Choices[0]
		assistant := session.Message{
			Role:             "assistant",
			Content:          choice.Message.Content,
			Thinking:         choice.Message.Reasoning,
			ReasoningDetails: choice.Message.ReasoningDetails,
			ToolCalls:        fromLLMToolCalls(choice.Message.ToolCalls),
		}
		produced = append(produced, assistant)
		local = append(local, assistant)

		if len(assistant.ToolCalls) == 0 {
			if steering.Len() == 0 {
				return produced, usage, nil
			}
			// A queued message starts another turn: archive the finished answer
			// so the live view does not concatenate both answers.
			if emit != nil {
				emit(Event{Reset: true})
			}
			continue
		}

		if emit != nil {
			emit(Event{Reset: true})
		}
		for _, call := range assistant.ToolCalls {
			if emit != nil {
				callCopy := call
				emit(Event{ToolStart: &callCopy})
			}

			var result string
			var err error
			var durationMs *int64
			approved := true
			if a.needsApproval(mode, call) {
				if approve == nil {
					approved = false
					err = fmt.Errorf("tool call requires user approval in NORMAL mode")
				} else {
					approved, err = approve(ctx, call)
					if err != nil {
						return produced, usage, err
					}
					if !approved {
						err = fmt.Errorf("tool call denied by the user")
					}
				}
			}
			if approved {
				started := time.Now()
				result, err = a.runTool(ctx, call, availableTools)
				elapsed := time.Since(started).Milliseconds()
				durationMs = &elapsed
				if err != nil {
					slog.Error("tool failed", "tool", call.Name, "error", err)
				}
			} else if err != nil {
				slog.Warn("tool call denied", "tool", call.Name, "reason", err)
			}
			isError := err != nil
			if err != nil {
				if result == "" {
					result = "error: " + err.Error()
				} else {
					result += "\n\n" + err.Error()
				}
			}
			resultMessage := session.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: call.ID,
				IsError:    isError,
				DurationMs: durationMs,
			}
			if emit != nil {
				resultCopy := resultMessage
				emit(Event{ToolResult: &resultCopy})
			}
			produced = append(produced, resultMessage)
			local = append(local, resultMessage)
		}
	}
}

// requestCost returns the spend of one request, like Pi Agent: the catalog
// estimate (with cache pricing and request-wide tiers), replaced by
// OpenRouter's billed cost when the response reports one.
func (a *Agent) requestCost(usage llm.Usage) float64 {
	cost := catalogCost(a.model.Cost, usage)
	if a.isOpenRouter() {
		if reported, ok := usage.ReportedCost(); ok {
			cost = reported
		}
	}
	return cost
}

// isOpenRouter reports whether requests go to OpenRouter.
func (a *Agent) isOpenRouter() bool {
	return a.model.Provider == "openrouter" || strings.Contains(a.model.BaseURL, "openrouter.ai")
}

// catalogCost prices a request with the model's per-million-token rates. The
// highest matching tier applies to the whole request, as in Pi Agent.
func catalogCost(cost *config.Cost, usage llm.Usage) float64 {
	if cost == nil {
		return 0
	}
	input := usage.InputTokens()
	cacheRead := usage.CacheReadTokens()
	cacheWrite := usage.CacheWriteTokens()
	output := usage.OutputTokens()

	inputRate, outputRate := cost.Input, cost.Output
	cacheReadRate, cacheWriteRate := cost.CacheRead, cost.CacheWrite
	totalInput := input + cacheRead + cacheWrite
	threshold := -1
	for _, tier := range cost.Tiers {
		if totalInput > tier.InputTokensAbove && tier.InputTokensAbove > threshold {
			threshold = tier.InputTokensAbove
			inputRate, outputRate = tier.Input, tier.Output
			cacheReadRate, cacheWriteRate = tier.CacheRead, tier.CacheWrite
		}
	}

	const perMillion = 1_000_000
	return (float64(input)*inputRate +
		float64(output)*outputRate +
		float64(cacheRead)*cacheReadRate +
		float64(cacheWrite)*cacheWriteRate) / perMillion
}

// shouldCompactContext reports whether the current projected context crossed
// the compaction threshold.
func (a *Agent) shouldCompactContext(contextTokens, contextWindow int) bool {
	if !a.compactionEnabled || contextWindow <= 0 {
		return false
	}
	threshold := contextWindow - a.compactionReserveTokens
	if threshold < 1 {
		threshold = 1
	}
	return contextTokens > threshold
}

// reasoningReplay describes how assistant reasoning is sent back to the
// provider.
type reasoningReplay struct {
	// reasoningField is the OpenAI-compatible body field that carries plain
	// reasoning text, e.g. "reasoning" for OpenRouter endpoints.
	reasoningField string
	// requiresReasoningContent adds an empty or echoed reasoning_content field.
	requiresReasoningContent bool
}

// usable reports whether an assistant message has reasoning that will be
// replayed to the provider.
func (r reasoningReplay) usable(message session.Message) bool {
	if len(message.ReasoningDetails) > 0 {
		return true
	}
	if strings.TrimSpace(message.Thinking) == "" {
		return false
	}
	return r.reasoningField != "" || r.requiresReasoningContent
}

// reasoningReplay resolves how this model's reasoning is replayed.
func (a *Agent) reasoningReplay() reasoningReplay {
	if !a.model.Reasoning {
		return reasoningReplay{}
	}
	replay := reasoningReplay{
		requiresReasoningContent: a.compatBool("requiresReasoningContentOnAssistantMessages"),
	}
	switch a.compatString("thinkingFormat") {
	case "", "openrouter", "string-thinking":
		replay.reasoningField = "reasoning"
	}
	return replay
}

// normalizeMode returns the canonical mode name, defaulting to FOLIA.
func normalizeMode(mode string) string {
	switch mode {
	case session.ModeNormal:
		return session.ModeNormal
	case session.ModeYolo:
		return session.ModeYolo
	default:
		return session.ModeFolia
	}
}

// modeInstruction tells the model which mode it runs in and what that means
// for the available tools.
func modeInstruction(mode string) string {
	switch normalizeMode(mode) {
	case session.ModeYolo:
		return "<current_mode>\n" + session.ModeYolo + "\n</current_mode>\n" +
			"All tools are available and non-read-only tools run without asking the user."
	case session.ModeNormal:
		return "<current_mode>\n" + session.ModeNormal + "\n</current_mode>\n" +
			"All tools are available. Non-read-only tools require the user's approval before they run; the harness asks the user."
	default:
		return "<current_mode>\n" + session.ModeFolia + "\n</current_mode>\n" +
			"Foliarz is in FOLIA mode: only read-only tools are available. " +
			"Non-read-only tools are blocked and must not be called."
	}
}

// containsTool reports whether tools contains a tool with the given name.
func containsTool(toolset []tools.Tool, name string) bool {
	for _, tool := range toolset {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// toolsForMode returns the tools available in mode. FOLIA mode only exposes
// read-only tools.
func (a *Agent) toolsForMode(mode string) []tools.Tool {
	if normalizeMode(mode) != session.ModeFolia {
		return a.tools
	}
	available := make([]tools.Tool, 0, len(a.tools))
	for _, tool := range a.tools {
		if tool.ReadOnly {
			available = append(available, tool)
		}
	}
	return available
}

// schemaSupportsStrict reports whether a tool schema can be sent with strict
// mode without changing its meaning: every property must be required and
// additional properties must be disabled.
func schemaSupportsStrict(parameters json.RawMessage) bool {
	var schema struct {
		Type                 string                     `json:"type"`
		AdditionalProperties *bool                      `json:"additionalProperties"`
		Properties           map[string]json.RawMessage `json:"properties"`
		Required             []string                   `json:"required"`
	}
	if err := json.Unmarshal(parameters, &schema); err != nil {
		return false
	}
	if schema.Type != "object" || schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		return false
	}
	for property := range schema.Properties {
		found := false
		for _, required := range schema.Required {
			if required == property {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// needsApproval reports whether a call must be confirmed by the user.
func (a *Agent) needsApproval(mode string, call session.ToolCall) bool {
	if normalizeMode(mode) != session.ModeNormal {
		return false
	}
	for _, tool := range a.tools {
		if tool.Name == call.Name {
			return !tool.ReadOnly
		}
	}
	return false
}

func (a *Agent) runTool(ctx context.Context, call session.ToolCall, availableTools []tools.Tool) (string, error) {
	var tool tools.Tool
	for _, candidate := range a.tools {
		if candidate.Name == call.Name {
			tool = candidate
			break
		}
	}
	if tool.Name == "" {
		return "", fmt.Errorf("unknown tool %q", call.Name)
	}
	if !containsTool(availableTools, tool.Name) {
		return "", fmt.Errorf("tool %q is blocked in FOLIA mode: Foliarz is read-only and cannot write files or execute commands; switch to NORMAL mode with /normal to use it", call.Name)
	}

	args := json.RawMessage(call.Arguments)
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	if !json.Valid(args) {
		return "", fmt.Errorf("invalid arguments for tool %q", call.Name)
	}
	return runToolSafely(ctx, tool, args)
}

// runToolSafely runs a tool and converts a panic into a tool error so a tool bug
// can never terminate the TUI.
func runToolSafely(ctx context.Context, tool tools.Tool, args json.RawMessage) (result string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("tool panicked", "tool", tool.Name, "panic", recovered)
			result = ""
			err = fmt.Errorf("tool %q panicked: %v", tool.Name, recovered)
		}
	}()
	return tool.Run(ctx, args)
}

func (a *Agent) toolDefinitions(availableTools []tools.Tool) []llm.Tool {
	if len(availableTools) == 0 {
		return nil
	}
	supportsStrict := a.compatBool("supportsStrictMode")
	definitions := make([]llm.Tool, 0, len(availableTools))
	for _, tool := range availableTools {
		definitions = append(definitions, llm.Tool{
			Type: "function",
			Function: llm.Function{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
				Strict:      supportsStrict && schemaSupportsStrict(tool.Parameters),
			},
		})
	}
	return definitions
}

// reasoning returns the provider reasoning effort for a thinking level. Levels
// without a non-null mapping are sent without a reasoning field.
func (a *Agent) reasoning(level string) *llm.Reasoning {
	effort := a.thinkingEffort(level)
	if effort == "" {
		return nil
	}
	return &llm.Reasoning{Effort: effort}
}

func (a *Agent) thinkingEffort(level string) string {
	if level == "" || a.model.ThinkingLevelMap == nil {
		return ""
	}
	value, ok := a.model.ThinkingLevelMap[level]
	if !ok || value == nil {
		return ""
	}
	return *value
}

// reasoningFor shapes the reasoning request for the model's thinking format.
func (a *Agent) reasoningFor(level string, extra map[string]any) *llm.Reasoning {
	effort := a.thinkingEffort(level)
	switch a.compatString("thinkingFormat") {
	case "", "openrouter":
		if effort == "" {
			return nil
		}
		return &llm.Reasoning{Effort: effort}
	case "openai":
		if effort != "" {
			extra["reasoning_effort"] = effort
		}
	case "deepseek":
		if effort == "" || effort == "none" {
			extra["thinking"] = map[string]any{"type": "disabled"}
		} else {
			extra["thinking"] = map[string]any{"type": "enabled"}
			extra["reasoning_effort"] = effort
		}
	case "zai":
		if effort == "" || effort == "none" {
			extra["thinking"] = map[string]any{"type": "disabled"}
		} else {
			extra["thinking"] = map[string]any{"type": "enabled"}
		}
	case "qwen":
		extra["enable_thinking"] = effort != "" && effort != "none"
	case "string-thinking":
		extra["thinking"] = effort
	default:
		if effort == "" {
			return nil
		}
		return &llm.Reasoning{Effort: effort}
	}
	return nil
}

func (a *Agent) compatBool(key string) bool {
	value, _ := a.model.Compat[key].(bool)
	return value
}

func (a *Agent) compatString(key string) string {
	value, _ := a.model.Compat[key].(string)
	return value
}

func cloneParameters(params map[string]any) map[string]any {
	if len(params) == 0 {
		return nil
	}
	clone := make(map[string]any, len(params))
	for key, value := range params {
		clone[key] = value
	}
	return clone
}

// sessionAffinityHeaders returns session-affinity headers when the model
// compatibility settings ask for them.
func (a *Agent) sessionAffinityHeaders(ctx context.Context) map[string]string {
	if !a.compatBool("sendSessionAffinityHeaders") {
		return nil
	}
	id := sessionIDFrom(ctx)
	if id == "" {
		return nil
	}

	format := a.compatString("sessionAffinityFormat")
	if format == "" {
		if _, ok := a.model.Compat["openRouterRouting"]; ok {
			format = "openrouter"
		} else {
			format = "openai"
		}
	}

	headers := make(map[string]string)
	switch format {
	case "openrouter":
		headers["x-session-id"] = id
	case "openai-nosession":
		headers["x-client-request-id"] = id
		headers["x-session-affinity"] = id
	default:
		headers["session_id"] = id
		headers["x-client-request-id"] = id
		headers["x-session-affinity"] = id
	}
	return headers
}

// providerRouting returns the provider routing preferences of the model.
func (a *Agent) providerRouting() map[string]any {
	routing, _ := a.model.Compat["openRouterRouting"].(map[string]any)
	return routing
}

func toLLMMessage(message session.Message, replay reasoningReplay) llm.Message {
	converted := llm.Message{
		Role:       message.Role,
		Content:    message.Content,
		ToolCallID: message.ToolCallID,
	}
	if message.Role == "assistant" {
		thinking := strings.TrimSpace(message.Thinking)
		switch {
		case len(message.ReasoningDetails) > 0 && (replay.reasoningField != "" || replay.requiresReasoningContent):
			// Structured reasoning blocks are signed replay metadata; send them
			// back exactly as the provider returned them.
			converted.ReasoningDetails = message.ReasoningDetails
		case thinking != "" && replay.reasoningField != "":
			converted.Reasoning = thinking
		case thinking != "" && replay.requiresReasoningContent:
			converted.ReasoningContent = &thinking
		}
		// Pi also sends an empty reasoning_content alongside structured details
		// when the provider requires the field on replayed assistant messages.
		if replay.requiresReasoningContent && converted.ReasoningContent == nil {
			empty := ""
			converted.ReasoningContent = &empty
		}
	}
	for _, call := range message.ToolCalls {
		converted.ToolCalls = append(converted.ToolCalls, llm.ToolCall{
			ID:   call.ID,
			Type: "function",
			Function: llm.FunctionCall{
				Name:      call.Name,
				Arguments: call.Arguments,
			},
		})
	}
	return converted
}

func fromLLMToolCalls(calls []llm.ToolCall) []session.ToolCall {
	if len(calls) == 0 {
		return nil
	}

	// Some OpenAI-compatible servers omit or repeat tool-call IDs. Every stored
	// call needs a unique ID so its result can be matched.
	used := make(map[string]bool, len(calls))
	next := 1
	uniqueID := func(id string) string {
		if id != "" && !used[id] {
			used[id] = true
			return id
		}
		for {
			candidate := fmt.Sprintf("call_%d", next)
			next++
			if !used[candidate] {
				used[candidate] = true
				return candidate
			}
		}
	}

	converted := make([]session.ToolCall, 0, len(calls))
	for _, call := range calls {
		converted = append(converted, session.ToolCall{
			ID:        uniqueID(call.ID),
			Name:      call.Function.Name,
			Arguments: call.Function.Arguments,
		})
	}
	return converted
}
