package aiw

// OnOpenFn is invoked when a conversation is successfully established and opened.
type OnOpenFn func(ctx ConversationalContext) error

// OnErrorFn is invoked when an error occurs during conversation lifecycle or message dispatching.
type OnErrorFn func(ctx ConversationalContext, err error)

// OnCustomerMessageFn is invoked when a customer message is processed or confirmed via SSE.
type OnCustomerMessageFn func(ctx ConversationalContext, mex string) error

// OnBotMessageFn is invoked when a bot / AI response is received via SSE.
type OnBotMessageFn func(ctx ConversationalContext, mex string) error

// OnCloseFn is invoked when a conversation is closed or terminated.
type OnCloseFn func(ctx ConversationalContext) error

// ConversationalService exposes operations to open conversations, dispatch customer messages, and manage lifecycle.
type ConversationalService interface {
	// AddCustomerMessage dispatches a message from the customer to the active conversation.
	AddCustomerMessage(ctx ConversationalContext, mex string) error

	// OpenConversation initiates a conversational session and registers lifecycle callbacks.
	OpenConversation(
		ctx ConversationalContext,
		onOpenFn OnOpenFn,
		onErrorFn OnErrorFn,
		onCustomerMessageFn OnCustomerMessageFn,
		onBotMessageFn OnBotMessageFn,
		onCloseFn OnCloseFn,
	) error

	// CloseConversation terminates an active conversational session.
	CloseConversation(ctx ConversationalContext) error
}
