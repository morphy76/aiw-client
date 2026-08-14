package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/morphy76/aiw-client/internal/conversational/application/ports/inbound"
	"github.com/morphy76/aiw-client/internal/conversational/application/ports/outbound"
	"github.com/morphy76/aiw-client/internal/conversational/application/service"
	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockConversationRepository mocks outbound.ConversationRepository.
type MockConversationRepository struct {
	mock.Mock
}

func (m *MockConversationRepository) Save(ctx context.Context, conv *model.Conversation) error {
	args := m.Called(ctx, conv)
	return args.Error(0)
}

func (m *MockConversationRepository) FindByExternalID(ctx context.Context, externalID string) (*model.Conversation, error) {
	args := m.Called(ctx, externalID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Conversation), args.Error(1)
}

func (m *MockConversationRepository) FindByDialogID(ctx context.Context, dialogID string) (*model.Conversation, error) {
	args := m.Called(ctx, dialogID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*model.Conversation), args.Error(1)
}

func (m *MockConversationRepository) Delete(ctx context.Context, externalID string) error {
	args := m.Called(ctx, externalID)
	return args.Error(0)
}

// MockAIWGateway mocks outbound.AIWGateway.
type MockAIWGateway struct {
	mock.Mock
}

func (m *MockAIWGateway) OpenSessionStream(ctx context.Context, cmd inbound.OpenConversationCommand, handler outbound.StreamEventHandler) error {
	args := m.Called(ctx, cmd, handler)
	if args.Error(0) == nil && handler != nil {
		_ = handler.OnCreated("dlg-555")
	}
	return args.Error(0)
}

func (m *MockAIWGateway) SendCustomerMessage(ctx context.Context, cmd inbound.AddCustomerMessageCommand, dialogID string) error {
	args := m.Called(ctx, cmd, dialogID)
	return args.Error(0)
}

func (m *MockAIWGateway) CloseSession(ctx context.Context, cmd inbound.CloseConversationCommand, dialogID string) error {
	args := m.Called(ctx, cmd, dialogID)
	return args.Error(0)
}

func TestConversationalService_OpenConversation(t *testing.T) {
	t.Run("successfully opens a new conversation", func(t *testing.T) {
		repo := new(MockConversationRepository)
		gateway := new(MockAIWGateway)
		svc := service.NewConversationalService(repo, gateway)

		ctx := context.Background()
		extID := "ext-100"
		cmd := inbound.OpenConversationCommand{ExternalID: extID}

		repo.On("FindByExternalID", ctx, extID).Return(nil, model.ErrConversationNotFound)
		gateway.On("OpenSessionStream", ctx, cmd, mock.Anything).Return(nil)
		repo.On("Save", ctx, mock.MatchedBy(func(c *model.Conversation) bool {
			return c.ExternalID() == extID && c.DialogID() == "dlg-555" && c.IsActive()
		})).Return(nil)

		conv, err := svc.OpenConversation(ctx, cmd, nil)
		require.NoError(t, err)
		require.NotNil(t, conv)
		assert.Equal(t, extID, conv.ExternalID())
		assert.Equal(t, "dlg-555", conv.DialogID())
		assert.True(t, conv.IsActive())

		repo.AssertExpectations(t)
		gateway.AssertExpectations(t)
	})

	t.Run("fails when gateway returns error", func(t *testing.T) {
		repo := new(MockConversationRepository)
		gateway := new(MockAIWGateway)
		svc := service.NewConversationalService(repo, gateway)

		ctx := context.Background()
		extID := "ext-100"
		cmd := inbound.OpenConversationCommand{ExternalID: extID}

		repo.On("FindByExternalID", ctx, extID).Return(nil, model.ErrConversationNotFound)
		gateway.On("OpenSessionStream", ctx, cmd, mock.Anything).Return(errors.New("network failure"))

		conv, err := svc.OpenConversation(ctx, cmd, nil)
		assert.Error(t, err)
		assert.Nil(t, conv)
		assert.Contains(t, err.Error(), "network failure")

		repo.AssertExpectations(t)
		gateway.AssertExpectations(t)
	})

	t.Run("fails when conversation is already active", func(t *testing.T) {
		repo := new(MockConversationRepository)
		gateway := new(MockAIWGateway)
		svc := service.NewConversationalService(repo, gateway)

		ctx := context.Background()
		extID := "ext-100"
		cmd := inbound.OpenConversationCommand{ExternalID: extID}

		existingConv, err := model.NewConversation(extID)
		require.NoError(t, err)
		require.NoError(t, existingConv.Open("dlg-existing"))

		repo.On("FindByExternalID", ctx, extID).Return(existingConv, nil)

		conv, err := svc.OpenConversation(ctx, cmd, nil)
		assert.ErrorIs(t, err, model.ErrConversationAlreadyOpen)
		assert.Nil(t, conv)

		repo.AssertExpectations(t)
		gateway.AssertExpectations(t)
	})
}

