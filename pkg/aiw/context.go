package aiw

import (
	"context"
	"fmt"
	"sync"
)

// ConversationalContext extends standard context.Context with conversational domain metadata
// and session configuration parameters (tenant, dialog model, authorization, sandbox mode, headers).
type ConversationalContext interface {
	context.Context

	// ExternalID returns the unique business identifier for the customer / external entity.
	ExternalID() string

	// DialogID returns the active dialog session identifier, or empty string if not yet opened.
	DialogID() string

	// SetDialogID updates the dialog session identifier in a thread-safe manner.
	SetDialogID(dialogID string)

	// WithDialogID returns a new ConversationalContext instance with the provided dialog ID while preserving configs.
	WithDialogID(dialogID string) ConversationalContext

	// Tenant returns the tenant namespace (default: "default").
	Tenant() string

	// DialogModel returns the target dialog model name (e.g., "RocchettoEmbeddingsV2").
	DialogModel() string

	// BearerToken returns the PAT / bearer token for authentication.
	BearerToken() string

	// Sandbox reports whether the session operates in sandbox mode.
	Sandbox() bool

	// BaseURL returns the custom base URL if configured.
	BaseURL() string

	// Headers returns a copy of additional HTTP headers configured for the session.
	Headers() map[string]string

	// CognitiveSystemHeader returns the formatted cognitive system header value ("live:<tenant>").
	CognitiveSystemHeader() string
}

// conversationalContext implements ConversationalContext.
type conversationalContext struct {
	context.Context
	externalID  string
	mu          sync.RWMutex
	dialogID    string
	tenant      string
	dialogModel string
	bearerToken string
	sandbox     bool
	baseURL     string
	headers     map[string]string
}

// NewConversationalContext creates a new ConversationalContext wrapping parent context.Context.
// If parent is nil, context.Background() is used as the base context.
func NewConversationalContext(parent context.Context, externalID string) ConversationalContext {
	if parent == nil {
		parent = context.Background()
	}
	return &conversationalContext{
		Context:    parent,
		externalID: externalID,
		tenant:     "default",
		headers:    make(map[string]string),
	}
}

// ExternalID returns the external identifier.
func (c *conversationalContext) ExternalID() string {
	return c.externalID
}

// DialogID returns the active dialog identifier.
func (c *conversationalContext) DialogID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.dialogID
}

// SetDialogID sets the dialog identifier thread-safely.
func (c *conversationalContext) SetDialogID(dialogID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dialogID = dialogID
}

// WithDialogID returns a new ConversationalContext instance initialized with the given dialogID.
func (c *conversationalContext) WithDialogID(dialogID string) ConversationalContext {
	c.mu.RLock()
	headersCopy := make(map[string]string, len(c.headers))
	for k, v := range c.headers {
		headersCopy[k] = v
	}
	c.mu.RUnlock()

	return &conversationalContext{
		Context:     c.Context,
		externalID:  c.externalID,
		dialogID:    dialogID,
		tenant:      c.tenant,
		dialogModel: c.dialogModel,
		bearerToken: c.bearerToken,
		sandbox:     c.sandbox,
		baseURL:     c.baseURL,
		headers:     headersCopy,
	}
}

// Tenant returns the tenant namespace.
func (c *conversationalContext) Tenant() string {
	if c.tenant == "" {
		return "default"
	}
	return c.tenant
}

// DialogModel returns the dialog model name.
func (c *conversationalContext) DialogModel() string {
	return c.dialogModel
}

// BearerToken returns the PAT / bearer token.
func (c *conversationalContext) BearerToken() string {
	return c.bearerToken
}

// Sandbox returns whether sandbox mode is enabled.
func (c *conversationalContext) Sandbox() bool {
	return c.sandbox
}

// BaseURL returns the base URL.
func (c *conversationalContext) BaseURL() string {
	return c.baseURL
}

// Headers returns a copy of custom HTTP headers.
func (c *conversationalContext) Headers() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.headers == nil {
		return make(map[string]string)
	}
	copyMap := make(map[string]string, len(c.headers))
	for k, v := range c.headers {
		copyMap[k] = v
	}
	return copyMap
}

// CognitiveSystemHeader returns the x-cognitive-system formatted header ("live:<tenant>").
func (c *conversationalContext) CognitiveSystemHeader() string {
	tenant := c.Tenant()
	return fmt.Sprintf("live:%s", tenant)
}
