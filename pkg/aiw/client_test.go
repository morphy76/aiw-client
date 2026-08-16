package aiw_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/morphy76/aiw-client/pkg/aiw"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestHTTPClient(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func TestClient_LifecycleAndConversationalFlow(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	clientMock := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Accept") == "text/event-stream" {
			go func() {
				_, _ = fmt.Fprint(pw, "data: {\"lifecycle\":{\"event\":\"created\",\"dialog_id\":\"diag-client-101\"}}\n\n")
			}()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       pr,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"status":"ok"}`))),
		}, nil
	})

	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	client, err := aiw.New(
		aiw.WithHTTPClient(clientMock),
		aiw.WithBaseURL("https://dev.lab.aiwave.io"),
		aiw.WithLogger(logger),
		aiw.WithTimeout(5*time.Second),
	)
	require.NoError(t, err)
	require.NotNil(t, client)
	defer func() {
		require.NoError(t, client.Close())
	}()

	convSvc := client.Conversational()
	require.NotNil(t, convSvc)

	// Create conversational context
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	convCtx, err := aiw.NewConversationalContextBuilder().
		WithContext(ctx).
		WithExternalID("cust-test-101").
		Build()
	require.NoError(t, err)
	assert.Equal(t, "cust-test-101", convCtx.ExternalID())

	var onOpenCalled int32
	var onCreatedCalled int32
	var onBotMsgCalled int32

	// Open conversation
	err = convSvc.OpenConversation(
		convCtx,
		func(c aiw.ConversationalContext) error {
			atomic.AddInt32(&onOpenCalled, 1)
			assert.Equal(t, "cust-test-101", c.ExternalID())
			return nil
		},
		func(c aiw.ConversationalContext, dialogID string) error {
			atomic.AddInt32(&onCreatedCalled, 1)
			assert.Equal(t, "cust-test-101", c.ExternalID())
			assert.Equal(t, "diag-client-101", dialogID)
			return nil
		},
		func(c aiw.ConversationalContext, err error, cancel aiw.CancelStreamFunc) {
			t.Fatalf("unexpected error callback: %v", err)
		},
		nil,
		func(c aiw.ConversationalContext, mex string) error {
			atomic.AddInt32(&onBotMsgCalled, 1)
			return nil
		},
		nil,
		func(c aiw.ConversationalContext) error {
			return nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&onOpenCalled))
	assert.Equal(t, int32(1), atomic.LoadInt32(&onCreatedCalled))
	assert.Equal(t, "diag-client-101", convCtx.DialogID())

	// Add customer message
	err = convSvc.AddCustomerMessage(convCtx, "Hello, can you help me with my account?")
	require.NoError(t, err)
}

func TestClient_ErrorHandlingInFlow(t *testing.T) {
	clientMock := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	})

	client, err := aiw.New(aiw.WithHTTPClient(clientMock))
	require.NoError(t, err)
	defer func() {
		_ = client.Close()
	}()

	convSvc := client.Conversational()

	// Empty external ID
	invalidCtx := aiw.NewConversationalContext(context.Background(), "")
	var errReported error

	err = convSvc.OpenConversation(
		invalidCtx,
		nil,
		nil,
		func(c aiw.ConversationalContext, err error, cancel aiw.CancelStreamFunc) {
			errReported = err
		},
		nil,
		nil,
		nil,
		nil,
	)
	assert.Error(t, err)
	assert.NotNil(t, errReported)
}

