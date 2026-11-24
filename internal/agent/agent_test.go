package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdukat/foliarz/internal/config"
	"github.com/mdukat/foliarz/internal/llm"
	"github.com/mdukat/foliarz/internal/session"
	"github.com/mdukat/foliarz/internal/skills"
	"github.com/mdukat/foliarz/internal/tools"
)

func stringPtr(value string) *string { return &value }

func testModel() *config.Model {
	return &config.Model{
		ID: "deepseek/deepseek-v4.1-flash",
		ThinkingLevelMap: map[string]*string{
			"off":  stringPtr("none"),
			"low":  stringPtr("low"),
			"high": stringPtr("high"),
			"max":  stringPtr("max"),
		},
		Compat: map[string]any{
			"openRouterRouting": map[string]any{
				"only":            []string{"alibaba"},
				"allow_fallbacks": false,
			},
		},
	}
}

func sseResponse(w http.ResponseWriter, chunks ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	for _, chunk := range chunks {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
		if flusher != nil {
			flusher.Flush()
		}
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	if flusher != nil {
		flusher.Flush()
	}
}

func TestRespondRunsToolCalls(t *testing.T) {
	var requests []llm.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, request)

		if len(requests) == 1 {
			sseResponse(w,
				`{"choices":[{"delta":{"reasoning":"I should check the time."}}]}`,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"datetime","arguments":"{}"}}]}}]}`,
				`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
			)
			return
		}
		sseResponse(w,
			`{"choices":[{"delta":{"content":"It is noon."}}]}`,
			`{"choices":[],"usage":{"prompt_tokens":20,"completion_tokens":4,"total_tokens":24}}`,
		)
	}))
	defer server.Close()

	agent := New(llm.New(server.URL, "key", nil), testModel(), "system prompt", tools.All(), nil)
	history := []session.Message{{Role: "user", Content: "what time is it?"}}

	var events []Event
	messages, usage, err := agent.Respond(context.Background(), history, "off", session.ModeNormal, "", CompactionContext{}, nil, func(event Event) {
		events = append(events, event)
	}, nil)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}

	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	first := requests[0]
	if first.Model != "deepseek/deepseek-v4.1-flash" {
		t.Errorf("model = %q", first.Model)
	}
	if !first.Stream {
		t.Error("stream = false, want true")
	}
	if first.Messages[0].Role != "system" || !strings.HasPrefix(first.Messages[0].Content, "system prompt") {
		t.Errorf("first message = %+v, want system prompt", first.Messages[0])
	}
	if !strings.Contains(first.Messages[0].Content, "<current_mode>\nNORMAL\n</current_mode>") {
		t.Errorf("system prompt does not contain the current mode:\n%s", first.Messages[0].Content)
	}
	foundDatetime := false
	for _, tool := range first.Tools {
		if tool.Function.Name == "datetime" {
			foundDatetime = true
		}
	}
	if !foundDatetime {
		t.Errorf("datetime tool missing from %+v", first.Tools)
	}
	if first.Reasoning == nil || first.Reasoning.Effort != "none" {
		t.Errorf("reasoning = %+v, want effort none", first.Reasoning)
	}
	if only, _ := first.Provider["only"].([]any); len(only) != 1 || only[0] != "alibaba" {
		t.Errorf("provider routing = %+v", first.Provider)
	}

	second := requests[1]
	var toolResult *llm.Message
	for i := range second.Messages {
		if second.Messages[i].Role == "tool" {
			toolResult = &second.Messages[i]
		}
	}
	if toolResult == nil {
		t.Fatal("second request has no tool result")
	}
	if toolResult.ToolCallID != "call-1" {
		t.Errorf("tool_call_id = %q", toolResult.ToolCallID)
	}
	if !strings.Contains(toolResult.Content, "T") {
		t.Errorf("tool result = %q, want a timestamp", toolResult.Content)
	}

	if len(messages) != 3 {
		t.Fatalf("messages = %+v, want assistant, tool, assistant", messages)
	}
	if messages[0].Role != "assistant" || len(messages[0].ToolCalls) != 1 {
		t.Errorf("first produced message = %+v", messages[0])
	}
	if messages[0].Thinking != "I should check the time." {
		t.Errorf("thinking = %q", messages[0].Thinking)
	}
	if messages[1].Role != "tool" || messages[1].ToolCallID != "call-1" {
		t.Errorf("tool message = %+v", messages[1])
	}
	if messages[1].DurationMs == nil {
		t.Error("tool result has no duration")
	}
	if messages[2].Content != "It is noon." {
		t.Errorf("final message = %+v", messages[2])
	}
	if usage.Tokens != 24 {
		t.Errorf("tokens = %d, want 24", usage.Tokens)
	}

	var thinking, content strings.Builder
	resets, toolStarts, toolResults := 0, 0, 0
	startBeforeResult := false
	var toolResultEvent *session.Message
	for _, event := range events {
		if event.Reset {
			resets++
		}
		if event.ToolStart != nil {
			toolStarts++
			if event.ToolStart.ID != "call-1" || event.ToolStart.Name != "datetime" {
				t.Errorf("tool start = %+v", event.ToolStart)
			}
		}
		if event.ToolResult != nil {
			toolResults++
			if toolStarts > 0 {
				startBeforeResult = true
			}
			eventResult := *event.ToolResult
			toolResultEvent = &eventResult
		}
		thinking.WriteString(event.Thinking)
		content.WriteString(event.Content)
	}
	if resets == 0 {
		t.Error("no reset events")
	}
	if toolStarts != 1 || toolResults != 1 {
		t.Errorf("tool events = %d starts, %d results", toolStarts, toolResults)
	}
	if !startBeforeResult {
		t.Error("tool result was emitted before the tool start")
	}
	if toolResultEvent == nil || toolResultEvent.ToolCallID != "call-1" || toolResultEvent.IsError {
		t.Errorf("tool result event = %+v", toolResultEvent)
	}
	if thinking.String() != "I should check the time." {
		t.Errorf("streamed thinking = %q", thinking.String())
	}
	if content.String() != "It is noon." {
		t.Errorf("streamed content = %q", content.String())
	}
}

