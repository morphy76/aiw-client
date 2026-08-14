package model

import (
	"strings"
	"sync"
	"time"
)

// Conversation is the aggregate root representing a conversational dialog session.
type Conversation struct {
	mu         sync.RWMutex
	externalID string
	dialogID   string
	state      State
	messages   []Message
	createdAt  time.Time
	updatedAt  time.Time
}

// NewConversation instantiates a new Conversation aggregate in StateIdle.
func NewConversation(externalID string) (*Conversation, error) {
	trimmed := strings.TrimSpace(externalID)
	if trimmed == "" {
		return nil, ErrInvalidExternalID
	}
	now := time.Now().UTC()
	return &Conversation{
		externalID: trimmed,
		state:      StateIdle,
		messages:   make([]Message, 0),
		createdAt:  now,
		updatedAt:  now,
	}, nil
}

// Reconstitute creates a Conversation aggregate from stored state (used by repository mappers).
func Reconstitute(externalID, dialogID string, state State, messages []Message, createdAt, updatedAt time.Time) *Conversation {
	msgs := make([]Message, len(messages))
	copy(msgs, messages)
	return &Conversation{
		externalID: externalID,
		dialogID:   dialogID,
		state:      state,
		messages:   msgs,
		createdAt:  createdAt,
		updatedAt:  updatedAt,
	}
}

// ExternalID returns the external business identifier.
func (c *Conversation) ExternalID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.externalID
}

// DialogID returns the conversation dialog ID.
func (c *Conversation) DialogID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.dialogID
}

// State returns the current conversation state.
func (c *Conversation) State() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// IsActive reports whether the conversation is actively open.
func (c *Conversation) IsActive() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state == StateActive
}

// CreatedAt returns the conversation initialization time.
func (c *Conversation) CreatedAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.createdAt
}

// UpdatedAt returns the conversation last modification time.
func (c *Conversation) UpdatedAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.updatedAt
}

// Messages returns a copy of all conversation messages.
func (c *Conversation) Messages() []Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	msgs := make([]Message, len(c.messages))
	copy(msgs, c.messages)
	return msgs
}

// Open transitions the conversation to active state with the assigned dialog ID.
func (c *Conversation) Open(dialogID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	trimmed := strings.TrimSpace(dialogID)
	if trimmed == "" {
		return ErrInvalidDialogID
	}
	if c.state == StateActive {
		return ErrConversationAlreadyOpen
	}
	if c.state == StateClosed {
		return ErrConversationClosed
	}

	c.dialogID = trimmed
	c.state = StateActive
	c.updatedAt = time.Now().UTC()
	return nil
}

// AddMessage appends a message to the active conversation.
func (c *Conversation) AddMessage(msg Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == StateIdle {
		return ErrConversationNotActive
	}
	if c.state == StateClosed {
		return ErrConversationClosed
	}

	c.messages = append(c.messages, msg)
	c.updatedAt = time.Now().UTC()
	return nil
}

// Close transitions the conversation to closed state.
func (c *Conversation) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == StateClosed {
		return ErrConversationClosed
	}

	c.state = StateClosed
	c.updatedAt = time.Now().UTC()
	return nil
}
