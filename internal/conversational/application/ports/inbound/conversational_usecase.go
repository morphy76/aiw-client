package inbound

import (
	"context"

	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
)

// StreamEventHandler handles real-time SSE events dispatched during conversation streaming.
type StreamEventHandler interface {
	OnCreated(dialogID string) error
	OnCustomerMessage(text string) error
	OnBotMessage(text string) error
	OnAborted(reason string)
	OnClosed()
	OnError(err error)
}

// OpenConversationCommand encapsulates inputs for opening a conversation session.
type OpenConversationCommand struct {
	ExternalID  string
	Tenant      string
	DialogModel string
	BearerToken string
	Sandbox     bool
	BaseURL     string
	Headers     map[string]string
}

// AddCustomerMessageCommand encapsulates inputs for appending and dispatching a customer message.
type AddCustomerMessageCommand struct {
	ExternalID  string
	Message     string
	Tenant      string
	BearerToken string
	Sandbox     bool
	BaseURL     string
	Headers     map[string]string
}

// CloseConversationCommand encapsulates inputs for closing a conversation session.
type CloseConversationCommand struct {
	ExternalID  string
	Tenant      string
	BearerToken string
	Sandbox     bool
	BaseURL     string
	Headers     map[string]string
}

// ConversationalUseCase defines the application use-case boundary for managing conversations.
type ConversationalUseCase interface {
	// OpenConversation executes the conversation opening workflow and attaches the event handler.
	OpenConversation(ctx context.Context, cmd OpenConversationCommand, handler StreamEventHandler) (*model.Conversation, error)

	// AddCustomerMessage appends and dispatches a customer message.
	AddCustomerMessage(ctx context.Context, cmd AddCustomerMessageCommand) (*model.Conversation, error)

	// CloseConversation executes the conversation closing workflow.
	CloseConversation(ctx context.Context, cmd CloseConversationCommand) error

	// GetConversation retrieves an existing conversation by external ID.
	GetConversation(ctx context.Context, externalID string) (*model.Conversation, error)
}
