package aiw_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/morphy76/aiw-client/pkg/aiw"
)

func TestConversationalServiceBuilder_BuildDefault(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	clientMock := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Accept") == "text/event-stream" {
			go func() {
				_, _ = fmt.Fprint(pw, "data: {\"lifecycle\":{\"event\":\"created\",\"dialog_id\":\"diag-builder-default\"}}\n\n")
			}()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       pr,
			}, nil
		}
		return &http.Response{StatusCode: http.StatusOK}, nil
	})

	svc, err := aiw.NewConversationalServiceBuilder().
		WithHTTPClient(clientMock).
		Build()
	require.NoError(t, err)
	require.NotNil(t, svc)

	ctx := aiw.NewConversationalContext(context.Background(), "user-builder-test")
	err = svc.OpenConversation(ctx, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "diag-builder-default", ctx.DialogID())

	err = svc.AddCustomerMessage(ctx, "Hello from builder test")
	require.NoError(t, err)
}

func TestConversationalServiceBuilder_WithCustomDependencies(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	clientMock := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Accept") == "text/event-stream" {
			go func() {
				_, _ = fmt.Fprint(pw, "data: {\"lifecycle\":{\"event\":\"created\",\"dialog_id\":\"diag-builder-101\"}}\n\n")
			}()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       pr,
			}, nil
		}
		return &http.Response{StatusCode: http.StatusOK}, nil
	})

	logger := zerolog.Nop()

	svc, err := aiw.NewConversationalServiceBuilder().
		WithHTTPClient(clientMock).
		WithBaseURL("https://dev.lab.aiwave.io").
		WithLogger(logger).
		WithTimeout(15 * time.Second).
		Build()

	require.NoError(t, err)
	require.NotNil(t, svc)

	// Inject pre-configured ConversationalService into the Client facade
	client, err := aiw.New(
		aiw.WithConversationalService(svc),
	)
	require.NoError(t, err)
	require.NotNil(t, client)
	defer func() { _ = client.Close() }()

	// Verify that the facade returns the exact injected service instance
	assert.Same(t, svc, client.Conversational())

	// Test functional flow through the facade
	convCtx := aiw.NewConversationalContext(context.Background(), "user-facade-injected")
	err = client.Conversational().OpenConversation(convCtx, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "diag-builder-101", convCtx.DialogID())
}
