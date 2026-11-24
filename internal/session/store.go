package session

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Store persists sessions as JSON files in a directory.
type Store struct {
	dir string
}

// NewStore creates a session store backed by dir.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Dir returns the directory sessions are stored in.
func (s *Store) Dir() string {
	return s.dir
}

// Save writes the session to its JSON file, creating it or replacing it
// atomically.
func (s *Store) Save(sess *Session) error {
	if sess.ID == "" {
		sess.ID = uuid.NewString()
	} else if _, err := uuid.Parse(sess.ID); err != nil {
		return fmt.Errorf("invalid session ID %q: %w", sess.ID, err)
	}

	now := time.Now().UTC()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = now
	}
	sess.UpdatedAt = now
	if sess.Messages == nil {
		sess.Messages = []Message{}
	}

	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create session directory %s: %w", s.dir, err)
	}

	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session %s: %w", sess.ID, err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(s.dir, ".session-*.json")
	if err != nil {
		return fmt.Errorf("create session file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write session %s: %w", sess.ID, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync session %s: %w", sess.ID, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close session %s: %w", sess.ID, err)
	}

	path := s.path(sess.ID)
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("save session %s: %w", sess.ID, err)
	}
	return nil
}

// Load reads the session with the given ID.
func (s *Store) Load(id string) (*Session, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, fmt.Errorf("invalid session ID %q: %w", id, err)
	}

	data, err := os.ReadFile(s.path(id))
	if err != nil {
		return nil, fmt.Errorf("read session %s: %w", id, err)
	}

	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("parse session %s: %w", id, err)
	}
	if sess.ID != id {
		return nil, fmt.Errorf("session file %s declares ID %q", id, sess.ID)
	}
	// Foliarz always starts in FOLIA mode, even when reopening a session that
	// was saved in NORMAL mode.
	sess.Mode = ModeFolia
	return &sess, nil
}

// List returns all valid sessions, most recently updated first.
func (s *Store) List() ([]*Session, error) {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return []*Session{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session directory %s: %w", s.dir, err)
	}

	sessions := make([]*Session, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		sess, err := s.Load(id)
		if err != nil {
			// A single damaged session file must not hide the others.
			slog.Warn("skipping unreadable session", "session", id, "error", err)
			continue
		}
		sessions = append(sessions, sess)
	}

	sort.Slice(sessions, func(i, j int) bool {
		if !sessions[i].UpdatedAt.Equal(sessions[j].UpdatedAt) {
			return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
		}
		return sessions[i].ID < sessions[j].ID
	})
	return sessions, nil
}

func (s *Store) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}
