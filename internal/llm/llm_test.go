package llm

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatStream(t *testing.T) {
	var got Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`{"choices":[{"delta":{"reasoning":"I should "}}]}`,
			`{"choices":[{"delta":{"reasoning":"check."}}]}`,
			`{"choices":[{"delta":{"content":"Hello"}}]}`,
			`{"choices":[{"delta":{"content":" world","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"datetime","arguments":"{}"}}]}}]}`,
			`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
		}
		for _, chunk := range chunks {
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	var deltas []Delta
	response, err := New(server.URL, "key", nil).ChatStream(context.Background(), Request{
		Model:    "test",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, func(delta Delta) {
		deltas = append(deltas, delta)
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}

	if !got.Stream || got.StreamOptions == nil || !got.StreamOptions.IncludeUsage {
		t.Errorf("stream options = %+v", got.StreamOptions)
	}
	if len(deltas) != 4 {
		t.Errorf("deltas = %+v, want 4", deltas)
	}
	if response.Choices[0].Message.Content != "Hello world" {
		t.Errorf("content = %q", response.Choices[0].Message.Content)
	}
	if response.Choices[0].Message.Reasoning != "I should check." {
		t.Errorf("reasoning = %q", response.Choices[0].Message.Reasoning)
	}
	calls := response.Choices[0].Message.ToolCalls
	if len(calls) != 1 || calls[0].ID != "call-1" || calls[0].Function.Name != "datetime" {
		t.Errorf("tool calls = %+v", calls)
	}
	if response.Usage.TotalTokens != 8 {
		t.Errorf("usage = %+v", response.Usage)
	}
}

func TestChatStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer server.Close()

	_, err := New(server.URL, "", nil).ChatStream(context.Background(), Request{}, nil)
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error = %v", err)
	}
}

func TestChatStreamIncomplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"partial"}}]}` + "\n\n"))
	}))
	defer server.Close()

	if _, err := New(server.URL, "", nil).ChatStream(context.Background(), Request{}, nil); err == nil {
		t.Fatal("ChatStream() error = nil, want incomplete stream error")
	}
}

func TestRequestMarshalsExtraAndMaxTokens(t *testing.T) {
	data, err := json.Marshal(Request{
		Model:     "model",
		Messages:  []Message{{Role: "user", Content: "hi"}},
		MaxTokens: 123,
		Extra:     map[string]any{"reasoning_effort": "high", "enable_thinking": true},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["max_tokens"] != float64(123) {
		t.Errorf("max_tokens = %v", fields["max_tokens"])
	}
	if fields["reasoning_effort"] != "high" || fields["enable_thinking"] != true {
		t.Errorf("extra fields missing: %v", fields)
	}
}

func TestClientAndRequestHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Provider"); got != "provider-value" {
			t.Errorf("X-Provider = %q", got)
		}
		if got := r.Header.Get("X-Session"); got != "session-value" {
			t.Errorf("X-Session = %q", got)
		}
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	client := New(server.URL, "", map[string]string{"X-Provider": "provider-value"})
	_, err := client.ChatStream(context.Background(), Request{
		Headers: map[string]string{"X-Session": "session-value"},
	}, nil)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
}

func TestMessageReasoningContentMarshalsEmpty(t *testing.T) {
	empty := ""
	data, err := json.Marshal(Message{Role: "assistant", Content: "answer", ReasoningContent: &empty})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	value, ok := fields["reasoning_content"]
	if !ok || value != "" {
		t.Errorf("reasoning_content = %v, present %v", value, ok)
	}
}