func TestConversationalService_AddCustomerMessage(t *testing.T) {
	t.Run("successfully adds customer message", func(t *testing.T) {
		repo := new(MockConversationRepository)
		gateway := new(MockAIWGateway)
		svc := service.NewConversationalService(repo, gateway)

		ctx := context.Background()
		extID := "ext-100"
		dialogID := "dlg-555"
		cmd := inbound.AddCustomerMessageCommand{
			ExternalID: extID,
			Message:    "Hello assistant",
		}

		activeConv, err := model.NewConversation(extID)
		require.NoError(t, err)
		require.NoError(t, activeConv.Open(dialogID))

		repo.On("FindByExternalID", ctx, extID).Return(activeConv, nil)
		gateway.On("SendCustomerMessage", ctx, cmd, dialogID).Return(nil)
		repo.On("Save", ctx, mock.Anything).Return(nil)

		conv, err := svc.AddCustomerMessage(ctx, cmd)
		require.NoError(t, err)
		require.NotNil(t, conv)

		messages := conv.Messages()
		require.Len(t, messages, 1)
		assert.Equal(t, "Hello assistant", messages[0].Content())

		repo.AssertExpectations(t)
		gateway.AssertExpectations(t)
	})

	t.Run("fails if conversation not found", func(t *testing.T) {
		repo := new(MockConversationRepository)
		gateway := new(MockAIWGateway)
		svc := service.NewConversationalService(repo, gateway)

		ctx := context.Background()
		repo.On("FindByExternalID", ctx, "unknown").Return(nil, model.ErrConversationNotFound)

		conv, err := svc.AddCustomerMessage(ctx, inbound.AddCustomerMessageCommand{
			ExternalID: "unknown",
			Message:    "Hello",
		})
		assert.ErrorIs(t, err, model.ErrConversationNotFound)
		assert.Nil(t, conv)
	})
}

func TestConversationalService_CloseConversation(t *testing.T) {
	t.Run("successfully closes conversation", func(t *testing.T) {
		repo := new(MockConversationRepository)
		gateway := new(MockAIWGateway)
		svc := service.NewConversationalService(repo, gateway)

		ctx := context.Background()
		extID := "ext-100"
		dialogID := "dlg-555"
		cmd := inbound.CloseConversationCommand{ExternalID: extID}

		activeConv, err := model.NewConversation(extID)
		require.NoError(t, err)
		require.NoError(t, activeConv.Open(dialogID))

		repo.On("FindByExternalID", ctx, extID).Return(activeConv, nil)
		gateway.On("CloseSession", ctx, cmd, dialogID).Return(nil)
		repo.On("Save", ctx, mock.Anything).Return(nil)

		err = svc.CloseConversation(ctx, cmd)
		require.NoError(t, err)
		assert.Equal(t, model.StateClosed, activeConv.State())

		repo.AssertExpectations(t)
		gateway.AssertExpectations(t)
	})
}
