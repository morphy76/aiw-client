package aiw

import (
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"

	outboundAdapters "github.com/morphy76/aiw-client/internal/conversational/adapters/outbound"
	appService "github.com/morphy76/aiw-client/internal/conversational/application/service"
)

// HTTPClient defines the standard interface for executing HTTP requests.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// ConversationalServiceBuilder constructs and wires up ConversationalService instances with their dependencies.
type ConversationalServiceBuilder struct {
	httpClient HTTPClient
	baseURL    string
	logger     zerolog.Logger
	timeout    time.Duration
}

// NewConversationalServiceBuilder creates a new ConversationalServiceBuilder initialized with default configurations.
func NewConversationalServiceBuilder() *ConversationalServiceBuilder {
	return &ConversationalServiceBuilder{
		httpClient: http.DefaultClient,
		logger:     zerolog.Nop(),
		timeout:    30 * time.Second,
	}
}

// WithHTTPClient sets the HTTP client dependency.
func (b *ConversationalServiceBuilder) WithHTTPClient(client HTTPClient) *ConversationalServiceBuilder {
	if client != nil {
		b.httpClient = client
	}
	return b
}

// WithBaseURL sets the default base endpoint URL.
func (b *ConversationalServiceBuilder) WithBaseURL(baseURL string) *ConversationalServiceBuilder {
	b.baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return b
}

// WithLogger sets the structured logger dependency.
func (b *ConversationalServiceBuilder) WithLogger(logger zerolog.Logger) *ConversationalServiceBuilder {
	b.logger = logger
	return b
}

// WithTimeout sets the default operation timeout.
func (b *ConversationalServiceBuilder) WithTimeout(timeout time.Duration) *ConversationalServiceBuilder {
	if timeout > 0 {
		b.timeout = timeout
	}
	return b
}

// Build wires up internal components and returns a ready-to-use ConversationalService.
func (b *ConversationalServiceBuilder) Build() (ConversationalService, error) {
	repo := outboundAdapters.NewInMemoryConversationRepository()
	gateway := outboundAdapters.NewHTTPSSEGateway(b.httpClient, b.baseURL)

	useCase := appService.NewConversationalService(repo, gateway)
	adapter := newConversationalServiceAdapter(useCase)

	return adapter, nil
}

