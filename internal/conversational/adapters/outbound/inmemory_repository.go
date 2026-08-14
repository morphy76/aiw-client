package outbound

import (
	"context"
	"sync"

	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
)

// InMemoryConversationRepository provides an in-memory thread-safe implementation of ConversationRepository.
type InMemoryConversationRepository struct {
	mu     sync.RWMutex
	byExt  map[string]*model.Conversation
	byDiag map[string]*model.Conversation
}

// NewInMemoryConversationRepository creates a new InMemoryConversationRepository.
func NewInMemoryConversationRepository() *InMemoryConversationRepository {
	return &InMemoryConversationRepository{
		byExt:  make(map[string]*model.Conversation),
		byDiag: make(map[string]*model.Conversation),
	}
}

// Save stores or updates a conversation aggregate.
func (r *InMemoryConversationRepository) Save(_ context.Context, conv *model.Conversation) error {
	if conv == nil {
		return model.ErrConversationNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.byExt[conv.ExternalID()] = conv
	if conv.DialogID() != "" {
		r.byDiag[conv.DialogID()] = conv
	}
	return nil
}

// FindByExternalID retrieves a conversation aggregate by its external identifier.
func (r *InMemoryConversationRepository) FindByExternalID(_ context.Context, externalID string) (*model.Conversation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	conv, ok := r.byExt[externalID]
	if !ok {
		return nil, model.ErrConversationNotFound
	}
	return conv, nil
}

// FindByDialogID retrieves a conversation aggregate by its dialog identifier.
func (r *InMemoryConversationRepository) FindByDialogID(_ context.Context, dialogID string) (*model.Conversation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	conv, ok := r.byDiag[dialogID]
	if !ok {
		return nil, model.ErrConversationNotFound
	}
	return conv, nil
}

// Delete removes a conversation aggregate.
func (r *InMemoryConversationRepository) Delete(_ context.Context, externalID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if conv, ok := r.byExt[externalID]; ok {
		delete(r.byDiag, conv.DialogID())
		delete(r.byExt, externalID)
	}
	return nil
}
