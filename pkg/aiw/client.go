package aiw

import (
	"time"

	"github.com/rs/zerolog"
)

// Client is the main AIW facade interface exposing conversational and platform services.
type Client interface {
	// Conversational returns the conversational service.
	Conversational() ConversationalService

	// Close terminates any active sessions and releases client resources.
	Close() error
}

// clientFacade is the concrete implementation of the Client facade.
type clientFacade struct {
	logger  zerolog.Logger
	timeout time.Duration
	convSvc ConversationalService
}

// New instantiates a new AIW Client facade configured with the provided options.
func New(opts ...Option) (Client, error) {
	builder := NewClientBuilder()
	cfg := Config{
		Logger:     builder.logger,
		Timeout:    builder.timeout,
		HTTPClient: builder.httpClient,
		BaseURL:    builder.baseURL,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return builder.
		WithLogger(cfg.Logger).
		WithTimeout(cfg.Timeout).
		WithConversationalService(cfg.ConversationalService).
		WithHTTPClient(cfg.HTTPClient).
		WithBaseURL(cfg.BaseURL).
		Build()
}

// Conversational returns the conversational service.
func (c *clientFacade) Conversational() ConversationalService {
	return c.convSvc
}

// Close gracefully terminates all client background operations and releases resources.
func (c *clientFacade) Close() error {
	start := time.Now()
	c.logger.Debug().Msg("closing AIW client facade")
	c.logger.Info().Dur("duration_ms", time.Since(start)).Msg("AIW client facade closed")
	return nil
}
