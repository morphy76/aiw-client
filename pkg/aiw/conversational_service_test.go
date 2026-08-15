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
	defer pw.Close()

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

	convSvc, err := aiw.NewConversationalServiceBuilder().
		WithHTTPClient(client).
		WithBaseURL("https://dev.lab.aiwave.io").
		Build()
	require.NoError(t, err)

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
	defer pw.Close()

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

	convSvc, err := aiw.NewConversationalServiceBuilder().
		WithHTTPClient(client).
		WithBaseURL("https://dev.lab.aiwave.io").
		Build()
	require.NoError(t, err)

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

func TestConversationalService_OnBotMessageAndCustomerMessageFlow(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Accept") == "text/event-stream" {
			go func() {
				_, _ = fmt.Fprint(pw, "data: {\"lifecycle\":{\"event\":\"created\",\"dialog_id\":\"dlg-stream-flow\"}}\n\n")
				time.Sleep(10 * time.Millisecond)
				_, _ = fmt.Fprint(pw, "data: {\"message\":{\"event\":\"messageAdded\",\"role\":\"CUSTOMER\",\"text\":\"User query\"}}\n\n")
				time.Sleep(10 * time.Millisecond)
				_, _ = fmt.Fprint(pw, "data: {\"message\":{\"event\":\"messageAdded\",\"role\":\"BOT\",\"text\":\"Bot response answer\"}}\n\n")
			}()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       pr,
			}, nil
		}
		return &http.Response{StatusCode: http.StatusOK}, nil
	})

	convSvc, err := aiw.NewConversationalServiceBuilder().
		WithHTTPClient(client).
		WithBaseURL("https://dev.lab.aiwave.io").
		Build()
	require.NoError(t, err)

	convCtx := aiw.NewConversationalContext(context.Background(), "ext-full-flow")

	var mu sync.Mutex
	var receivedCustomerMsg string
	var receivedBotMsg string

	err = convSvc.OpenConversation(
		convCtx,
		nil,
		nil,
		func(c aiw.ConversationalContext, mex string) error {
			mu.Lock()
			receivedCustomerMsg = mex
			mu.Unlock()
			return nil
		},
		func(c aiw.ConversationalContext, mex string) error {
			mu.Lock()
			receivedBotMsg = mex
			mu.Unlock()
			return nil
		},
		nil,
	)
	require.NoError(t, err)

	time.Sleep(60 * time.Millisecond)
	mu.Lock()
	assert.Equal(t, "User query", receivedCustomerMsg)
	assert.Equal(t, "Bot response answer", receivedBotMsg)
	mu.Unlock()
}

func TestConversationalService_OnBotMessagePanicRecovery(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		go func() {
			_, _ = fmt.Fprint(pw, "data: {\"lifecycle\":{\"event\":\"created\",\"dialog_id\":\"dlg-bot-panic\"}}\n\n")
			time.Sleep(10 * time.Millisecond)
			_, _ = fmt.Fprint(pw, "data: {\"message\":{\"event\":\"messageAdded\",\"role\":\"BOT\",\"text\":\"Bot answers\"}}\n\n")
		}()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       pr,
		}, nil
	})

	convSvc, err := aiw.NewConversationalServiceBuilder().
		WithHTTPClient(client).
		WithBaseURL("https://dev.lab.aiwave.io").
		Build()
	require.NoError(t, err)

	convCtx := aiw.NewConversationalContext(context.Background(), "ext-panic-bot")

	var mu sync.Mutex
	var onErrorCalled int32
	var errorReported error

	err = convSvc.OpenConversation(
		convCtx,
		nil,
		func(c aiw.ConversationalContext, err error) {
			atomic.AddInt32(&onErrorCalled, 1)
			mu.Lock()
			errorReported = err
			mu.Unlock()
		},
		nil,
		func(c aiw.ConversationalContext, mex string) error {
			panic("bot message handler panic")
		},
		nil,
	)
	require.NoError(t, err)

	time.Sleep(60 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&onErrorCalled))
	mu.Lock()
	assert.NotNil(t, errorReported)
	assert.Contains(t, errorReported.Error(), "panic in OnBotMessageFn callback")
	mu.Unlock()
}

func TestConversationalService_AddCustomerMessageWithoutOpen(t *testing.T) {
	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	})

	convSvc, err := aiw.NewConversationalServiceBuilder().
		WithHTTPClient(client).
		Build()
	require.NoError(t, err)

	convCtx := aiw.NewConversationalContext(context.Background(), "ext-unopened")

	err = convSvc.AddCustomerMessage(convCtx, "Hello")
	assert.Error(t, err)
}

