package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/morphy76/aiw-client/internal/conversational/application/ports/inbound"
	"github.com/morphy76/aiw-client/internal/conversational/application/ports/outbound"
	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
)

// ConversationalService implements the inbound.ConversationalUseCase interface.
type ConversationalService struct {
	repo    outbound.ConversationRepository
	gateway outbound.AIWGateway
}

// NewConversationalService constructs a new ConversationalService instance.
func NewConversationalService(
	repo outbound.ConversationRepository,
	gateway outbound.AIWGateway,
) *ConversationalService {
	return &ConversationalService{
		repo:    repo,
		gateway: gateway,
	}
}

type streamHandlerProxy struct {
	conv    *model.Conversation
	repo    outbound.ConversationRepository
	handler inbound.StreamEventHandler
	ctx     context.Context
}

func (p *streamHandlerProxy) OnCreated(dialogID string) error {
	if err := p.conv.Open(dialogID); err != nil && !errors.Is(err, model.ErrConversationAlreadyOpen) {
		return err
	}
	_ = p.repo.Save(p.ctx, p.conv)
	if p.handler != nil {
		return p.handler.OnCreated(dialogID)
	}
	return nil
}

func (p *streamHandlerProxy) OnCustomerMessage(text string) error {
	msgID := uuid.New().String()
	if msg, err := model.NewMessage(msgID, model.SenderCustomer, text, time.Now().UTC()); err == nil {
		_ = p.conv.AddMessage(msg)
		_ = p.repo.Save(p.ctx, p.conv)
	}
	if p.handler != nil {
		return p.handler.OnCustomerMessage(text)
	}
	return nil
}

func (p *streamHandlerProxy) OnBotMessage(text string) error {
	msgID := uuid.New().String()
	if msg, err := model.NewMessage(msgID, model.SenderAgent, text, time.Now().UTC()); err == nil {
		_ = p.conv.AddMessage(msg)
		_ = p.repo.Save(p.ctx, p.conv)
	}
	if p.handler != nil {
		return p.handler.OnBotMessage(text)
	}
	return nil
}

func (p *streamHandlerProxy) OnAborted(reason string) {
	_ = p.conv.Close()
	_ = p.repo.Save(p.ctx, p.conv)
	if p.handler != nil {
		p.handler.OnAborted(reason)
	}
}

func (p *streamHandlerProxy) OnClosed() {
	_ = p.conv.Close()
	_ = p.repo.Save(p.ctx, p.conv)
	if p.handler != nil {
		p.handler.OnClosed()
	}
}

func (p *streamHandlerProxy) OnError(err error) {
	if p.handler != nil {
		p.handler.OnError(err)
	}
}

// OpenConversation initiates a conversation session for the given external ID.
func (s *ConversationalService) OpenConversation(
	ctx context.Context,
	cmd inbound.OpenConversationCommand,
	handler inbound.StreamEventHandler,
) (*model.Conversation, error) {
	start := time.Now()
	log := zerolog.Ctx(ctx).With().
		Str("op", "OpenConversation").
		Str("external_id", cmd.ExternalID).
		Logger()
	log.Debug().Msg("starting conversation opening")

	existing, err := s.repo.FindByExternalID(ctx, cmd.ExternalID)
	if err == nil && existing != nil && existing.IsActive() {
		log.Error().Err(model.ErrConversationAlreadyOpen).Dur("duration_ms", time.Since(start)).Msg("conversation already active")
		return nil, model.ErrConversationAlreadyOpen
	}

	conv, err := model.NewConversation(cmd.ExternalID)
	if err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to create conversation domain aggregate")
		return nil, err
	}

	proxy := &streamHandlerProxy{
		conv:    conv,
		repo:    s.repo,
		handler: handler,
		ctx:     ctx,
	}

	if err := s.gateway.OpenSessionStream(ctx, cmd, proxy); err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to open session via gateway")
		return nil, err
	}

	if err := s.repo.Save(ctx, conv); err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to persist conversation aggregate")
		return nil, err
	}

	log.Info().
		Str("dialog_id", conv.DialogID()).
		Dur("duration_ms", time.Since(start)).
		Msg("completed conversation opening")
	return conv, nil
}

// AddCustomerMessage appends a customer message to the active conversation and dispatches it via gateway.
func (s *ConversationalService) AddCustomerMessage(
	ctx context.Context,
	cmd inbound.AddCustomerMessageCommand,
) (*model.Conversation, error) {
	start := time.Now()
	log := zerolog.Ctx(ctx).With().
		Str("op", "AddCustomerMessage").
		Str("external_id", cmd.ExternalID).
		Logger()
	log.Debug().Msg("starting customer message dispatch")

	conv, err := s.repo.FindByExternalID(ctx, cmd.ExternalID)
	if err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to find conversation")
		return nil, err
	}
	if conv == nil {
		log.Error().Err(model.ErrConversationNotFound).Dur("duration_ms", time.Since(start)).Msg("conversation not found")
		return nil, model.ErrConversationNotFound
	}

	msgID := uuid.New().String()
	msg, err := model.NewMessage(msgID, model.SenderCustomer, cmd.Message, time.Now().UTC())
	if err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("invalid customer message")
		return nil, err
	}

	if err := conv.AddMessage(msg); err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to add message to aggregate")
		return nil, err
	}

	if err := s.gateway.SendCustomerMessage(ctx, cmd, conv.DialogID()); err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to send customer message via gateway")
		return nil, err
	}

	if err := s.repo.Save(ctx, conv); err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to save conversation aggregate")
		return nil, err
	}

	log.Info().
		Str("dialog_id", conv.DialogID()).
		Str("msg_id", msgID).
		Dur("duration_ms", time.Since(start)).
		Msg("completed customer message dispatch")
	return conv, nil
}

