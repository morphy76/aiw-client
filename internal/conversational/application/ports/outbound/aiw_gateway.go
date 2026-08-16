package outbound

import (
	"context"

	"github.com/morphy76/aiw-client/internal/conversational/application/ports/inbound"
	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
)

// StreamEventHandler handles events dispatched from the real-time conversational SSE stream.
type StreamEventHandler interface {
	// OnOpen is called when the SSE transport stream is connected (HTTP 200).
	OnOpen() error

	// OnCreated is called when lifecycle.event == "created" is received with the assigned dialog ID.
	OnCreated(dialogID string) error

	// OnCustomerMessage is called when message.event == "messageAdded" with role "CUSTOMER" is received.
	OnCustomerMessage(text string) error

	// OnBotMessage is called when message.event == "messageAdded" with role "BOT" or "AGENT" is received.
	OnBotMessage(text string) error

	// OnDialogTerminated is called when lifecycle.event == "aborted" or "closed" is received.
	OnDialogTerminated(isAborted bool, reason string)

	// OnClosed is called when stream transport termination occurs.
	OnClosed()

	// OnError is called on stream, transport, or parsing errors, supplying a cancellation handle.
	OnError(err error, cancel func(requestDialogTermination bool))
}

// AIWGateway defines the external network/streaming contract to the AIW platform.
type AIWGateway interface {
	// OpenSessionStream opens an SSE stream with the AIW platform, waits for the initial created event,
	// and routes real-time events to the provided StreamEventHandler.
	OpenSessionStream(ctx context.Context, cmd inbound.OpenConversationCommand, handler StreamEventHandler) error

	// SendCustomerMessage transmits a customer message payload to POST /dialog/api/conversation/v1.0/message/{dialogId}.
	SendCustomerMessage(ctx context.Context, cmd inbound.AddCustomerMessageCommand, dialogID string) error

	// CloseSession sends a DELETE request to /dialog/api/conversation/v1.0/{externalId}/{dialogId}.
	CloseSession(ctx context.Context, cmd inbound.CloseConversationCommand, dialogID string) error

	// ListSessions retrieves recent activity sessions for an external ID from /dialogSession/v1.0/_fromFilter.
	ListSessions(ctx context.Context, cmd inbound.ListSessionsCommand) ([]model.RecentActivity, error)

	// GetSessionRecording retrieves past XML dialog recording data from /dialogSession/v1.0/_withRecordingData.
	GetSessionRecording(ctx context.Context, cmd inbound.RestoreConversationCommand) ([]model.Message, error)
}