func TestRespondUnknownTool(t *testing.T) {
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		if step == 1 {
			sseResponse(w,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"missing","arguments":"{}"}}]}}]}`,
				`{"choices":[],"usage":{"total_tokens":5}}`,
			)
			return
		}
		sseResponse(w,
			`{"choices":[{"delta":{"content":"done"}}]}`,
			`{"choices":[],"usage":{"total_tokens":7}}`,
		)
	}))
	defer server.Close()

	agent := New(llm.New(server.URL, "", nil), testModel(), "", tools.All(), nil)
	messages, _, err := agent.Respond(context.Background(), nil, "off", session.ModeNormal, "", CompactionContext{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("messages = %+v", messages)
	}
	if !strings.HasPrefix(messages[1].Content, "error: unknown tool") {
		t.Errorf("tool result = %q", messages[1].Content)
	}
}

func TestRespondAllowsManyToolSteps(t *testing.T) {
	const toolSteps = 12
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		if step <= toolSteps {
			sseResponse(w,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-`+fmt.Sprint(step)+`","type":"function","function":{"name":"datetime","arguments":"{}"}}]}}]}`,
			)
			return
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"done"}}]}`)
	}))
	defer server.Close()

	agent := New(llm.New(server.URL, "", nil), testModel(), "", tools.All(), nil)
	messages, _, err := agent.Respond(context.Background(), nil, "off", session.ModeNormal, "", CompactionContext{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if len(messages) != toolSteps*2+1 {
		t.Errorf("len(messages) = %d, want %d", len(messages), toolSteps*2+1)
	}
	if messages[len(messages)-1].Content != "done" {
		t.Errorf("final message = %+v", messages[len(messages)-1])
	}
}

func TestReasoningOnlyForMappedLevels(t *testing.T) {
	agent := New(nil, testModel(), "", nil, nil)
	if got := agent.reasoning("medium"); got != nil {
		t.Errorf("reasoning(medium) = %+v, want nil", got)
	}
	if got := agent.reasoning("high"); got == nil || got.Effort != "high" {
		t.Errorf("reasoning(high) = %+v, want high", got)
	}
}

func TestRespondPropagatesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": {"message": "bad request"}}`))
	}))
	defer server.Close()

	agent := New(llm.New(server.URL, "", nil), testModel(), "", tools.All(), nil)
	if _, _, err := agent.Respond(context.Background(), nil, "off", session.ModeNormal, "", CompactionContext{}, nil, nil, nil); err == nil {
		t.Fatal("Respond() error = nil, want API error")
	}
}

func TestFoliaModeOnlyExposesReadOnlyTools(t *testing.T) {
	var requests []llm.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, request)
		sseResponse(w, `{"choices":[{"delta":{"content":"ok"}}]}`)
	}))
	defer server.Close()

	readOnly := tools.Tool{
		Name:        "read-only-tool",
		Description: "read only",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		ReadOnly:    true,
		Run:         func(context.Context, json.RawMessage) (string, error) { return "read", nil },
	}
	mutating := tools.Tool{
		Name:        "mutating-tool",
		Description: "mutating",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Run:         func(context.Context, json.RawMessage) (string, error) { return "wrote", nil },
	}

	agent := New(llm.New(server.URL, "", nil), testModel(), "system prompt", []tools.Tool{readOnly, mutating}, nil)

	if _, _, err := agent.Respond(context.Background(), nil, "off", session.ModeFolia, "", CompactionContext{}, nil, nil, nil); err != nil {
		t.Fatalf("FOLIA Respond() error = %v", err)
	}
	if len(requests[0].Tools) != 1 || requests[0].Tools[0].Function.Name != "read-only-tool" {
		t.Errorf("FOLIA tools = %+v", requests[0].Tools)
	}
	if !strings.Contains(requests[0].Messages[0].Content, "Foliarz is in FOLIA mode: only read-only tools are available") {
		t.Errorf("FOLIA system prompt lacks the read-only instruction:\n%s", requests[0].Messages[0].Content)
	}

	if _, _, err := agent.Respond(context.Background(), nil, "off", session.ModeNormal, "", CompactionContext{}, nil, nil, nil); err != nil {
		t.Fatalf("NORMAL Respond() error = %v", err)
	}
	if len(requests[1].Tools) != 2 {
		t.Errorf("NORMAL tools = %+v", requests[1].Tools)
	}
}

