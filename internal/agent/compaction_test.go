package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdukat/foliarz/internal/llm"
	"github.com/mdukat/foliarz/internal/session"
	"github.com/mdukat/foliarz/internal/tools"
)

func TestCompactLargeContext(t *testing.T) {
	summaryText := "## Goal\nVerify compaction."
	var sawRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(request.Tools) != 0 {
			t.Errorf("summarization request has tools: %d", len(request.Tools))
		}
		if len(request.Messages) != 2 || request.Messages[0].Role != "system" {
			t.Errorf("summarization messages = %+v", request.Messages)
		}
		content, _ := json.Marshal(summaryText)
		sseResponse(w, fmt.Sprintf(`{"choices":[{"delta":{"content":%s}}]}`, content))
	}))
	defer server.Close()

	var history []session.Message
	for i := 0; i < 1000; i++ {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		message := session.Message{Role: role, Content: strings.Repeat("x", 200)}
		if i == 10 {
			message.ToolCalls = []session.ToolCall{{ID: "read-1", Name: "read", Arguments: `{"path":"a.txt"}`}}
			message.Content = strings.Repeat("x", 120)
		}
		if i == 20 {
			message.ToolCalls = []session.ToolCall{{ID: "write-1", Name: "write", Arguments: `{"path":"b.txt","content":"x"}`}}
			message.Content = strings.Repeat("x", 120)
		}
		history = append(history, message)
	}

	agent := New(llm.New(server.URL, "", nil), testModel(), "system", tools.All(), nil)
	result, err := agent.Compact(context.Background(), CompactionInput{
		History:          history,
		KeepRecentTokens: 20000,
		ThinkingLevel:    "off",
	})
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if !sawRequest {
		t.Fatal("no summarization request was made")
	}
	if result.FirstKeptMessage <= 0 || result.FirstKeptMessage >= len(history) {
		t.Errorf("FirstKeptMessage = %d", result.FirstKeptMessage)
	}
	if result.TokensBefore < 40000 {
		t.Errorf("TokensBefore = %d, want about 50000", result.TokensBefore)
	}
	if result.EstimatedTokensAfter >= result.TokensBefore {
		t.Errorf("tokens after %d >= before %d", result.EstimatedTokensAfter, result.TokensBefore)
	}
	if !strings.Contains(result.Summary, summaryText) {
		t.Errorf("summary = %q", result.Summary)
	}
	if !strings.Contains(result.Summary, "<read-files>\na.txt\n</read-files>") {
		t.Errorf("summary lacks read files: %q", result.Summary)
	}
	if !strings.Contains(result.Summary, "<modified-files>\nb.txt\n</modified-files>") {
		t.Errorf("summary lacks modified files: %q", result.Summary)
	}
	if len(result.ReadFiles) != 1 || result.ReadFiles[0] != "a.txt" {
		t.Errorf("ReadFiles = %v", result.ReadFiles)
	}
	if len(result.ModifiedFiles) != 1 || result.ModifiedFiles[0] != "b.txt" {
		t.Errorf("ModifiedFiles = %v", result.ModifiedFiles)
	}
}

func TestRespondUsesCompactionSummary(t *testing.T) {
	var request llm.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"ok"}}]}`)
	}))
	defer server.Close()

	history := []session.Message{
		{Role: "user", Content: "old question"},
		{Role: "assistant", Content: "old answer"},
		{Role: "user", Content: "new question"},
	}
	agent := New(llm.New(server.URL, "", nil), testModel(), "system", tools.All(), nil)
	if _, _, err := agent.Respond(context.Background(), history, "off", session.ModeFolia, "", CompactionContext{Summary: "EARLIER SUMMARY", FirstKeptMessage: 2}, nil, nil, nil); err != nil {
		t.Fatalf("Respond() error = %v", err)
	}

	var contents []string
	for _, message := range request.Messages {
		contents = append(contents, message.Content)
	}
	joined := strings.Join(contents, "\n---\n")
	if !strings.Contains(joined, "EARLIER SUMMARY") {
		t.Error("summary is missing from the request")
	}
	if strings.Contains(joined, "old question") || strings.Contains(joined, "old answer") {
		t.Error("summarized messages were sent to the model")
	}
	if !strings.Contains(joined, "new question") {
		t.Error("kept message is missing from the request")
	}
}

func TestEstimateIncludesReasoningDetails(t *testing.T) {
	plain := session.Message{Role: "assistant", Content: "answer"}
	withDetails := plain
	withDetails.ReasoningDetails = []json.RawMessage{json.RawMessage(`"` + strings.Repeat("x", 4000) + `"`)}
	if estimateMessageTokens(withDetails) <= estimateMessageTokens(plain) {
		t.Error("reasoning details are not counted in token estimates")
	}
}

func TestCompactionUsesProviderRouting(t *testing.T) {
	var request llm.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		sseResponse(w, `{"choices":[{"delta":{"content":"summary"}}]}`)
	}))
	defer server.Close()

	agent := New(llm.New(server.URL, "", nil), testModel(), "system", tools.All(), nil)
	history := make([]session.Message, 40)
	for i := range history {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		history[i] = session.Message{Role: role, Content: strings.Repeat("x", 200)}
	}
	if _, err := agent.Compact(context.Background(), CompactionInput{History: history, KeepRecentTokens: 500}); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	only, _ := request.Provider["only"].([]any)
	if len(only) != 1 || only[0] != "alibaba" {
		t.Errorf("summarization provider routing = %+v", request.Provider)
	}
	if request.Provider["allow_fallbacks"] != false {
		t.Errorf("allow_fallbacks = %v", request.Provider["allow_fallbacks"])
	}
}
