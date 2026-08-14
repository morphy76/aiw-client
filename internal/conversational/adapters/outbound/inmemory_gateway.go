package outbound

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/morphy76/aiw-client/internal/conversational/application/ports/inbound"
	outboundPorts "github.com/morphy76/aiw-client/internal/conversational/application/ports/outbound"
	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
)

type inMemorySession struct {
	externalID string
	closed     bool
	messages   []string
	handler    outboundPorts.StreamEventHandler
}

// InMemoryAIWGateway provides an in-memory test implementation of AIWGateway.
type InMemoryAIWGateway struct {
	mu       sync.RWMutex
	sessions map[string]*inMemorySession
}

// NewInMemoryAIWGateway creates a new InMemoryAIWGateway.
func NewInMemoryAIWGateway() *InMemoryAIWGateway {
	return &InMemoryAIWGateway{
		sessions: make(map[string]*inMemorySession),
	}
}

// OpenSessionStream simulates opening an SSE session and immediately dispatching OnCreated.
func (g *InMemoryAIWGateway) OpenSessionStream(
	_ context.Context,
	cmd inbound.OpenConversationCommand,
	handler outboundPorts.StreamEventHandler,
) error {
	if cmd.ExternalID == "" {
		return model.ErrInvalidExternalID
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	dialogID := "diag-" + uuid.New().String()
	g.sessions[dialogID] = &inMemorySession{
		externalID: cmd.ExternalID,
		closed:     false,
		messages:   make([]string, 0),
		handler:    handler,
	}

	if handler != nil {
		if err := handler.OnCreated(dialogID); err != nil {
			return err
		}
	}
	return nil
}

// SendCustomerMessage simulates sending a customer message and triggers OnCustomerMessage handler.
func (g *InMemoryAIWGateway) SendCustomerMessage(
	_ context.Context,
	cmd inbound.AddCustomerMessageCommand,
	dialogID string,
) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	session, exists := g.sessions[dialogID]
	if !exists {
		return model.ErrConversationNotFound
	}
	if session.closed {
		return model.ErrConversationClosed
	}

	session.messages = append(session.messages, cmd.Message)

	if session.handler != nil {
		_ = session.handler.OnCustomerMessage(cmd.Message)
	}
	return nil
}

// SimulateBotResponse helper to push a bot response to the registered handler.
func (g *InMemoryAIWGateway) SimulateBotResponse(dialogID string, botMessage string) error {
	g.mu.RLock()
	session, exists := g.sessions[dialogID]
	g.mu.RUnlock()

	if !exists {
		return model.ErrConversationNotFound
	}
	if session.handler != nil {
		return session.handler.OnBotMessage(botMessage)
	}
	return nil
}

// CloseSession simulates closing a session on the platform.
func (g *InMemoryAIWGateway) CloseSession(
	_ context.Context,
	_ inbound.CloseConversationCommand,
	dialogID string,
) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	session, exists := g.sessions[dialogID]
	if !exists {
		return model.ErrConversationNotFound
	}
	session.closed = true

	if session.handler != nil {
		session.handler.OnClosed()
	}
	return nil
}