// CloseConversation terminates an active conversation session.
func (s *ConversationalService) CloseConversation(
	ctx context.Context,
	cmd inbound.CloseConversationCommand,
) error {
	start := time.Now()
	log := zerolog.Ctx(ctx).With().
		Str("op", "CloseConversation").
		Str("external_id", cmd.ExternalID).
		Logger()
	log.Debug().Msg("starting conversation termination")

	conv, err := s.repo.FindByExternalID(ctx, cmd.ExternalID)
	if err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to find conversation")
		return err
	}
	if conv == nil {
		log.Error().Err(model.ErrConversationNotFound).Dur("duration_ms", time.Since(start)).Msg("conversation not found")
		return model.ErrConversationNotFound
	}

	dialogID := conv.DialogID()

	if err := conv.Close(); err != nil {
		if errors.Is(err, model.ErrConversationClosed) {
			log.Info().Msg("conversation was already closed")
			return nil
		}
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to close conversation aggregate")
		return err
	}

	if err := s.gateway.CloseSession(ctx, cmd, dialogID); err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to close session via gateway")
		return err
	}

	if err := s.repo.Save(ctx, conv); err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to save closed conversation aggregate")
		return err
	}

	log.Info().
		Str("dialog_id", dialogID).
		Dur("duration_ms", time.Since(start)).
		Msg("completed conversation termination")
	return nil
}

// GetConversation retrieves an existing conversation aggregate by external ID.
func (s *ConversationalService) GetConversation(
	ctx context.Context,
	externalID string,
) (*model.Conversation, error) {
	start := time.Now()
	log := zerolog.Ctx(ctx).With().
		Str("op", "GetConversation").
		Str("external_id", externalID).
		Logger()
	log.Debug().Msg("retrieving conversation")

	conv, err := s.repo.FindByExternalID(ctx, externalID)
	if err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to query conversation")
		return nil, err
	}
	if conv == nil {
		log.Error().Err(model.ErrConversationNotFound).Dur("duration_ms", time.Since(start)).Msg("conversation not found")
		return nil, model.ErrConversationNotFound
	}

	log.Info().
		Str("dialog_id", conv.DialogID()).
		Dur("duration_ms", time.Since(start)).
		Msg("conversation retrieved successfully")
	return conv, nil
}

// ListSessions retrieves summary past sessions/activities from the gateway.
func (s *ConversationalService) ListSessions(
	ctx context.Context,
	cmd inbound.ListSessionsCommand,
) ([]model.RecentActivity, error) {
	start := time.Now()
	log := zerolog.Ctx(ctx).With().
		Str("op", "ListSessions").
		Str("external_id", cmd.ExternalID).
		Str("assistant", cmd.AssistantName).
		Logger()
	log.Debug().Msg("starting ListSessions")

	activities, err := s.gateway.ListSessions(ctx, cmd)
	if err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to list sessions via gateway")
		return nil, err
	}

	log.Info().
		Int("activities_count", len(activities)).
		Dur("duration_ms", time.Since(start)).
		Msg("completed ListSessions")
	return activities, nil
}

// RestoreConversation retrieves previous session recording history, populates aggregate, and persists state.
func (s *ConversationalService) RestoreConversation(
	ctx context.Context,
	cmd inbound.RestoreConversationCommand,
) (*model.Conversation, error) {
	start := time.Now()
	log := zerolog.Ctx(ctx).With().
		Str("op", "RestoreConversation").
		Str("external_id", cmd.ExternalID).
		Logger()
	log.Debug().Msg("starting RestoreConversation")

	messages, err := s.gateway.GetSessionRecording(ctx, cmd)
	if err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to get session recording from gateway")
		return nil, err
	}

	existing, _ := s.repo.FindByExternalID(ctx, cmd.ExternalID)
	var conv *model.Conversation
	if existing != nil {
		conv = existing
		for _, msg := range messages {
			_ = conv.AddMessage(msg)
		}
	} else {
		now := time.Now().UTC()
		conv = model.Reconstitute(cmd.ExternalID, "", model.StateIdle, messages, now, now)
	}

	if err := s.repo.Save(ctx, conv); err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to save restored conversation aggregate")
		return nil, err
	}

	log.Info().
		Int("messages_count", len(messages)).
		Dur("duration_ms", time.Since(start)).
		Msg("completed RestoreConversation")
	return conv, nil
}
