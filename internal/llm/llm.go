// Package llm implements a client for OpenAI-compatible chat completions
// APIs, such as OpenRouter.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Client calls an OpenAI-compatible chat completions endpoint.
type Client struct {
	endpoint string
	apiKey   string
	headers  map[string]string
	http     *http.Client
}

// New creates a client for the API at baseURL, for example
// "https://openrouter.ai/api/v1".
func New(baseURL, apiKey string, headers map[string]string) *Client {
	copiedHeaders := make(map[string]string, len(headers))
	for key, value := range headers {
		copiedHeaders[key] = value
	}
	// No overall client timeout: a response can stream for a long time while
	// tools run. Cancellation is controlled by the request context (ctrl+c),
	// while transport timeouts still catch unreachable or stalled endpoints.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ResponseHeaderTimeout = 60 * time.Second
	transport.IdleConnTimeout = 90 * time.Second

	return &Client{
		endpoint: strings.TrimRight(baseURL, "/") + "/chat/completions",
		apiKey:   apiKey,
		headers:  copiedHeaders,
		http:     &http.Client{Transport: transport},
	}
}

// Message is a chat message sent to or received from the API.
type Message struct {
	Role             string            `json:"role"`
	Content          string            `json:"content"`
	Reasoning        string            `json:"reasoning,omitempty"`
	ReasoningContent *string           `json:"reasoning_content,omitempty"`
	ReasoningDetails []json.RawMessage `json:"reasoning_details,omitempty"`
	ToolCalls        []ToolCall        `json:"tool_calls,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
}

// ToolCall is a tool invocation requested by the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall is the function name and raw JSON arguments of a tool call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool is a function declared to the model.
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// Function describes a callable function.
type Function struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict,omitempty"`
}

// Reasoning configures the model's reasoning effort.
type Reasoning struct {
	Effort string `json:"effort"`
}

// StreamOptions configures a streaming request.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// Request is a chat completions request.
type Request struct {
	Model         string            `json:"model"`
	Messages      []Message         `json:"messages"`
	Tools         []Tool            `json:"tools,omitempty"`
	Reasoning     *Reasoning        `json:"reasoning,omitempty"`
	Provider      map[string]any    `json:"provider,omitempty"`
	MaxTokens     int               `json:"max_tokens,omitempty"`
	Extra         map[string]any    `json:"-"`
	Headers       map[string]string `json:"-"`
	Stream        bool              `json:"stream"`
	StreamOptions *StreamOptions    `json:"stream_options,omitempty"`
}