func writeSSE(w http.ResponseWriter, chunks ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, chunk := range chunks {
		_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
}

func TestChatStreamAssemblesReasoningDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w,
			`{"choices":[{"delta":{"reasoning":"We","reasoning_details":[{"type":"reasoning.text","text":"We","format":"unknown","index":0}]}}]}`,
			`{"choices":[{"delta":{"reasoning":" need time","reasoning_details":[{"type":"reasoning.text","text":" need time","format":"unknown","index":0}]}}]}`,
			`{"choices":[{"delta":{"content":"hi"}}]}`,
		)
	}))
	defer server.Close()

	response, err := New(server.URL, "", nil).ChatStream(context.Background(), Request{}, nil)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	details := response.Choices[0].Message.ReasoningDetails
	if len(details) != 1 {
		t.Fatalf("reasoning details = %s", details)
	}
	var block map[string]any
	if err := json.Unmarshal(details[0], &block); err != nil {
		t.Fatal(err)
	}
	if block["text"] != "We need time" {
		t.Errorf("text = %v", block["text"])
	}
	if block["type"] != "reasoning.text" || block["format"] != "unknown" {
		t.Errorf("block = %v", block)
	}
	if response.Choices[0].Message.Reasoning != "We need time" {
		t.Errorf("reasoning = %q", response.Choices[0].Message.Reasoning)
	}
}

func TestChatStreamAcceptsLargeChunks(t *testing.T) {
	large := strings.Repeat("a", 2<<20)
	payload := `{"choices":[{"delta":{"content":"` + large + `"}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, payload)
	}))
	defer server.Close()

	response, err := New(server.URL, "", nil).ChatStream(context.Background(), Request{}, nil)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if len(response.Choices[0].Message.Content) != len(large) {
		t.Errorf("content length = %d, want %d", len(response.Choices[0].Message.Content), len(large))
	}
}

func TestChatStreamAcceptsEOFAfterFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\n"))
	}))
	defer server.Close()

	response, err := New(server.URL, "", nil).ChatStream(context.Background(), Request{}, nil)
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	if response.Choices[0].Message.Content != "hello" {
		t.Errorf("content = %q", response.Choices[0].Message.Content)
	}
	if response.Choices[0].FinishReason != "stop" {
		t.Errorf("finish reason = %q", response.Choices[0].FinishReason)
	}
}

func TestUsageParsingAndCosts(t *testing.T) {
	var usage Usage
	payload := `{
		"prompt_tokens": 1000,
		"completion_tokens": 200,
		"total_tokens": 1200,
		"prompt_tokens_details": {"cached_tokens": 300, "cache_write_tokens": 100},
		"cost": 0.05,
		"is_byok": false
	}`
	if err := json.Unmarshal([]byte(payload), &usage); err != nil {
		t.Fatal(err)
	}
	if got := usage.InputTokens(); got != 600 {
		t.Errorf("InputTokens() = %d, want 600", got)
	}
	if got := usage.CacheReadTokens(); got != 300 {
		t.Errorf("CacheReadTokens() = %d, want 300", got)
	}
	if got := usage.CacheWriteTokens(); got != 100 {
		t.Errorf("CacheWriteTokens() = %d, want 100", got)
	}
	if got := usage.OutputTokens(); got != 200 {
		t.Errorf("OutputTokens() = %d, want 200", got)
	}
	if cost, ok := usage.ReportedCost(); !ok || cost != 0.05 {
		t.Errorf("ReportedCost() = %v, %v", cost, ok)
	}
}

func TestUsageReportedCostBYOK(t *testing.T) {
	usage := Usage{
		Cost:   float64Ptr(0.01),
		IsBYOK: true,
		CostDetails: &CostDetails{
			UpstreamInferencePromptCost:  float64Ptr(0.002),
			UpstreamInferenceCompletions: float64Ptr(0.003),
		},
	}
	cost, ok := usage.ReportedCost()
	if !ok {
		t.Fatal("ReportedCost() not reported")
	}
	if cost != 0.015 {
		t.Errorf("ReportedCost() = %v, want 0.015", cost)
	}

	usage.CostDetails = nil
	if _, ok := usage.ReportedCost(); ok {
		t.Error("BYOK usage without cost details reported a cost")
	}
}

func float64Ptr(value float64) *float64 { return &value }

func TestReportedCostRejectsInvalidValues(t *testing.T) {
	nan := math.NaN()
	negative := -1.0
	for _, value := range []*float64{nil, &nan, &negative} {
		if _, ok := (Usage{Cost: value}).ReportedCost(); ok {
			t.Errorf("ReportedCost() accepted %v", value)
		}
	}
}
