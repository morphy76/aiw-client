package aiw_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/morphy76/aiw-client/pkg/aiw"
)

func TestConversationalService_OnOpenErrorBubbling(t *testing.T) {
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		go func() {
			_, _ = fmt.Fprint(pw, "data: {\"lifecycle\":{\"event\":\"created\",\"dialog_id\":\"dlg-test-1\"}}\n\n")
		}()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       pr,
		}, nil
	})

	clientFacade, err := aiw.NewClientBuilder().
		WithHTTPClient(client).
		WithBaseURL("https://dev.lab.aiwave.io").
		Build()
	require.NoError(t, err)
	convSvc := clientFacade.Conversational()

	convCtx := aiw.NewConversationalContext(context.Background(), "ext-fail-open")

	customErr := errors.New("cannot process open")
	var mu sync.Mutex
	var errorReported error
	var onErrorCalled int32

	err = convSvc.OpenConversation(
		convCtx,
		func(c aiw.ConversationalContext) error {
			return customErr
		},
		func(c aiw.ConversationalContext, err error) {
			atomic.AddInt32(&onErrorCalled, 1)
			mu.Lock()
			errorReported = err
			mu.Unlock()
		},
		nil,
		nil,
		nil,
	)

	assert.ErrorIs(t, err, customErr)
	assert.Equal(t, int32(1), atomic.LoadInt32(&onErrorCalled))
	mu.Lock()
	assert.Equal(t, customErr, errorReported)
	mu.Unlock()
}

func TestConversationalService_CallbackPanicRecovery(t *testing.T) {
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		go func() {
			_, _ = fmt.Fprint(pw, "data: {\"lifecycle\":{\"event\":\"created\",\"dialog_id\":\"dlg-test-panic\"}}\n\n")
		}()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       pr,
		}, nil
	})

	clientFacade, err := aiw.NewClientBuilder().
		WithHTTPClient(client).
		WithBaseURL("https://dev.lab.aiwave.io").
		Build()
	require.NoError(t, err)
	convSvc := clientFacade.Conversational()

	convCtx := aiw.NewConversationalContext(context.Background(), "ext-panic")

	var mu sync.Mutex
	var onErrorCalled int32
	var errorReported error

	// Panic in onOpenFn should be caught, not crash the process, and passed to onErrorFn
	err = convSvc.OpenConversation(
		convCtx,
		func(c aiw.ConversationalContext) error {
			panic("something went catastrophically wrong")
		},
		func(c aiw.ConversationalContext, err error) {
			atomic.AddInt32(&onErrorCalled, 1)
			mu.Lock()
			errorReported = err
			mu.Unlock()
		},
		nil,
		nil,
		nil,
	)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "panic in OnOpenFn callback")
	assert.Equal(t, int32(1), atomic.LoadInt32(&onErrorCalled))
	mu.Lock()
	assert.NotNil(t, errorReported)
	assert.Contains(t, errorReported.Error(), "panic in OnOpenFn callback")
	mu.Unlock()
}

