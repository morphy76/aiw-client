package aiw

import (
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/morphy76/aiw-client/internal/conversational/application/ports/inbound"
	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
)

type callbackHolder struct {
	onOpen            OnOpenFn
	onError           OnErrorFn
	onCustomerMessage OnCustomerMessageFn
	onBotMessage      OnBotMessageFn
	onClose           OnCloseFn
	closeOnce         sync.Once
}

// conversationalServiceAdapter adapts inbound.ConversationalUseCase to ConversationalService.
type conversationalServiceAdapter struct {
	useCase   inbound.ConversationalUseCase
	mu        sync.RWMutex
	callbacks map[string]*callbackHolder
}

// newConversationalServiceAdapter constructs a new conversationalServiceAdapter.
func newConversationalServiceAdapter(useCase inbound.ConversationalUseCase) *conversationalServiceAdapter {
	return &conversationalServiceAdapter{
		useCase:   useCase,
		callbacks: make(map[string]*callbackHolder),
	}
}

type sseStreamAdapterHandler struct {
	adapter *conversationalServiceAdapter
	ctx     ConversationalContext
	holder  *callbackHolder
}

func (h *sseStreamAdapterHandler) OnCreated(dialogID string) error {
	h.ctx.SetDialogID(dialogID)
	if h.holder != nil && h.holder.onOpen != nil {
		if err := h.adapter.safeInvokeOpen(h.ctx, h.holder.onOpen); err != nil {
			return err
		}
	}
	return nil
}

func (h *sseStreamAdapterHandler) OnCustomerMessage(text string) error {
	if h.holder != nil && h.holder.onCustomerMessage != nil {
		return h.adapter.safeInvokeCustomerMessage(h.ctx, h.holder.onCustomerMessage, text)
	}
	return nil
}

func (h *sseStreamAdapterHandler) OnBotMessage(text string) error {
	if h.holder != nil && h.holder.onBotMessage != nil {
		return h.adapter.safeInvokeBotMessage(h.ctx, h.holder.onBotMessage, text)
	}
	return nil
}

func (h *sseStreamAdapterHandler) OnAborted(_ string) {
	if h.holder != nil {
		if h.holder.onError != nil {
			h.adapter.safeInvokeError(h.ctx, h.holder.onError, model.ErrConversationAborted)
		}
		_ = h.adapter.safeInvokeClose(h.ctx, h.holder)
	}
}

func (h *sseStreamAdapterHandler) OnClosed() {
	if h.holder != nil {
		_ = h.adapter.safeInvokeClose(h.ctx, h.holder)
	}
}

func (h *sseStreamAdapterHandler) OnError(err error) {
	if h.holder != nil && h.holder.onError != nil {
		h.adapter.safeInvokeError(h.ctx, h.holder.onError, err)
	}
}

// OpenConversation initiates a conversational session, sets DialogID, and executes lifecycle callbacks.
func (a *conversationalServiceAdapter) OpenConversation(
	ctx ConversationalContext,
	onOpenFn OnOpenFn,
	onErrorFn OnErrorFn,
	onCustomerMessageFn OnCustomerMessageFn,
	onBotMessageFn OnBotMessageFn,
	onCloseFn OnCloseFn,
) error {
	start := time.Now()
	log := zerolog.Ctx(ctx).With().
		Str("op", "OpenConversation").
		Str("external_id", ctx.ExternalID()).
		Str("tenant", ctx.Tenant()).
		Str("dialog_model", ctx.DialogModel()).
		Logger()
	log.Debug().Msg("starting OpenConversation")

	if ctx.ExternalID() == "" {
		err := model.ErrInvalidExternalID
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("invalid external ID")
		if onErrorFn != nil {
			a.safeInvokeError(ctx, onErrorFn, err)
		}
		return err
	}

	holder := &callbackHolder{
		onOpen:            onOpenFn,
		onError:           onErrorFn,
		onCustomerMessage: onCustomerMessageFn,
		onBotMessage:      onBotMessageFn,
		onClose:           onCloseFn,
	}
	a.mu.Lock()
	a.callbacks[ctx.ExternalID()] = holder
	a.mu.Unlock()

	streamHandler := &sseStreamAdapterHandler{
		adapter: a,
		ctx:     ctx,
		holder:  holder,
	}

	conv, err := a.useCase.OpenConversation(ctx, inbound.OpenConversationCommand{
		ExternalID:  ctx.ExternalID(),
		Tenant:      ctx.Tenant(),
		DialogModel: ctx.DialogModel(),
		BearerToken: ctx.BearerToken(),
		Sandbox:     ctx.Sandbox(),
		BaseURL:     ctx.BaseURL(),
		Headers:     ctx.Headers(),
	}, streamHandler)
	if err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to open conversation")
		if onErrorFn != nil {
			a.safeInvokeError(ctx, onErrorFn, err)
		}
		return err
	}

	ctx.SetDialogID(conv.DialogID())

	log.Info().
		Str("dialog_id", conv.DialogID()).
		Dur("duration_ms", time.Since(start)).
		Msg("completed OpenConversation")
	return nil
}

