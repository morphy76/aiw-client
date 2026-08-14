package outbound_test

import (
	"context"
	"testing"

	"github.com/morphy76/aiw-client/internal/conversational/adapters/outbound"
	"github.com/morphy76/aiw-client/internal/conversational/application/ports/inbound"
	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryAIWGateway(t *testing.T) {
	ctx := context.Background()
	gw := outbound.NewInMemoryAIWGateway()
	handler := &testStreamHandler{}

	openCmd := inbound.OpenConversationCommand{
		ExternalID: "ext-123",
	}

	// Open session stream
	err := gw.OpenSessionStream(ctx, openCmd, handler)
	require.NoError(t, err)
	dialogID := handler.createdDialogID
	assert.NotEmpty(t, dialogID)

	// Send customer message
	msgCmd := inbound.AddCustomerMessageCommand{
		ExternalID: "ext-123",
		Message:    "Hello AIW",
	}
	err = gw.SendCustomerMessage(ctx, msgCmd, dialogID)
	require.NoError(t, err)
	assert.Contains(t, handler.customerMsgs, "Hello AIW")

	// Simulate bot response
	err = gw.SimulateBotResponse(dialogID, "Hello from Bot!")
	require.NoError(t, err)
	assert.Contains(t, handler.botMsgs, "Hello from Bot!")

	// Send message with invalid dialog
	err = gw.SendCustomerMessage(ctx, msgCmd, "invalid-dialog")
	assert.ErrorIs(t, err, model.ErrConversationNotFound)

	// Close session
	closeCmd := inbound.CloseConversationCommand{
		ExternalID: "ext-123",
	}
	err = gw.CloseSession(ctx, closeCmd, dialogID)
	require.NoError(t, err)
	assert.True(t, handler.closedCalled)

	// Send message after close
	err = gw.SendCustomerMessage(ctx, msgCmd, dialogID)
	assert.ErrorIs(t, err, model.ErrConversationClosed)
}
