package agent

import (
	"sync"

	"github.com/mdukat/foliarz/internal/session"
)

// MessageQueue holds user messages that steer a running response. The agent
// drains it between model steps so a queued message is injected right after the
// current assistant turn and its tool calls.
type MessageQueue struct {
	mu       sync.Mutex
	messages []session.Message
}

// NewMessageQueue creates an empty steering queue.
func NewMessageQueue() *MessageQueue {
	return &MessageQueue{}
}

// Push adds a user message to the queue.
func (q *MessageQueue) Push(message session.Message) {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.messages = append(q.messages, message)
}

// Drain removes and returns all queued messages.
func (q *MessageQueue) Drain() []session.Message {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	messages := q.messages
	q.messages = nil
	return messages
}

// Len returns the number of queued messages.
func (q *MessageQueue) Len() int {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.messages)
}