func TestFoliaModeRejectsMutatingToolCalls(t *testing.T) {
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		if step == 1 {
			sseResponse(w,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"mutating-tool","arguments":"{}"}}]}}]}`,
			)
			return
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"done"}}]}`)
	}))
	defer server.Close()

	mutating := tools.Tool{
		Name:        "mutating-tool",
		Description: "mutating",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Run:         func(context.Context, json.RawMessage) (string, error) { return "wrote", nil },
	}
	agent := New(llm.New(server.URL, "", nil), testModel(), "", []tools.Tool{mutating}, nil)
	messages, _, err := agent.Respond(context.Background(), nil, "off", session.ModeFolia, "", CompactionContext{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("messages = %+v", messages)
	}
	if !strings.HasPrefix(messages[1].Content, `error: tool "mutating-tool" is blocked in FOLIA mode`) {
		t.Errorf("tool result = %q", messages[1].Content)
	}
}

func TestToolErrorPreservesOutput(t *testing.T) {
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		if step == 1 {
			sseResponse(w,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"failing","arguments":"{}"}}]}}]}`,
			)
			return
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"done"}}]}`)
	}))
	defer server.Close()

	failing := tools.Tool{
		Name:        "failing",
		Description: "fails",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Run: func(context.Context, json.RawMessage) (string, error) {
			return "partial output", errors.New("boom")
		},
	}
	agent := New(llm.New(server.URL, "", nil), testModel(), "", []tools.Tool{failing}, nil)
	allow := func(context.Context, session.ToolCall) (bool, error) { return true, nil }
	messages, _, err := agent.Respond(context.Background(), nil, "off", session.ModeNormal, "", CompactionContext{}, nil, nil, allow)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("messages = %+v", messages)
	}
	if !messages[1].IsError {
		t.Error("tool result is not marked as an error")
	}
	if messages[1].Content != "partial output\n\nboom" {
		t.Errorf("tool result = %q", messages[1].Content)
	}
}

func TestNormalModeAsksBeforeMutatingTool(t *testing.T) {
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		if step == 1 {
			sseResponse(w,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"mutating-tool","arguments":"{\"value\":1}"}}]}}]}`,
			)
			return
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"done"}}]}`)
	}))
	defer server.Close()

	runs := 0
	mutating := tools.Tool{
		Name:        "mutating-tool",
		Description: "mutating",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Run: func(context.Context, json.RawMessage) (string, error) {
			runs++
			return "wrote", nil
		},
	}
	var asked session.ToolCall
	approve := func(_ context.Context, call session.ToolCall) (bool, error) {
		asked = call
		return true, nil
	}

	agent := New(llm.New(server.URL, "", nil), testModel(), "", []tools.Tool{mutating}, nil)
	messages, _, err := agent.Respond(context.Background(), nil, "off", session.ModeNormal, "", CompactionContext{}, nil, nil, approve)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if asked.Name != "mutating-tool" || asked.Arguments != `{"value":1}` {
		t.Errorf("approval call = %+v", asked)
	}
	if runs != 1 {
		t.Errorf("tool runs = %d, want 1", runs)
	}
	if messages[1].IsError || messages[1].Content != "wrote" {
		t.Errorf("tool result = %+v", messages[1])
	}
}

func TestNormalModeDeniesMutatingTool(t *testing.T) {
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		if step == 1 {
			sseResponse(w,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"mutating-tool","arguments":"{}"}}]}}]}`,
			)
			return
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"done"}}]}`)
	}))
	defer server.Close()

	runs := 0
	mutating := tools.Tool{
		Name:        "mutating-tool",
		Description: "mutating",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Run: func(context.Context, json.RawMessage) (string, error) {
			runs++
			return "wrote", nil
		},
	}
	deny := func(context.Context, session.ToolCall) (bool, error) { return false, nil }

	agent := New(llm.New(server.URL, "", nil), testModel(), "", []tools.Tool{mutating}, nil)
	messages, _, err := agent.Respond(context.Background(), nil, "off", session.ModeNormal, "", CompactionContext{}, nil, nil, deny)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if runs != 0 {
		t.Errorf("tool ran despite denial")
	}
	if !messages[1].IsError || !strings.Contains(messages[1].Content, "denied by the user") {
		t.Errorf("tool result = %+v", messages[1])
	}
	if messages[1].DurationMs != nil {
		t.Errorf("denied tool has a duration: %d", *messages[1].DurationMs)
	}
}

