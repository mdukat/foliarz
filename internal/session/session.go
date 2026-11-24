// Package session holds the state of a single Foliarz session.
package session

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/mdukat/foliarz/internal/thinking"
)

// Defaults used when a session starts.
const (
	DefaultModel         = "unset"
	DefaultThinkingLevel = thinking.Off

	// ModeFolia is the read-only mode. Foliarz always starts in this mode.
	ModeFolia = "FOLIA"

	// ModeNormal is the mode where non-read-only tools require approval.
	ModeNormal = "NORMAL"

	// ModeYolo is the mode where all tools run without questions.
	ModeYolo = "YOLO"
)

// ToolCall is a tool invocation requested by the model.
type ToolCall struct {
	// ID identifies the call in tool results.
	ID string `json:"id"`

	// Name is the tool name.
	Name string `json:"name"`

	// Arguments is the raw JSON arguments of the call.
	Arguments string `json:"arguments"`
}

// Message is a single message of a session transcript.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`

	// Thinking is the model's reasoning for an assistant message.
	Thinking string `json:"thinking,omitempty"`

	// ReasoningDetails are the provider's structured reasoning blocks for an
	// assistant message, replayed on later requests.
	ReasoningDetails []json.RawMessage `json:"reasoningDetails,omitempty"`

	// ToolCalls are the tool invocations requested by an assistant message.
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`

	// ToolCallID links a tool result message to its tool call.
	ToolCallID string `json:"toolCallId,omitempty"`

	// IsError marks a tool result that failed.
	IsError bool `json:"isError,omitempty"`

	// DurationMs is how long the tool took, for tool results.
	DurationMs *int64 `json:"durationMs,omitempty"`
}

// Session is the state of the current Foliarz session. It is the unit that is
// persisted to and restored from a session file.
type Session struct {
	// ID is the session UUID, also used as the session file name.
	ID string `json:"id"`

	// CreatedAt is when the session was created.
	CreatedAt time.Time `json:"createdAt"`

	// UpdatedAt is when the session was last saved.
	UpdatedAt time.Time `json:"updatedAt"`

	// WorkDir is the directory the agent works in.
	WorkDir string `json:"workDir"`

	// Mode is the current tool mode, ModeFolia, ModeNormal or ModeYolo.
	Mode string `json:"mode"`

	// ProjectContext holds project instructions loaded at session start, such
	// as AGENTS.md. It is not persisted; it is reloaded for each session.
	ProjectContext string `json:"-"`

	// Summary is the structured summary of messages before FirstKeptMessage.
	Summary string `json:"summary,omitempty"`

	// FirstKeptMessage is the index of the first message kept after compaction.
	FirstKeptMessage int `json:"firstKeptMessage,omitempty"`

	// SummaryReadFiles and SummaryModifiedFiles are cumulative file lists for
	// compaction summaries.
	SummaryReadFiles     []string `json:"summaryReadFiles,omitempty"`
	SummaryModifiedFiles []string `json:"summaryModifiedFiles,omitempty"`

	// SummaryTokensBefore is the estimated context size before the last
	// compaction.
	SummaryTokensBefore int `json:"summaryTokensBefore,omitempty"`

	// Provider is the ID of the model provider.
	Provider string `json:"provider,omitempty"`

	// Model is the name of the model used by the session.
	Model string `json:"model,omitempty"`

	// ThinkingLevel is the reasoning effort requested from the model.
	ThinkingLevel string `json:"thinkingLevel"`

	// TokensUsed is the number of tokens consumed in the session.
	TokensUsed int `json:"tokensUsed"`

	// ContextWindow is the context window size of the model in tokens;
	// zero means unknown.
	ContextWindow int `json:"contextWindow,omitempty"`

	// CostUSD is the total spend of the session in US dollars.
	CostUSD float64 `json:"costUsd,omitempty"`

	// Messages is the session transcript.
	Messages []Message `json:"messages"`
}

// New creates a session with a fresh UUID working in workDir.
func New(workDir string) *Session {
	now := time.Now().UTC()
	return &Session{
		ID:            uuid.NewString(),
		CreatedAt:     now,
		UpdatedAt:     now,
		WorkDir:       workDir,
		Mode:          ModeFolia,
		Model:         DefaultModel,
		ThinkingLevel: DefaultThinkingLevel,
		Messages:      []Message{},
	}
}
