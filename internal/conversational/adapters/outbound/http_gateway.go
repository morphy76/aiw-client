package outbound

import (
	"context"

	"github.com/morphy76/aiw-client/internal/conversational/application/ports/inbound"
	outboundPorts "github.com/morphy76/aiw-client/internal/conversational/application/ports/outbound"
	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
)

// HTTPGateway composes modular outbound HTTP clients to fulfill the outboundPorts.AIWGateway interface.
type HTTPGateway struct {
	streamClient  *LiveStreamClient
	messageClient *MessageClient
	sessionClient *SessionClient
}

// NewHTTPGateway creates a new HTTPGateway composing stream, message, and session sub-clients.
func NewHTTPGateway(client HTTPClient, baseURL string) *HTTPGateway {
	return &HTTPGateway{
		streamClient:  NewLiveStreamClient(client, baseURL),
		messageClient: NewMessageClient(client, baseURL),
		sessionClient: NewSessionClient(client, baseURL),
	}
}

// OpenSessionStream delegates SSE live stream management to LiveStreamClient.
func (g *HTTPGateway) OpenSessionStream(
	ctx context.Context,
	cmd inbound.OpenConversationCommand,
	handler outboundPorts.StreamEventHandler,
) error {
	return g.streamClient.OpenSessionStream(ctx, cmd, handler)
}

// SendCustomerMessage delegates customer message dispatching to MessageClient.
func (g *HTTPGateway) SendCustomerMessage(
	ctx context.Context,
	cmd inbound.AddCustomerMessageCommand,
	dialogID string,
) error {
	return g.messageClient.SendCustomerMessage(ctx, cmd, dialogID)
}

// CloseSession delegates dialog session closing to MessageClient.
func (g *HTTPGateway) CloseSession(
	ctx context.Context,
	cmd inbound.CloseConversationCommand,
	dialogID string,
) error {
	return g.messageClient.CloseSession(ctx, cmd, dialogID)
}

// ListSessions delegates dialog session listing and filtering to SessionClient.
func (g *HTTPGateway) ListSessions(
	ctx context.Context,
	cmd inbound.ListSessionsCommand,
) ([]model.RecentActivity, error) {
	return g.sessionClient.ListSessions(ctx, cmd)
}

// GetSessionRecording delegates recording history fetching to SessionClient.
func (g *HTTPGateway) GetSessionRecording(
	ctx context.Context,
	cmd inbound.RestoreConversationCommand,
) ([]model.Message, error) {
	return g.sessionClient.GetSessionRecording(ctx, cmd)
}