func TestYoloModeDoesNotAsk(t *testing.T) {
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		if step == 1 {
			sseResponse(w,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"mutating-tool","arguments":"{}"}}]}}]}`,
			)
			return
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"done"}}]}`)
	}))
	defer server.Close()

	runs := 0
	mutating := tools.Tool{
		Name:        "mutating-tool",
		Description: "mutating",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Run: func(context.Context, json.RawMessage) (string, error) {
			runs++
			return "wrote", nil
		},
	}
	approvals := 0
	approve := func(context.Context, session.ToolCall) (bool, error) {
		approvals++
		return false, nil
	}

	agent := New(llm.New(server.URL, "", nil), testModel(), "", []tools.Tool{mutating}, nil)
	if _, _, err := agent.Respond(context.Background(), nil, "off", session.ModeYolo, "", CompactionContext{}, nil, nil, approve); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if approvals != 0 {
		t.Errorf("YOLO asked for approval %d times", approvals)
	}
	if runs != 1 {
		t.Errorf("tool runs = %d, want 1", runs)
	}
}

func TestProjectContextIsAddedToSystemPrompt(t *testing.T) {
	var request llm.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"ok"}}]}`)
	}))
	defer server.Close()

	agent := New(llm.New(server.URL, "", nil), testModel(), "system prompt", nil, nil)
	if _, _, err := agent.Respond(context.Background(), nil, "off", session.ModeFolia, "PROJECT RULES", CompactionContext{}, nil, nil, nil); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}

	system := request.Messages[0].Content
	for _, want := range []string{"system prompt", "PROJECT RULES", "<current_mode>"} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt does not contain %q:\n%s", want, system)
		}
	}
}

func TestRequestAppliesModelSettingsAndCompat(t *testing.T) {
	model := testModel()
	model.Reasoning = true
	model.MaxTokens = 100
	model.SamplingParams = map[string]any{"temperature": 0.2}
	model.Compat["thinkingFormat"] = "openai"
	model.Compat["supportsStrictMode"] = true
	model.Compat["sendSessionAffinityHeaders"] = true
	model.Compat["sessionAffinityFormat"] = "openrouter"
	model.Compat["requiresReasoningContentOnAssistantMessages"] = true

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-session-id"); got != "sess-1" {
			t.Errorf("x-session-id = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"ok"}}]}`)
	}))
	defer server.Close()

	agent := New(llm.New(server.URL, "", nil), model, "system prompt", tools.All(), nil)
	history := []session.Message{{Role: "assistant", Content: "previous"}}
	ctx := WithSessionID(context.Background(), "sess-1")
	if _, _, err := agent.Respond(ctx, history, "high", session.ModeNormal, "", CompactionContext{}, nil, nil, nil); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}

	if body["max_tokens"] != float64(100) {
		t.Errorf("max_tokens = %v", body["max_tokens"])
	}
	if body["temperature"] != 0.2 {
		t.Errorf("temperature = %v", body["temperature"])
	}
	if body["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %v", body["reasoning_effort"])
	}
	if _, ok := body["reasoning"]; ok {
		t.Errorf("openai thinking format still sent openrouter reasoning: %v", body["reasoning"])
	}

	toolsJSON, _ := body["tools"].([]any)
	strictByTool := map[string]bool{}
	for _, raw := range toolsJSON {
		tool, _ := raw.(map[string]any)
		function, _ := tool["function"].(map[string]any)
		name, _ := function["name"].(string)
		strict, _ := function["strict"].(bool)
		strictByTool[name] = strict
	}
	if !strictByTool["bash"] {
		t.Errorf("bash tool is not strict: %v", strictByTool)
	}
	if strictByTool["read"] {
		t.Errorf("read tool was marked strict despite optional arguments: %v", strictByTool)
	}

	messages, _ := body["messages"].([]any)
	foundReasoningContent := false
	for _, raw := range messages {
		message, _ := raw.(map[string]any)
		if message["role"] != "assistant" {
			continue
		}
		if value, ok := message["reasoning_content"]; ok && value == "" {
			foundReasoningContent = true
		}
	}
	if !foundReasoningContent {
		t.Errorf("assistant message has no empty reasoning_content: %v", messages)
	}
}

