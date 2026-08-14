package outbound

import (
	"context"

	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
)

// ConversationRepository defines the persistence contract for conversation aggregates.
type ConversationRepository interface {
	// Save persists or updates the conversation aggregate.
	Save(ctx context.Context, conv *model.Conversation) error

	// FindByExternalID retrieves a conversation aggregate by customer external ID.
	FindByExternalID(ctx context.Context, externalID string) (*model.Conversation, error)

	// FindByDialogID retrieves a conversation aggregate by dialog ID.
	FindByDialogID(ctx context.Context, dialogID string) (*model.Conversation, error)

	// Delete removes a conversation aggregate by external ID.
	Delete(ctx context.Context, externalID string) error
}
