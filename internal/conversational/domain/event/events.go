package event

import (
	"time"

	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
)

// ConversationOpenedEvent is published when a conversation session is successfully opened.
type ConversationOpenedEvent struct {
	ExternalID string
	DialogID   string
	Timestamp  time.Time
}

// CustomerMessageAddedEvent is published when a message from a customer is added.
type CustomerMessageAddedEvent struct {
	ExternalID string
	DialogID   string
	Message    model.Message
	Timestamp  time.Time
}

// ConversationClosedEvent is published when a conversation session is terminated.
type ConversationClosedEvent struct {
	ExternalID string
	DialogID   string
	Timestamp  time.Time
}