func TestSkillsAreAdvertisedWithFoliaSafety(t *testing.T) {
	var request llm.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"ok"}}]}`)
	}))
	defer server.Close()

	skillSet := []skills.Skill{{
		Name:          "web-search",
		Description:   "Search the web.",
		Path:          "/tmp/foliarz-skills/web-search",
		File:          "/tmp/foliarz-skills/web-search/SKILL.md",
		Body:          "Run a script.",
		RequiresShell: true,
	}}
	agent := New(llm.New(server.URL, "", nil), testModel(), "system", nil, skillSet)
	if _, _, err := agent.Respond(context.Background(), nil, "off", session.ModeFolia, "", CompactionContext{}, nil, nil, nil); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	system := request.Messages[0].Content
	for _, want := range []string{"<skills>", "<name>web-search</name>", "<requires_shell>true</requires_shell>", "In FOLIA mode, never execute shell commands"} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt lacks %q:\n%s", want, system)
		}
	}
}

func TestMidTurnCompactionBetweenToolSteps(t *testing.T) {
	const summaryText = "## Goal\nContinue after compaction."
	var requests []llm.Request
	mainCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, request)

		if strings.HasPrefix(request.Messages[0].Content, "You are a context summarization assistant") {
			content, _ := json.Marshal(summaryText)
			sseResponse(w, fmt.Sprintf(`{"choices":[{"delta":{"content":%s}}]}`, content))
			return
		}
		mainCalls++
		if mainCalls == 1 {
			sseResponse(w,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"big","arguments":"{}"}}]}}]}`,
			)
			return
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"done"}}]}`)
	}))
	defer server.Close()

	big := tools.Tool{
		Name:        "big",
		Description: "Returns a large result",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		ReadOnly:    true,
		Run: func(context.Context, json.RawMessage) (string, error) {
			return strings.Repeat("x", 6000), nil
		},
	}

	model := testModel()
	model.ContextWindow = 1_000_000
	agent := New(llm.New(server.URL, "", nil), model, "system", []tools.Tool{big}, nil,
		WithCompaction(true, 200, 500),
	)

	var history []session.Message
	for i := 0; i < 30; i++ {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		history = append(history, session.Message{Role: role, Content: strings.Repeat("y", 200)})
	}

	var events []Event
	messages, _, err := agent.Respond(context.Background(), history, "off", session.ModeFolia, "", CompactionContext{ContextWindow: 2000}, nil, func(event Event) {
		events = append(events, event)
	}, nil)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}

	if len(requests) != 3 {
		t.Fatalf("requests = %d, want main, summary, main", len(requests))
	}
	if len(requests[1].Tools) != 0 {
		t.Errorf("summarization request has tools")
	}
	if len(messages) != 3 {
		t.Fatalf("messages = %+v", messages)
	}

	var compaction *CompactionResult
	for _, event := range events {
		if event.Compaction != nil {
			compaction = event.Compaction
		}
	}
	if compaction == nil {
		t.Fatal("no compaction event was emitted")
	}
	if !strings.Contains(compaction.Summary, "Continue after compaction") {
		t.Errorf("summary = %q", compaction.Summary)
	}
	if compaction.FirstKeptMessage <= 0 {
		t.Errorf("FirstKeptMessage = %d", compaction.FirstKeptMessage)
	}
}

func TestMidTurnCompactionUsesProviderUsage(t *testing.T) {
	mainCalls := 0
	summaryCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(request.Messages[0].Content, "You are a context summarization assistant") {
			summaryCalls++
			sseResponse(w, `{"choices":[{"delta":{"content":"SUMMARY"}}]}`)
			return
		}
		mainCalls++
		if mainCalls == 1 {
			sseResponse(w,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"small","arguments":"{}"}}]}}]}`,
				`{"choices":[],"usage":{"prompt_tokens":4900,"completion_tokens":100,"total_tokens":5000}}`,
			)
			return
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"done"}}]}`)
	}))
	defer server.Close()

	small := tools.Tool{
		Name:        "small",
		Description: "small result",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		ReadOnly:    true,
		Run:         func(context.Context, json.RawMessage) (string, error) { return "ok", nil },
	}
	model := testModel()
	model.ContextWindow = 1_000_000
	agent := New(llm.New(server.URL, "", nil), model, "system", []tools.Tool{small}, nil,
		WithCompaction(true, 0, 10),
	)

	history := []session.Message{
		{Role: "user", Content: strings.Repeat("a", 100)},
		{Role: "assistant", Content: strings.Repeat("b", 100)},
		{Role: "user", Content: strings.Repeat("c", 100)},
	}
	var compactionSeen bool
	_, _, err := agent.Respond(context.Background(), history, "off", session.ModeFolia, "", CompactionContext{ContextWindow: 3000}, nil, func(event Event) {
		if event.Compaction != nil {
			compactionSeen = true
		}
	}, nil)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if summaryCalls != 1 {
		t.Errorf("summary calls = %d, want 1", summaryCalls)
	}
	if !compactionSeen {
		t.Error("usage-based mid-turn compaction did not run")
	}
}

func TestReasoningIsReplayedToProvider(t *testing.T) {
	var request llm.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"ok"}}]}`)
	}))
	defer server.Close()

	model := testModel()
	model.Reasoning = true
	agent := New(llm.New(server.URL, "", nil), model, "system", tools.All(), nil)

	details := json.RawMessage(`{"type":"reasoning.text","text":"check the time","format":"unknown","index":0}`)
	history := []session.Message{
		{Role: "user", Content: "what time is it?"},
		{Role: "assistant", Thinking: "I should check the time.", Content: "checking"},
		{Role: "assistant", Thinking: "structured reasoning", ReasoningDetails: []json.RawMessage{details}},
		{Role: "assistant", Thinking: "thinking only"},
	}
	if _, _, err := agent.Respond(context.Background(), history, "high", session.ModeFolia, "", CompactionContext{}, nil, nil, nil); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}

	var assistants []llm.Message
	for _, message := range request.Messages {
		if message.Role == "assistant" {
			assistants = append(assistants, message)
		}
	}
	if len(assistants) != 3 {
		t.Fatalf("assistant messages = %d, want 3 (thinking-only message must be kept)", len(assistants))
	}
	if assistants[0].Reasoning != "I should check the time." {
		t.Errorf("plain reasoning = %q", assistants[0].Reasoning)
	}
	if len(assistants[1].ReasoningDetails) != 1 || string(assistants[1].ReasoningDetails[0]) != string(details) {
		t.Errorf("reasoning details = %s", assistants[1].ReasoningDetails)
	}
	if assistants[2].Reasoning != "thinking only" || assistants[2].Content != "" {
		t.Errorf("thinking-only message = %+v", assistants[2])
	}
}

