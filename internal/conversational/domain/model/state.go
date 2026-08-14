package model

// State represents the lifecycle state of a conversation.
type State string

const (
	// StateIdle indicates the conversation has been initialized but not yet opened.
	StateIdle State = "IDLE"

	// StateActive indicates the conversation is actively open and exchanging messages.
	StateActive State = "ACTIVE"

	// StateClosed indicates the conversation has been closed and cannot accept further messages.
	StateClosed State = "CLOSED"
)

// String returns the string representation of the conversation state.
func (s State) String() string {
	return string(s)
}
