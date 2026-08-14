package model

import "errors"

var (
	// ErrInvalidExternalID is returned when the external ID is empty or invalid.
	ErrInvalidExternalID = errors.New("external ID cannot be empty")

	// ErrInvalidDialogID is returned when the dialog ID is empty or invalid.
	ErrInvalidDialogID = errors.New("dialog ID cannot be empty")

	// ErrInvalidMessage is returned when message content or fields are invalid.
	ErrInvalidMessage = errors.New("message content cannot be empty")

	// ErrConversationAlreadyOpen is returned when attempting to open a conversation that is already active.
	ErrConversationAlreadyOpen = errors.New("conversation is already open")

	// ErrConversationNotActive is returned when performing an operation on a non-active conversation.
	ErrConversationNotActive = errors.New("conversation is not in active state")

	// ErrConversationClosed is returned when an operation is attempted on a closed conversation.
	ErrConversationClosed = errors.New("conversation is closed")

	// ErrConversationAborted is returned when the remote server sends an aborted lifecycle event.
	ErrConversationAborted = errors.New("conversation was aborted by remote server")

	// ErrConversationNotFound is returned when a conversation aggregate cannot be found.
	ErrConversationNotFound = errors.New("conversation not found")

	// ErrSSEConnectionFailed is returned when the SSE connection cannot be established or response is non-200.
	ErrSSEConnectionFailed = errors.New("failed to establish SSE connection")
)
