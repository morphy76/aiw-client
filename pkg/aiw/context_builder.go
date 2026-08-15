package aiw

import (
	"context"
	"errors"
	"strings"
)

var (
	// ErrMissingExternalID is returned when building a ConversationalContext without an external ID.
	ErrMissingExternalID = errors.New("external ID is required")
)

// ConversationalContextBuilder provides a fluent builder to construct ConversationalContext instances
// with all necessary configurations for open conversation and message exchange.
type ConversationalContextBuilder struct {
	ctx         context.Context
	externalID  string
	dialogID    string
	tenant      string
	dialogModel string
	bearerToken string
	sandbox     bool
	headers     map[string]string
}

// NewConversationalContextBuilder creates a new ConversationalContextBuilder initialized with sensible defaults.
func NewConversationalContextBuilder() *ConversationalContextBuilder {
	return &ConversationalContextBuilder{
		ctx:     context.Background(),
		tenant:  "default",
		headers: make(map[string]string),
	}
}

// WithContext sets the parent context.Context.
func (b *ConversationalContextBuilder) WithContext(ctx context.Context) *ConversationalContextBuilder {
	if ctx != nil {
		b.ctx = ctx
	}
	return b
}

// WithExternalID sets the external ID (required).
func (b *ConversationalContextBuilder) WithExternalID(externalID string) *ConversationalContextBuilder {
	b.externalID = strings.TrimSpace(externalID)
	return b
}

// WithDialogID sets an existing dialog ID (optional, typically set after conversation opening).
func (b *ConversationalContextBuilder) WithDialogID(dialogID string) *ConversationalContextBuilder {
	b.dialogID = strings.TrimSpace(dialogID)
	return b
}

// WithTenant sets the tenant namespace (e.g. "default", "customer-service").
func (b *ConversationalContextBuilder) WithTenant(tenant string) *ConversationalContextBuilder {
	trimmed := strings.TrimSpace(tenant)
	if trimmed != "" {
		b.tenant = trimmed
	}
	return b
}

// WithDialogModel sets the target dialog model (e.g., "RocchettoEmbeddingsV2", "Rocchetto").
func (b *ConversationalContextBuilder) WithDialogModel(model string) *ConversationalContextBuilder {
	b.dialogModel = strings.TrimSpace(model)
	return b
}

// WithBearerToken sets the personal access token (PAT) / bearer token.
func (b *ConversationalContextBuilder) WithBearerToken(token string) *ConversationalContextBuilder {
	b.bearerToken = strings.TrimSpace(token)
	return b
}

// WithSandbox toggles sandbox mode (x-cognitive-sandbox).
func (b *ConversationalContextBuilder) WithSandbox(sandbox bool) *ConversationalContextBuilder {
	b.sandbox = sandbox
	return b
}

// WithHeader adds a custom HTTP header.
func (b *ConversationalContextBuilder) WithHeader(key, value string) *ConversationalContextBuilder {
	if b.headers == nil {
		b.headers = make(map[string]string)
	}
	b.headers[key] = value
	return b
}

// WithHeaders merges multiple headers.
func (b *ConversationalContextBuilder) WithHeaders(headers map[string]string) *ConversationalContextBuilder {
	if b.headers == nil {
		b.headers = make(map[string]string)
	}
	for k, v := range headers {
		b.headers[k] = v
	}
	return b
}

// Build validates the builder configuration and returns a ConversationalContext instance.
func (b *ConversationalContextBuilder) Build() (ConversationalContext, error) {
	if b.externalID == "" {
		return nil, ErrMissingExternalID
	}
	if b.ctx == nil {
		b.ctx = context.Background()
	}
	if b.tenant == "" {
		b.tenant = "default"
	}

	headersCopy := make(map[string]string, len(b.headers))
	for k, v := range b.headers {
		headersCopy[k] = v
	}

	return &conversationalContext{
		Context:     b.ctx,
		externalID:  b.externalID,
		dialogID:    b.dialogID,
		tenant:      b.tenant,
		dialogModel: b.dialogModel,
		bearerToken: b.bearerToken,
		sandbox:     b.sandbox,
		headers:     headersCopy,
	}, nil
}