func TestReasoningNotReplayedForNonReasoningModel(t *testing.T) {
	var request llm.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"ok"}}]}`)
	}))
	defer server.Close()

	model := testModel()
	model.Reasoning = false
	agent := New(llm.New(server.URL, "", nil), model, "system", tools.All(), nil)

	history := []session.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Thinking: "hidden reasoning", Content: "answer"},
		{Role: "assistant", Thinking: "only reasoning"},
	}
	if _, _, err := agent.Respond(context.Background(), history, "off", session.ModeFolia, "", CompactionContext{}, nil, nil, nil); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}

	var assistants []llm.Message
	for _, message := range request.Messages {
		if message.Role == "assistant" {
			assistants = append(assistants, message)
		}
	}
	if len(assistants) != 1 {
		t.Fatalf("assistant messages = %d, want 1", len(assistants))
	}
	if assistants[0].Reasoning != "" || assistants[0].ReasoningContent != nil {
		t.Errorf("reasoning was replayed for a non-reasoning model: %+v", assistants[0])
	}
}

func TestQueuedMessageSteersBetweenToolSteps(t *testing.T) {
	var requests []llm.Request
	step := 0
	queue := NewMessageQueue()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
		step++
		if step == 1 {
			sseResponse(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"slow","arguments":"{}"}}]}}]}`)
			return
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"steered"}}]}`)
	}))
	defer server.Close()

	slow := tools.Tool{
		Name:        "slow",
		Description: "slow tool",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		ReadOnly:    true,
		Run: func(context.Context, json.RawMessage) (string, error) {
			// The user types while the tool runs.
			queue.Push(session.Message{Role: "user", Content: "actually, change of plan"})
			return "tool output", nil
		},
	}

	agent := New(llm.New(server.URL, "", nil), testModel(), "system", []tools.Tool{slow}, nil)
	messages, _, err := agent.Respond(context.Background(), []session.Message{{Role: "user", Content: "start"}}, "off", session.ModeFolia, "", CompactionContext{}, queue, nil, nil)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}

	second := requests[1].Messages
	var contents []string
	for _, message := range second {
		contents = append(contents, message.Role+": "+message.Content)
	}
	joined := strings.Join(contents, "\n")
	steeringIndex := strings.Index(joined, "user: actually, change of plan")
	toolIndex := strings.Index(joined, "tool output")
	if steeringIndex == -1 || toolIndex == -1 {
		t.Fatalf("second request lacks steering or tool result:\n%s", joined)
	}
	if steeringIndex < toolIndex {
		t.Errorf("steering message was injected before the tool result:\n%s", joined)
	}

	if len(messages) != 4 {
		t.Fatalf("messages = %+v", messages)
	}
	if messages[2].Role != "user" || messages[2].Content != "actually, change of plan" {
		t.Errorf("steering message position = %+v", messages[2])
	}
	if messages[3].Content != "steered" {
		t.Errorf("final message = %+v", messages[3])
	}
}

func TestQueuedMessageAfterFinalAnswerStartsNewTurn(t *testing.T) {
	var requests []llm.Request
	step := 0
	queue := NewMessageQueue()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
		step++
		if step == 1 {
			// The user queues a message just as the first answer streams.
			queue.Push(session.Message{Role: "user", Content: "also check the tests"})
			sseResponse(w, `{"choices":[{"delta":{"content":"first answer"}}]}`)
			return
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"second answer"}}]}`)
	}))
	defer server.Close()

	agent := New(llm.New(server.URL, "", nil), testModel(), "system", tools.All(), nil)
	messages, _, err := agent.Respond(context.Background(), []session.Message{{Role: "user", Content: "first question"}}, "off", session.ModeFolia, "", CompactionContext{}, queue, nil, nil)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want a follow-up request", len(requests))
	}
	last := requests[1].Messages[len(requests[1].Messages)-1]
	if last.Role != "user" || last.Content != "also check the tests" {
		t.Errorf("follow-up request last message = %+v", last)
	}
	if len(messages) != 3 {
		t.Fatalf("messages = %+v", messages)
	}
	if messages[0].Content != "first answer" || messages[1].Content != "also check the tests" || messages[2].Content != "second answer" {
		t.Errorf("message order = %+v", messages)
	}
}

