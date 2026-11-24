package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

func int64Ptr(value int64) *int64 { return &value }

func TestNew(t *testing.T) {
	sess := New("/tmp/work")
	if _, err := uuid.Parse(sess.ID); err != nil {
		t.Fatalf("ID %q is not a UUID: %v", sess.ID, err)
	}
	if sess.WorkDir != "/tmp/work" {
		t.Errorf("WorkDir = %q", sess.WorkDir)
	}
	if sess.CreatedAt.IsZero() || sess.UpdatedAt.IsZero() {
		t.Error("timestamps are zero")
	}
	if sess.Model != DefaultModel {
		t.Errorf("Model = %q, want %q", sess.Model, DefaultModel)
	}
	if sess.ThinkingLevel != DefaultThinkingLevel {
		t.Errorf("ThinkingLevel = %q, want %q", sess.ThinkingLevel, DefaultThinkingLevel)
	}
	if sess.Messages == nil {
		t.Error("Messages = nil, want empty slice")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := New("/tmp/work")
	sess.Provider = "openrouter"
	sess.Model = "deepseek/deepseek-v4.1-flash"
	sess.ThinkingLevel = "high"
	sess.TokensUsed = 1234
	sess.ContextWindow = 1048576
	sess.Messages = []Message{
		{Role: "user", Content: "hello"},
		{
			Role:     "assistant",
			Thinking: "I should check the time.",
			ToolCalls: []ToolCall{{
				ID:        "call-1",
				Name:      "datetime",
				Arguments: `{}`,
			}},
		},
		{Role: "tool", Content: "2026-11-24T14:46:12+01:00", ToolCallID: "call-1", IsError: true, DurationMs: int64Ptr(42)},
	}

	if err := store.Save(sess); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	path := filepath.Join(store.Dir(), sess.ID+".json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat session file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("session file permissions = %o, want 600", perm)
	}

	loaded, err := store.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(loaded, sess) {
		t.Errorf("loaded session = %+v, want %+v", loaded, sess)
	}
}

func TestSaveUpdatesTimestamp(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := New("/tmp/work")
	created := sess.CreatedAt
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	first := sess.UpdatedAt

	time.Sleep(time.Millisecond)
	if err := store.Save(sess); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}
	if !sess.UpdatedAt.After(first) {
		t.Errorf("UpdatedAt = %v, want after %v", sess.UpdatedAt, first)
	}
	if !sess.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt changed from %v to %v", created, sess.CreatedAt)
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "sessions")
	store := NewStore(dir)

	if err := store.Save(New("/tmp/work")); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("stat directory: %v", err)
	}
}

func TestLoadInvalidID(t *testing.T) {
	store := NewStore(t.TempDir())
	for _, id := range []string{"not-a-uuid", "../escape", ""} {
		if _, err := store.Load(id); err == nil {
			t.Errorf("Load(%q) error = nil, want error", id)
		}
	}
}

func TestLoadMissingSession(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.Load(uuid.NewString()); err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}

func TestLoadMismatchedID(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := os.MkdirAll(store.Dir(), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := uuid.NewString()
	content := `{"id": "` + uuid.NewString() + `", "messages": []}`
	if err := os.WriteFile(filepath.Join(store.Dir(), id+".json"), []byte(content), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	if _, err := store.Load(id); err == nil {
		t.Fatal("Load() error = nil, want mismatch error")
	}
}

func TestListReturnsNewestFirst(t *testing.T) {
	store := NewStore(t.TempDir())
	older := New("/tmp/older")
	if err := store.Save(older); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	time.Sleep(time.Millisecond)
	newer := New("/tmp/newer")
	if err := store.Save(newer); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len(sessions) = %d, want 2", len(sessions))
	}
	if sessions[0].ID != newer.ID || sessions[1].ID != older.ID {
		t.Errorf("session order = [%s %s], want [%s %s]", sessions[0].ID, sessions[1].ID, newer.ID, older.ID)
	}
}

func TestListMissingDirectory(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "missing"))
	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("len(sessions) = %d, want 0", len(sessions))
	}
}

func TestLoadAlwaysStartsInFoliaMode(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := New("/tmp/work")
	sess.Mode = ModeNormal
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := store.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Mode != ModeFolia {
		t.Errorf("Mode = %q, want %q", loaded.Mode, ModeFolia)
	}
}

func TestNewStartsInFoliaMode(t *testing.T) {
	if got := New("/tmp/work").Mode; got != ModeFolia {
		t.Errorf("Mode = %q, want %q", got, ModeFolia)
	}
}

func TestReasoningDetailsRoundTrip(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := New("/tmp/work")
	sess.Messages = []Message{
		{Role: "user", Content: "hello"},
		{
			Role:             "assistant",
			Thinking:         "reasoning text",
			ReasoningDetails: []json.RawMessage{json.RawMessage(`{"type":"reasoning.text","text":"reasoning text","index":0}`)},
			Content:          "answer",
		},
	}
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := store.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.Messages[1].ReasoningDetails) != 1 {
		t.Fatalf("reasoning details = %s", loaded.Messages[1].ReasoningDetails)
	}
	var got, want any
	if err := json.Unmarshal(loaded.Messages[1].ReasoningDetails[0], &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(sess.Messages[1].ReasoningDetails[0], &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reasoning details changed: %v", got)
	}
}

func TestListSkipsCorruptSessions(t *testing.T) {
	store := NewStore(t.TempDir())
	good := New("/tmp/work")
	if err := store.Save(good); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	corruptID := uuid.NewString()
	path := filepath.Join(store.Dir(), corruptID+".json")
	if err := os.WriteFile(path, []byte(`{"id": "`+corruptID+`", "messages": [`), 0o600); err != nil {
		t.Fatal(err)
	}

	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != good.ID {
		t.Errorf("sessions = %+v, want only the readable session", sessions)
	}
}

func TestCostRoundTrip(t *testing.T) {
	store := NewStore(t.TempDir())
	sess := New("/tmp/work")
	sess.CostUSD = 0.0421
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loaded, err := store.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.CostUSD != 0.0421 {
		t.Errorf("CostUSD = %v", loaded.CostUSD)
	}
}