func TestConversationalService_EmptyExternalID(t *testing.T) {
	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	})

	convSvc, err := aiw.NewConversationalServiceBuilder().
		WithHTTPClient(client).
		Build()
	require.NoError(t, err)

	convCtx := aiw.NewConversationalContext(context.Background(), "")

	var onErrorCalled int32
	err = convSvc.OpenConversation(
		convCtx,
		nil,
		func(c aiw.ConversationalContext, err error) {
			atomic.AddInt32(&onErrorCalled, 1)
		},
		nil,
		nil,
		nil,
	)
	assert.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&onErrorCalled))

	onErrorCalled = 0
	err = convSvc.AddCustomerMessage(convCtx, "test")
	assert.Error(t, err)
}

func TestConversationalService_FullMultiTurnFlowReplicatingJS(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	var postsReceived []string
	var deleteReceived bool
	var mu sync.Mutex

	var convSvc aiw.ConversationalService

	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Accept") == "text/event-stream" {
			go func() {
				_, _ = fmt.Fprint(pw, "data: {\"lifecycle\":{\"event\":\"created\",\"dialog_id\":\"dlg-js-replicate-123\"}}\n\n")
			}()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       pr,
			}, nil
		}

		if req.Method == http.MethodPost {
			bodyBytes, _ := io.ReadAll(req.Body)
			mu.Lock()
			postsReceived = append(postsReceived, string(bodyBytes))
			mu.Unlock()

			assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
			assert.Equal(t, "application/json", req.Header.Get("Accept"))
			assert.Equal(t, "Bearer my-pat-token", req.Header.Get("Authorization"))
			assert.Equal(t, "live:my-tenant", req.Header.Get("x-cognitive-system"))
			assert.Equal(t, "false", req.Header.Get("x-cognitive-sandbox"))
			assert.Equal(t, "/dialog/api/conversation/v1.0/message/dlg-js-replicate-123", req.URL.Path)

			postCount := len(postsReceived)

			// Asynchronously simulate SSE pushing messageAdded events (CUSTOMER confirmation then BOT reply)
			go func(count int) {
				time.Sleep(10 * time.Millisecond)
				_, _ = fmt.Fprintf(pw, "data: {\"message\":{\"event\":\"messageAdded\",\"role\":\"CUSTOMER\",\"text\":\"%s\"}}\n\n", "user-msg")
				time.Sleep(10 * time.Millisecond)
				_, _ = fmt.Fprintf(pw, "data: {\"message\":{\"event\":\"messageAdded\",\"role\":\"BOT\",\"text\":\"bot-reply-for-%d\"}}\n\n", count)
			}(postCount)

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"status":"ok"}`))),
			}, nil
		}

		if req.Method == http.MethodDelete {
			mu.Lock()
			deleteReceived = true
			mu.Unlock()

			assert.Equal(t, "Bearer my-pat-token", req.Header.Get("Authorization"))
			assert.Equal(t, "live:my-tenant", req.Header.Get("x-cognitive-system"))
			assert.Equal(t, "/dialog/api/conversation/v1.0/user-ext-999/dlg-js-replicate-123", req.URL.Path)

			go func() {
				time.Sleep(10 * time.Millisecond)
				_, _ = fmt.Fprint(pw, "data: {\"lifecycle\":{\"event\":\"closed\"}}\n\n")
			}()

			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(bytes.NewReader([]byte(``))),
			}, nil
		}

		return &http.Response{StatusCode: http.StatusNotFound}, nil
	})

	var err error
	convSvc, err = aiw.NewConversationalServiceBuilder().
		WithHTTPClient(client).
		WithBaseURL("https://dev.lab.aiwave.io").
		Build()
	require.NoError(t, err)

	convCtx, err := aiw.NewConversationalContextBuilder().
		WithExternalID("user-ext-999").
		WithTenant("my-tenant").
		WithBearerToken("my-pat-token").
		WithSandbox(false).
		Build()
	require.NoError(t, err)

	var (
		turnsCompleted int32
		customerTurns  int32
		onCloseCalled  int32
	)

	err = convSvc.OpenConversation(
		convCtx,
		func(c aiw.ConversationalContext) error {
			assert.Equal(t, "dlg-js-replicate-123", c.DialogID())
			// Turn 1: Post initial customer message
			return convSvc.AddCustomerMessage(c, "First question from user")
		},
		func(c aiw.ConversationalContext, err error) {
			t.Errorf("unexpected error: %v", err)
		},
		func(c aiw.ConversationalContext, mex string) error {
			atomic.AddInt32(&customerTurns, 1)
			return nil
		},
		func(c aiw.ConversationalContext, mex string) error {
			current := atomic.AddInt32(&turnsCompleted, 1)
			if current == 1 {
				// Turn 2: Follow up question
				return convSvc.AddCustomerMessage(c, "Second question from user")
			}
			// Reached desired turns (2 turns), close conversation as in conversation-test.js
			return convSvc.CloseConversation(c)
		},
		func(c aiw.ConversationalContext) error {
			atomic.AddInt32(&onCloseCalled, 1)
			return nil
		},
	)
	require.NoError(t, err)

	// Wait for async events to settle
	assert.Eventually(t, func() bool {
		return atomic.LoadInt32(&turnsCompleted) == 2 &&
			atomic.LoadInt32(&onCloseCalled) == 1 &&
			atomic.LoadInt32(&customerTurns) == 2
	}, 2*time.Second, 20*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, postsReceived, 2)
	assert.Contains(t, postsReceived[0], "First question from user")
	assert.Contains(t, postsReceived[0], `"command":"addMessage"`)
	assert.Contains(t, postsReceived[0], `"role":"CUSTOMER"`)
	assert.Contains(t, postsReceived[1], "Second question from user")
	assert.True(t, deleteReceived)
}

func TestConversationalService_ListSessions(t *testing.T) {
	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "/dialog/api/chat/sessions/RocchettoEmbeddingsV2", req.URL.Path)
		assert.Equal(t, "user-456", req.URL.Query().Get("externalId"))
		assert.Equal(t, "5", req.URL.Query().Get("numberOfSessionsToRetrieve"))

		jsonResp := `[
			{
				"external_id": "user-456_sess_1",
				"title": "Password reset inquiry",
				"start_time": "2026-08-15T09:00:00Z"
			}
		]`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(jsonResp))),
		}, nil
	})

	convSvc, err := aiw.NewConversationalServiceBuilder().
		WithHTTPClient(client).
		Build()
	require.NoError(t, err)

	convCtx := aiw.NewConversationalContext(context.Background(), "user-456")
	sessions, err := convSvc.ListSessions(convCtx, aiw.ListSessionsQuery{
		AssistantName: "RocchettoEmbeddingsV2",
		Limit:         5,
	})
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, "user-456_sess_1", sessions[0].ExternalID)
	assert.Equal(t, "Password reset inquiry", sessions[0].Title)
}

func TestConversationalService_RestoreConversation(t *testing.T) {
	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "/dialog/api/dialogSession/v1.0/_byExternalId/user-prev-77", req.URL.Path)

		jsonResp := `[
			{
				"recordingData": "<recording><session><userTurn dateTime=\"15/08/2026 09:00:00.000\"><item id=\"u_u\"><subItem><value>Can I return an item?</value></subItem></item></userTurn><systemTurn dateTime=\"15/08/2026 09:00:02.000\"><item id=\"u_m\"><subItem><value>{\"answer\":\"Yes, within 30 days.\",\"sources\":[{\"id\":\"p1\",\"title\":\"Return Policy\"}]}</value></subItem></item></systemTurn></session></recording>"
			}
		]`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(jsonResp))),
		}, nil
	})

	convSvc, err := aiw.NewConversationalServiceBuilder().
		WithHTTPClient(client).
		Build()
	require.NoError(t, err)

	convCtx := aiw.NewConversationalContext(context.Background(), "user-prev-77")
	messages, err := convSvc.RestoreConversation(convCtx)
	require.NoError(t, err)
	require.Len(t, messages, 2)

	assert.Equal(t, "CUSTOMER", messages[0].Sender)
	assert.Equal(t, "Can I return an item?", messages[0].Content)

	assert.Equal(t, "AGENT", messages[1].Sender)
	require.NotNil(t, messages[1].Answer)
	assert.Equal(t, "Yes, within 30 days.", messages[1].Answer.Text)
	require.Len(t, messages[1].Answer.Sources, 1)
	assert.Equal(t, "p1", messages[1].Answer.Sources[0].ID)
	assert.Equal(t, "Return Policy", messages[1].Answer.Sources[0].Title)
}

func TestConversationalService_AddCustomerMessageWithOptions(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	var sentJSON string
	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Accept") == "text/event-stream" {
			go func() {
				_, _ = fmt.Fprint(pw, "data: {\"lifecycle\":{\"event\":\"created\",\"dialog_id\":\"dlg-att-1\"}}\n\n")
			}()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       pr,
			}, nil
		}
		if req.Method == http.MethodPost {
			bodyBytes, _ := io.ReadAll(req.Body)
			sentJSON = string(bodyBytes)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte(`{"status":"ok"}`))),
			}, nil
		}
		return &http.Response{StatusCode: http.StatusOK}, nil
	})

	convSvc, err := aiw.NewConversationalServiceBuilder().
		WithHTTPClient(client).
		Build()
	require.NoError(t, err)

	convCtx := aiw.NewConversationalContext(context.Background(), "user-att-test")
	err = convSvc.OpenConversation(convCtx, nil, nil, nil, nil, nil)
	require.NoError(t, err)

	err = convSvc.AddCustomerMessageWithOptions(convCtx, "Here is invoice", aiw.MessageOptions{
		Attachments: []aiw.Attachment{
			{
				Filename:   "invoice.pdf",
				ContentRef: "ref-invoice-101",
				Metadata:   map[string]string{"type": "invoice"},
			},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, sentJSON, "invoice.pdf")
	assert.Contains(t, sentJSON, "ref-invoice-101")
}