func TestFollowUpTurnEmitsReset(t *testing.T) {
	step := 0
	queue := NewMessageQueue()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		if step == 1 {
			queue.Push(session.Message{Role: "user", Content: "follow up"})
			sseResponse(w, `{"choices":[{"delta":{"content":"first answer"}}]}`)
			return
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"second answer"}}]}`)
	}))
	defer server.Close()

	agent := New(llm.New(server.URL, "", nil), testModel(), "system", tools.All(), nil)
	var events []Event
	if _, _, err := agent.Respond(context.Background(), []session.Message{{Role: "user", Content: "question"}}, "off", session.ModeFolia, "", CompactionContext{}, queue, func(event Event) {
		events = append(events, event)
	}, nil); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}

	resetIndex := -1
	secondContentIndex := -1
	for i, event := range events {
		if event.Reset && resetIndex == -1 {
			resetIndex = i
		}
		if event.Content == "second answer" {
			secondContentIndex = i
		}
	}
	if resetIndex == -1 {
		t.Fatal("follow-up turn did not emit a reset")
	}
	if secondContentIndex <= resetIndex {
		t.Errorf("reset index %d, second answer index %d", resetIndex, secondContentIndex)
	}
}

func TestToolCallsWithoutIDsGetUniqueIDs(t *testing.T) {
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		if step == 1 {
			sseResponse(w,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"type":"function","function":{"name":"datetime","arguments":"{}"}},{"index":1,"type":"function","function":{"name":"datetime","arguments":"{}"}}]}}]}`,
			)
			return
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"done"}}]}`)
	}))
	defer server.Close()

	agent := New(llm.New(server.URL, "", nil), testModel(), "system", tools.All(), nil)
	messages, _, err := agent.Respond(context.Background(), nil, "off", session.ModeFolia, "", CompactionContext{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if len(messages) < 3 {
		t.Fatalf("messages = %+v", messages)
	}
	calls := messages[0].ToolCalls
	if len(calls) != 2 {
		t.Fatalf("tool calls = %+v", calls)
	}
	if calls[0].ID == "" || calls[1].ID == "" {
		t.Fatalf("tool call IDs are empty: %+v", calls)
	}
	if calls[0].ID == calls[1].ID {
		t.Errorf("tool call IDs are not unique: %q", calls[0].ID)
	}
	for _, message := range messages[1:] {
		if message.Role != "tool" {
			continue
		}
		if message.ToolCallID == "" {
			t.Errorf("tool result has no tool call ID: %+v", message)
		}
	}
}

func TestReasoningContentSetAlongsideDetails(t *testing.T) {
	var request llm.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"ok"}}]}`)
	}))
	defer server.Close()

	model := testModel()
	model.Reasoning = true
	model.Compat["requiresReasoningContentOnAssistantMessages"] = true
	agent := New(llm.New(server.URL, "", nil), model, "system", tools.All(), nil)

	details := json.RawMessage(`{"type":"reasoning.text","text":"r","index":0}`)
	history := []session.Message{{
		Role:             "assistant",
		Content:          "answer",
		Thinking:         "r",
		ReasoningDetails: []json.RawMessage{details},
	}}
	if _, _, err := agent.Respond(context.Background(), history, "off", session.ModeFolia, "", CompactionContext{}, nil, nil, nil); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	for _, message := range request.Messages {
		if message.Role != "assistant" {
			continue
		}
		if len(message.ReasoningDetails) != 1 {
			t.Errorf("reasoning details missing: %+v", message)
		}
		if message.ReasoningContent == nil || *message.ReasoningContent != "" {
			t.Errorf("reasoning_content = %v, want empty string", message.ReasoningContent)
		}
	}
}

func TestRequestCostMatchesCatalogPricing(t *testing.T) {
	cost := &config.Cost{Input: 1, Output: 2, CacheRead: 0.5, CacheWrite: 0.75}
	usage := llm.Usage{
		PromptTokens:     1_000_000,
		CompletionTokens: 500_000,
		PromptTokensDetails: &llm.PromptTokensDetails{
			CachedTokens:     200_000,
			CacheWriteTokens: 100_000,
		},
	}
	// 700k input * $1 + 500k output * $2 + 200k cache read * $0.5 + 100k write * $0.75, per million.
	want := 0.7*1 + 0.5*2 + 0.2*0.5 + 0.1*0.75
	if got := catalogCost(cost, usage); got != want {
		t.Errorf("catalogCost() = %v, want %v", got, want)
	}
}

func TestRequestCostUsesHighestMatchingTier(t *testing.T) {
	cost := &config.Cost{
		Input: 1, Output: 2, CacheRead: 0, CacheWrite: 0,
		Tiers: []config.CostTier{
			{InputTokensAbove: 100_000, Input: 0.5, Output: 1},
			{InputTokensAbove: 1_000_000, Input: 0.25, Output: 0.5},
		},
	}
	usage := llm.Usage{PromptTokens: 2_000_000, CompletionTokens: 1_000_000}
	// 2M input * $0.25 + 1M output * $0.5.
	if got := catalogCost(cost, usage); got != 1.0 {
		t.Errorf("catalogCost() = %v, want 1", got)
	}
}

