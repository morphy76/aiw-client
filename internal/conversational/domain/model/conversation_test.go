package model_test

import (
	"testing"
	"time"

	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConversation(t *testing.T) {
	t.Run("valid external ID creates conversation in idle state", func(t *testing.T) {
		conv, err := model.NewConversation("ext-100")
		require.NoError(t, err)
		require.NotNil(t, conv)

		assert.Equal(t, "ext-100", conv.ExternalID())
		assert.Empty(t, conv.DialogID())
		assert.Equal(t, model.StateIdle, conv.State())
		assert.Empty(t, conv.Messages())
		assert.False(t, conv.CreatedAt().IsZero())
	})

	t.Run("empty external ID returns error", func(t *testing.T) {
		conv, err := model.NewConversation("")
		assert.Nil(t, conv)
		assert.ErrorIs(t, err, model.ErrInvalidExternalID)
	})
}

func TestConversation_Open(t *testing.T) {
	t.Run("open transitions from idle to active", func(t *testing.T) {
		conv, err := model.NewConversation("ext-100")
		require.NoError(t, err)

		err = conv.Open("dlg-200")
		require.NoError(t, err)

		assert.Equal(t, "dlg-200", conv.DialogID())
		assert.Equal(t, model.StateActive, conv.State())
		assert.True(t, conv.IsActive())
	})

	t.Run("open with empty dialog ID fails", func(t *testing.T) {
		conv, err := model.NewConversation("ext-100")
		require.NoError(t, err)

		err = conv.Open("")
		assert.ErrorIs(t, err, model.ErrInvalidDialogID)
		assert.Equal(t, model.StateIdle, conv.State())
	})

	t.Run("open when already active fails", func(t *testing.T) {
		conv, err := model.NewConversation("ext-100")
		require.NoError(t, err)
		require.NoError(t, conv.Open("dlg-200"))

		err = conv.Open("dlg-300")
		assert.ErrorIs(t, err, model.ErrConversationAlreadyOpen)
	})

	t.Run("open when closed fails", func(t *testing.T) {
		conv, err := model.NewConversation("ext-100")
		require.NoError(t, err)
		require.NoError(t, conv.Open("dlg-200"))
		require.NoError(t, conv.Close())

		err = conv.Open("dlg-400")
		assert.ErrorIs(t, err, model.ErrConversationClosed)
	})
}

func TestConversation_AddCustomerMessage(t *testing.T) {
	t.Run("add customer message when active succeeds", func(t *testing.T) {
		conv, err := model.NewConversation("ext-100")
		require.NoError(t, err)
		require.NoError(t, conv.Open("dlg-200"))

		msg, err := model.NewMessage("msg-1", model.SenderCustomer, "Hello assistant", time.Now())
		require.NoError(t, err)

		err = conv.AddMessage(msg)
		require.NoError(t, err)

		messages := conv.Messages()
		require.Len(t, messages, 1)
		assert.Equal(t, "Hello assistant", messages[0].Content())
		assert.Equal(t, model.SenderCustomer, messages[0].Sender())
	})

	t.Run("add message when idle fails", func(t *testing.T) {
		conv, err := model.NewConversation("ext-100")
		require.NoError(t, err)

		msg, err := model.NewMessage("msg-1", model.SenderCustomer, "Hello", time.Now())
		require.NoError(t, err)

		err = conv.AddMessage(msg)
		assert.ErrorIs(t, err, model.ErrConversationNotActive)
	})

	t.Run("add message when closed fails", func(t *testing.T) {
		conv, err := model.NewConversation("ext-100")
		require.NoError(t, err)
		require.NoError(t, conv.Open("dlg-200"))
		require.NoError(t, conv.Close())

		msg, err := model.NewMessage("msg-1", model.SenderCustomer, "Hello", time.Now())
		require.NoError(t, err)

		err = conv.AddMessage(msg)
		assert.ErrorIs(t, err, model.ErrConversationClosed)
	})
}

func TestConversation_Close(t *testing.T) {
	t.Run("close when active succeeds", func(t *testing.T) {
		conv, err := model.NewConversation("ext-100")
		require.NoError(t, err)
		require.NoError(t, conv.Open("dlg-200"))

		err = conv.Close()
		require.NoError(t, err)
		assert.Equal(t, model.StateClosed, conv.State())
		assert.False(t, conv.IsActive())
	})

	t.Run("close when already closed fails", func(t *testing.T) {
		conv, err := model.NewConversation("ext-100")
		require.NoError(t, err)
		require.NoError(t, conv.Open("dlg-200"))
		require.NoError(t, conv.Close())

		err = conv.Close()
		assert.ErrorIs(t, err, model.ErrConversationClosed)
	})
}