// MarshalJSON merges Extra fields into the request body.
func (r Request) MarshalJSON() ([]byte, error) {
	type alias Request
	data, err := json.Marshal(alias(r))
	if err != nil || len(r.Extra) == 0 {
		return data, err
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for key, value := range r.Extra {
		fields[key] = value
	}
	return json.Marshal(fields)
}

// maxStreamLineBytes is the largest accepted server-sent event line.
const maxStreamLineBytes = 16 << 20

// Delta is an incremental piece of a streamed response.
type Delta struct {
	Content   string
	Reasoning string
}

// Response is a chat completions response.
type Response struct {
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
	Error   *Error   `json:"error,omitempty"`
}

// Choice is one completion choice.
type Choice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage is the token usage of a request.
type Usage struct {
	PromptTokens         int                  `json:"prompt_tokens"`
	CompletionTokens     int                  `json:"completion_tokens"`
	TotalTokens          int                  `json:"total_tokens"`
	CachedTokens         int                  `json:"cached_tokens,omitempty"`
	PromptCacheHitTokens int                  `json:"prompt_cache_hit_tokens,omitempty"`
	PromptTokensDetails  *PromptTokensDetails `json:"prompt_tokens_details,omitempty"`
	Cost                 *float64             `json:"cost,omitempty"`
	IsBYOK               bool                 `json:"is_byok,omitempty"`
	CostDetails          *CostDetails         `json:"cost_details,omitempty"`
}

// PromptTokensDetails is the prompt token breakdown some providers report.
type PromptTokensDetails struct {
	CachedTokens     int `json:"cached_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
}

// CostDetails is OpenRouter's upstream billing breakdown for BYOK requests.
type CostDetails struct {
	UpstreamInferenceCost        *float64 `json:"upstream_inference_cost"`
	UpstreamInferencePromptCost  *float64 `json:"upstream_inference_prompt_cost"`
	UpstreamInferenceCompletions *float64 `json:"upstream_inference_completions_cost"`
}

// CacheReadTokens returns the number of cached prompt tokens.
func (u Usage) CacheReadTokens() int {
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > 0 {
		return u.PromptTokensDetails.CachedTokens
	}
	if u.PromptCacheHitTokens > 0 {
		return u.PromptCacheHitTokens
	}
	return u.CachedTokens
}

// CacheWriteTokens returns the number of prompt tokens written to the cache.
func (u Usage) CacheWriteTokens() int {
	if u.PromptTokensDetails == nil {
		return 0
	}
	return u.PromptTokensDetails.CacheWriteTokens
}

// InputTokens returns the prompt tokens that were neither read from nor written
// to the cache, following OpenAI/OpenRouter semantics.
func (u Usage) InputTokens() int {
	input := u.PromptTokens - u.CacheReadTokens() - u.CacheWriteTokens()
	if input < 0 {
		return 0
	}
	return input
}

// OutputTokens returns the completion tokens, which include reasoning tokens.
func (u Usage) OutputTokens() int {
	return u.CompletionTokens
}

// ReportedCost returns OpenRouter's billed cost, combining it with the upstream
// cost for BYOK requests, like Pi Agent.
func (u Usage) ReportedCost() (float64, bool) {
	if !validCost(u.Cost) {
		return 0, false
	}
	if !u.IsBYOK {
		return *u.Cost, true
	}
	if u.CostDetails == nil {
		return 0, false
	}
	upstream := u.CostDetails.UpstreamInferenceCost
	if upstream == nil {
		if u.CostDetails.UpstreamInferencePromptCost == nil || u.CostDetails.UpstreamInferenceCompletions == nil {
			return 0, false
		}
		total := *u.CostDetails.UpstreamInferencePromptCost + *u.CostDetails.UpstreamInferenceCompletions
		upstream = &total
	}
	if !validCost(upstream) {
		return 0, false
	}
	return *u.Cost + *upstream, true
}

// validCost reports whether a reported amount is a finite, non-negative cost.
func validCost(value *float64) bool {
	if value == nil {
		return false
	}
	return !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0
}

// Error is an API error object.
type Error struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Code    any    `json:"code,omitempty"`
}

// Error implements error.
func (e *Error) Error() string {
	if e.Type != "" {
		return e.Type + ": " + e.Message
	}
	return e.Message
}

// streamChunk is a single server-sent event of a chat completions stream.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string           `json:"content"`
			Reasoning        string           `json:"reasoning"`
			ReasoningDetails []map[string]any `json:"reasoning_details"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
	Error *Error `json:"error,omitempty"`
}

// ChatStream sends a streaming chat completions request. onDelta is called for
// every content and reasoning delta. The returned response contains the
// complete message assembled from the stream.
func (c *Client) ChatStream(ctx context.Context, request Request, onDelta func(Delta)) (*Response, error) {
	request.Stream = true
	request.StreamOptions = &StreamOptions{IncludeUsage: true}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	c.applyHeaders(httpRequest, request.Headers)
	httpRequest.Header.Set("Accept", "text/event-stream")

	httpResponse, err := c.http.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode < 200 || httpResponse.StatusCode > 299 {
		data, _ := io.ReadAll(io.LimitReader(httpResponse.Body, 8<<20))
		return nil, httpError(httpResponse.Status, data)
	}

	var (
		content      strings.Builder
		reasoning    strings.Builder
		finishReason string
		usage        Usage
		done         bool
	)
	toolCalls := make(map[int]*ToolCall)
	reasoningDetails := make(map[int]map[string]any)

	scanner := bufio.NewScanner(httpResponse.Body)
	// Large tool results can arrive in a single SSE line, so allow big lines.
	scanner.Buffer(make([]byte, 64*1024), maxStreamLineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			done = true
			break
		}
		if payload == "" {
			continue
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return nil, fmt.Errorf("decode stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return nil, chunk.Error
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				content.WriteString(choice.Delta.Content)
				if onDelta != nil {
					onDelta(Delta{Content: choice.Delta.Content})
				}
			}
			if choice.Delta.Reasoning != "" {
				reasoning.WriteString(choice.Delta.Reasoning)
				if onDelta != nil {
					onDelta(Delta{Reasoning: choice.Delta.Reasoning})
				}
			}
			for _, detail := range choice.Delta.ReasoningDetails {
				index := 0
				if value, ok := detail["index"].(float64); ok {
					index = int(value)
				}
				existing, ok := reasoningDetails[index]
				if !ok {
					existing = make(map[string]any, len(detail))
					reasoningDetails[index] = existing
				}
				for key, value := range detail {
					if key == "text" {
						text, _ := value.(string)
						previous, _ := existing["text"].(string)
						existing["text"] = previous + text
						continue
					}
					existing[key] = value
				}
			}
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
			for _, delta := range choice.Delta.ToolCalls {
				call := toolCalls[delta.Index]
				if call == nil {
					call = &ToolCall{Type: "function"}
					toolCalls[delta.Index] = call
				}
				if delta.ID != "" {
					call.ID = delta.ID
				}
				if delta.Type != "" {
					call.Type = delta.Type
				}
				call.Function.Name += delta.Function.Name
				call.Function.Arguments += delta.Function.Arguments
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stream: %w", err)
	}
	// Some OpenAI-compatible servers close the stream after the final chunk
	// without sending `data: [DONE]`; a finish_reason already proves completion.
	if !done && finishReason == "" {
		return nil, fmt.Errorf("stream ended before completion")
	}

	indexes := make([]int, 0, len(toolCalls))
	for index := range toolCalls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	calls := make([]ToolCall, 0, len(indexes))
	for _, index := range indexes {
		calls = append(calls, *toolCalls[index])
	}

	detailIndexes := make([]int, 0, len(reasoningDetails))
	for index := range reasoningDetails {
		detailIndexes = append(detailIndexes, index)
	}
	sort.Ints(detailIndexes)
	var details []json.RawMessage
	for _, index := range detailIndexes {
		encoded, err := json.Marshal(reasoningDetails[index])
		if err != nil {
			continue
		}
		details = append(details, encoded)
	}

	return &Response{
		Choices: []Choice{{
			Message: Message{
				Role:             "assistant",
				Content:          content.String(),
				Reasoning:        reasoning.String(),
				ReasoningDetails: details,
				ToolCalls:        calls,
			},
			FinishReason: finishReason,
		}},
		Usage: usage,
	}, nil
}

func (c *Client) applyHeaders(httpRequest *http.Request, requestHeaders map[string]string) {
	httpRequest.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for key, value := range c.headers {
		httpRequest.Header.Set(key, value)
	}
	for key, value := range requestHeaders {
		httpRequest.Header.Set(key, value)
	}
}

func httpError(status string, data []byte) error {
	var apiError struct {
		Error *Error `json:"error"`
	}
	if json.Unmarshal(data, &apiError) == nil && apiError.Error != nil && apiError.Error.Message != "" {
		return fmt.Errorf("HTTP %s: %w", status, apiError.Error)
	}
	return fmt.Errorf("HTTP %s: %s", status, summarize(data))
}

func summarize(data []byte) string {
	const max = 500
	text := strings.TrimSpace(string(data))
	if len(text) > max {
		return text[:max] + "…"
	}
	return text
}