func TestRequestCostPrefersOpenRouterBilledCost(t *testing.T) {
	model := testModel()
	model.Provider = "openrouter"
	model.Cost = &config.Cost{Input: 1000, Output: 1000, CacheRead: 0, CacheWrite: 0}
	agent := New(nil, model, "", nil, nil)

	billed := 0.0123
	cost := agent.requestCost(llm.Usage{PromptTokens: 10, CompletionTokens: 10, Cost: &billed})
	if cost != billed {
		t.Errorf("requestCost() = %v, want billed %v", cost, billed)
	}

	cost = agent.requestCost(llm.Usage{PromptTokens: 10, CompletionTokens: 10})
	if cost != 0.02 {
		t.Errorf("requestCost() = %v, want catalog estimate 0.02", cost)
	}
}

func TestRequestCostIgnoresBilledCostForOtherProviders(t *testing.T) {
	model := testModel()
	model.Provider = "local"
	model.Cost = &config.Cost{Input: 1, Output: 0, CacheRead: 0, CacheWrite: 0}
	agent := New(nil, model, "", nil, nil)

	billed := 5.0
	cost := agent.requestCost(llm.Usage{PromptTokens: 1_000_000, CompletionTokens: 0, Cost: &billed})
	if cost != 1.0 {
		t.Errorf("requestCost() = %v, want catalog estimate 1", cost)
	}
}

func TestPanickingToolBecomesErrorResult(t *testing.T) {
	step := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		if step == 1 {
			sseResponse(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"boom","arguments":"{}"}}]}}]}`)
			return
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"done"}}]}`)
	}))
	defer server.Close()

	boom := tools.Tool{
		Name:        "boom",
		Description: "panics",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		ReadOnly:    true,
		Run: func(context.Context, json.RawMessage) (string, error) {
			panic("kaboom")
		},
	}
	agent := New(llm.New(server.URL, "", nil), testModel(), "system", []tools.Tool{boom}, nil)
	messages, _, err := agent.Respond(context.Background(), nil, "off", session.ModeFolia, "", CompactionContext{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("messages = %+v", messages)
	}
	if !messages[1].IsError || !strings.Contains(messages[1].Content, "kaboom") {
		t.Errorf("tool result = %+v", messages[1])
	}
}

func TestMidTurnCompactionUsesProviderRouting(t *testing.T) {
	var summaryRequest *llm.Request
	mainCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(request.Messages[0].Content, "You are a context summarization assistant") {
			summaryRequest = &request
			sseResponse(w, `{"choices":[{"delta":{"content":"SUMMARY"}}]}`)
			return
		}
		mainCalls++
		if mainCalls == 1 {
			sseResponse(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"big","arguments":"{}"}}]}}]}`)
			return
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"done"}}]}`)
	}))
	defer server.Close()

	big := tools.Tool{
		Name:        "big",
		Description: "big",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		ReadOnly:    true,
		Run:         func(context.Context, json.RawMessage) (string, error) { return strings.Repeat("x", 6000), nil },
	}
	model := testModel()
	model.ContextWindow = 1_000_000
	agent := New(llm.New(server.URL, "", nil), model, "system", []tools.Tool{big}, nil, WithCompaction(true, 200, 500))

	history := make([]session.Message, 30)
	for i := range history {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		history[i] = session.Message{Role: role, Content: strings.Repeat("y", 200)}
	}
	if _, _, err := agent.Respond(context.Background(), history, "off", session.ModeFolia, "", CompactionContext{ContextWindow: 2000}, nil, nil, nil); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}
	if summaryRequest == nil {
		t.Fatal("no summarization request was made")
	}
	only, _ := summaryRequest.Provider["only"].([]any)
	if len(only) != 1 || only[0] != "alibaba" {
		t.Errorf("summarization provider routing = %+v", summaryRequest.Provider)
	}
}

func TestInterruptedThinkingIsReplayed(t *testing.T) {
	var request llm.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"ok"}}]}`)
	}))
	defer server.Close()

	model := testModel()
	model.Reasoning = true
	agent := New(llm.New(server.URL, "", nil), model, "system", tools.All(), nil)

	// A ctrl+c during thinking preserves a thinking-only assistant message.
	history := []session.Message{
		{Role: "user", Content: "question"},
		{Role: "assistant", Thinking: "partial interrupted reasoning"},
	}
	if _, _, err := agent.Respond(context.Background(), history, "high", session.ModeFolia, "", CompactionContext{}, nil, nil, nil); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}

	found := false
	for _, message := range request.Messages {
		if message.Role == "assistant" && message.Reasoning == "partial interrupted reasoning" {
			found = true
		}
	}
	if !found {
		t.Errorf("interrupted thinking was not replayed: %+v", request.Messages)
	}
}
