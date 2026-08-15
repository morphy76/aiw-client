package aiw

import "time"

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

// Source represents an information citation or document reference supporting an answer.
type Source struct {
	ID    string
	Title string
}

// StructuredAnswer represents a structured bot reply containing answer text and citations.
type StructuredAnswer struct {
	Text    string
	Sources []Source
}

// Attachment represents a file or content artifact attached to a customer message.
type Attachment struct {
	Filename   string
	ContentRef string
	Metadata   map[string]string
}

// MessageOptions provides optional configuration when adding customer messages.
type MessageOptions struct {
	Attachments []Attachment
}

// Message represents a message in a conversation history.
type Message struct {
	ID          string
	Sender      string
	Content     string
	Timestamp   time.Time
	Attachments []Attachment
	Answer      *StructuredAnswer
}

// RecentActivity represents a summary of a previous conversation session.
type RecentActivity struct {
	ID         int64
	ExternalID string
	Title      string
	StartTime  time.Time
}

// ListSessionsQuery defines pagination and sorting filters when querying past activities.
type ListSessionsQuery struct {
	AssistantName string
	Limit         int
	LastIDFound   int
	SortField     string
	SortOrder     string
}

// ConversationalService exposes operations to open conversations, dispatch customer messages, and manage lifecycle.
type ConversationalService interface {
	// AddCustomerMessage dispatches a message from the customer to the active conversation.
	AddCustomerMessage(ctx ConversationalContext, mex string) error

	// AddCustomerMessageWithOptions dispatches a message from the customer with attachments and options.
	AddCustomerMessageWithOptions(ctx ConversationalContext, mex string, opts MessageOptions) error

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

	// RestoreConversation retrieves past session recording data and returns restored historical messages.
	RestoreConversation(ctx ConversationalContext) ([]Message, error)

	// ListSessions retrieves past conversational sessions/activities for the user.
	ListSessions(ctx ConversationalContext, query ListSessionsQuery) ([]RecentActivity, error)
}
