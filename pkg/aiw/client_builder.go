package aiw

import (
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// ClientBuilder constructs and wires up the AIW Client facade and its underlying services.
type ClientBuilder struct {
	logger      zerolog.Logger
	timeout     time.Duration
	convSvc     ConversationalService
	convBuilder *ConversationalServiceBuilder
	httpClient  HTTPClient
	baseURL     string
	useMock     bool
}

// NewClientBuilder creates a new ClientBuilder initialized with sensible defaults.
func NewClientBuilder() *ClientBuilder {
	return &ClientBuilder{
		logger:  zerolog.Nop(),
		timeout: 30 * time.Second,
	}
}

// NewBuilder creates a new ClientBuilder (convenience alias for NewClientBuilder).
func NewBuilder() *ClientBuilder {
	return NewClientBuilder()
}

// WithConversationalService injects a pre-constructed or custom ConversationalService directly.
func (b *ClientBuilder) WithConversationalService(svc ConversationalService) *ClientBuilder {
	b.convSvc = svc
	return b
}

// WithConversationalServiceBuilder injects and configures a ConversationalServiceBuilder.
func (b *ClientBuilder) WithConversationalServiceBuilder(builder *ConversationalServiceBuilder) *ClientBuilder {
	b.convBuilder = builder
	return b
}

// WithLogger sets the structured logger for the client facade.
func (b *ClientBuilder) WithLogger(logger zerolog.Logger) *ClientBuilder {
	b.logger = logger
	return b
}

// WithTimeout sets the default timeout for client operations.
func (b *ClientBuilder) WithTimeout(timeout time.Duration) *ClientBuilder {
	if timeout > 0 {
		b.timeout = timeout
	}
	return b
}

// WithHTTPClient sets the HTTP transport client used when constructing conversational services.
func (b *ClientBuilder) WithHTTPClient(client HTTPClient) *ClientBuilder {
	if client != nil {
		b.httpClient = client
	}
	return b
}

// WithBaseURL sets the base API endpoint URL for conversational services.
func (b *ClientBuilder) WithBaseURL(baseURL string) *ClientBuilder {
	b.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return b
}

// WithInMemoryGateway configures the client to construct in-memory conversational services for testing.
func (b *ClientBuilder) WithInMemoryGateway() *ClientBuilder {
	b.useMock = true
	return b
}

// Build validates configurations, instantiates any unprovided services, and returns an initialized Client facade.
func (b *ClientBuilder) Build() (Client, error) {
	convSvc := b.convSvc
	if convSvc == nil {
		if b.convBuilder != nil {
			var err error
			convSvc, err = b.convBuilder.Build()
			if err != nil {
				return nil, err
			}
		} else {
			builder := NewConversationalServiceBuilder().
				WithLogger(b.logger).
				WithTimeout(b.timeout)

			if b.httpClient != nil {
				builder.WithHTTPClient(b.httpClient)
			}
			if b.baseURL != "" {
				builder.WithBaseURL(b.baseURL)
			}
			if b.useMock {
				builder.WithInMemoryGateway()
			}

			var err error
			convSvc, err = builder.Build()
			if err != nil {
				return nil, err
			}
		}
	}

	return &clientFacade{
		logger:  b.logger,
		timeout: b.timeout,
		convSvc: convSvc,
	}, nil
}