// AddCustomerMessage appends a customer message to the conversation and dispatches it.
func (a *conversationalServiceAdapter) AddCustomerMessage(ctx ConversationalContext, mex string) error {
	start := time.Now()
	log := zerolog.Ctx(ctx).With().
		Str("op", "AddCustomerMessage").
		Str("external_id", ctx.ExternalID()).
		Str("tenant", ctx.Tenant()).
		Logger()
	log.Debug().Msg("starting AddCustomerMessage")

	a.mu.RLock()
	holder := a.callbacks[ctx.ExternalID()]
	a.mu.RUnlock()

	if ctx.ExternalID() == "" {
		err := model.ErrInvalidExternalID
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("missing external ID")
		if holder != nil && holder.onError != nil {
			a.safeInvokeError(ctx, holder.onError, err)
		}
		return err
	}

	_, err := a.useCase.AddCustomerMessage(ctx, inbound.AddCustomerMessageCommand{
		ExternalID:  ctx.ExternalID(),
		Message:     mex,
		Tenant:      ctx.Tenant(),
		BearerToken: ctx.BearerToken(),
		Sandbox:     ctx.Sandbox(),
		BaseURL:     ctx.BaseURL(),
		Headers:     ctx.Headers(),
	})
	if err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to add customer message")
		if holder != nil && holder.onError != nil {
			a.safeInvokeError(ctx, holder.onError, err)
		}
		return err
	}

	log.Info().
		Str("dialog_id", ctx.DialogID()).
		Dur("duration_ms", time.Since(start)).
		Msg("completed AddCustomerMessage")
	return nil
}

// CloseConversation terminates a conversation and invokes onCloseFn if registered.
func (a *conversationalServiceAdapter) CloseConversation(ctx ConversationalContext) error {
	start := time.Now()
	log := zerolog.Ctx(ctx).With().
		Str("op", "CloseConversation").
		Str("external_id", ctx.ExternalID()).
		Str("tenant", ctx.Tenant()).
		Logger()
	log.Debug().Msg("starting CloseConversation")

	a.mu.Lock()
	holder := a.callbacks[ctx.ExternalID()]
	delete(a.callbacks, ctx.ExternalID())
	a.mu.Unlock()

	if err := a.useCase.CloseConversation(ctx, inbound.CloseConversationCommand{
		ExternalID:  ctx.ExternalID(),
		Tenant:      ctx.Tenant(),
		BearerToken: ctx.BearerToken(),
		Sandbox:     ctx.Sandbox(),
		BaseURL:     ctx.BaseURL(),
		Headers:     ctx.Headers(),
	}); err != nil {
		log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("failed to close conversation")
		if holder != nil && holder.onError != nil {
			a.safeInvokeError(ctx, holder.onError, err)
		}
		return err
	}

	if holder != nil {
		if err := a.safeInvokeClose(ctx, holder); err != nil {
			log.Error().Err(err).Dur("duration_ms", time.Since(start)).Msg("onClose callback error")
			return err
		}
	}

	log.Info().
		Str("dialog_id", ctx.DialogID()).
		Dur("duration_ms", time.Since(start)).
		Msg("completed CloseConversation")
	return nil
}

func (a *conversationalServiceAdapter) safeInvokeOpen(ctx ConversationalContext, fn OnOpenFn) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in OnOpenFn callback: %v", r)
		}
	}()
	return fn(ctx)
}

func (a *conversationalServiceAdapter) safeInvokeError(ctx ConversationalContext, fn OnErrorFn, err error) {
	defer func() {
		_ = recover()
	}()
	fn(ctx, err)
}

func (a *conversationalServiceAdapter) safeInvokeCustomerMessage(ctx ConversationalContext, fn OnCustomerMessageFn, mex string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in OnCustomerMessageFn callback: %v", r)
		}
	}()
	return fn(ctx, mex)
}

func (a *conversationalServiceAdapter) safeInvokeBotMessage(ctx ConversationalContext, fn OnBotMessageFn, mex string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in OnBotMessageFn callback: %v", r)
		}
	}()
	return fn(ctx, mex)
}

func (a *conversationalServiceAdapter) safeInvokeClose(ctx ConversationalContext, holder *callbackHolder) (err error) {
	if holder == nil || holder.onClose == nil {
		return nil
	}
	holder.closeOnce.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic in OnCloseFn callback: %v", r)
			}
		}()
		err = holder.onClose(ctx)
	})
	return err
}
