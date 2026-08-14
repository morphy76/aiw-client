package aiw

import (
	"time"

	"github.com/rs/zerolog"
)

// Config encapsulates configuration parameters for the AIW client facade.
type Config struct {
	Logger                zerolog.Logger
	Timeout               time.Duration
	ConversationalService ConversationalService
}

// Option defines a functional configuration option for the AIW Client.
type Option func(*Config)

// WithLogger sets the structured logger for the client.
func WithLogger(logger zerolog.Logger) Option {
	return func(c *Config) {
		c.Logger = logger
	}
}

// WithTimeout sets the default request/session timeout for client operations.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.Timeout = timeout
	}
}

// WithConversationalService injects a pre-built or custom ConversationalService into the client facade.
func WithConversationalService(svc ConversationalService) Option {
	return func(c *Config) {
		c.ConversationalService = svc
	}
}
