package outbound_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/morphy76/aiw-client/internal/conversational/adapters/outbound"
	"github.com/morphy76/aiw-client/internal/conversational/application/ports/inbound"
	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestHTTPClient(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

type testStreamHandler struct {
	mu              sync.Mutex
	createdDialogID string
	customerMsgs    []string
	botMsgs         []string
	closedCalled    bool
	abortedCalled   bool
	errors          []error
}

func (h *testStreamHandler) OnCreated(dialogID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.createdDialogID = dialogID
	return nil
}

func (h *testStreamHandler) OnCustomerMessage(text string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.customerMsgs = append(h.customerMsgs, text)
	return nil
}

func (h *testStreamHandler) OnBotMessage(text string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.botMsgs = append(h.botMsgs, text)
	return nil
}

func (h *testStreamHandler) OnAborted(reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.abortedCalled = true
}

func (h *testStreamHandler) OnClosed() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closedCalled = true
}

func (h *testStreamHandler) OnError(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.errors = append(h.errors, err)
}

func TestHTTPSSEGateway_OpenSessionStream(t *testing.T) {
	t.Run("successfully connects and processes SSE events", func(t *testing.T) {
		var receivedHeaders http.Header
		var receivedPath string
		var receivedQuery string

		pr, pw := io.Pipe()

		client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
			receivedHeaders = req.Header.Clone()
			receivedPath = req.URL.Path
			receivedQuery = req.URL.RawQuery

			go func() {
				defer pw.Close()
				_, _ = fmt.Fprint(pw, "data: {\"lifecycle\":{\"event\":\"created\",\"dialog_id\":\"dlg-sse-123\"}}\n\n")
				time.Sleep(10 * time.Millisecond)
				_, _ = fmt.Fprint(pw, "data: {\"message\":{\"event\":\"messageAdded\",\"role\":\"CUSTOMER\",\"text\":\"Hi!\"}}\n\n")
				time.Sleep(10 * time.Millisecond)
				_, _ = fmt.Fprint(pw, "data: {\"message\":{\"event\":\"messageAdded\",\"role\":\"BOT\",\"text\":\"Hello human!\"}}\n\n")
				time.Sleep(10 * time.Millisecond)
				_, _ = fmt.Fprint(pw, "data: {\"lifecycle\":{\"event\":\"closed\"}}\n\n")
			}()

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       pr,
			}, nil
		})

		gw := outbound.NewHTTPSSEGateway(client, "https://dev.lab.aiwave.io")
		handler := &testStreamHandler{}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		cmd := inbound.OpenConversationCommand{
			ExternalID:  "ext-user-1",
			Tenant:      "customer-care",
			DialogModel: "RocchettoEmbeddingsV2",
			BearerToken: "pat-token-abc",
			Sandbox:     false,
		}

		err := gw.OpenSessionStream(ctx, cmd, handler)
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		assert.Equal(t, "/dialog/api/conversation/v1.0/live/ext-user-1", receivedPath)
		assert.Equal(t, "with_dialog_model=RocchettoEmbeddingsV2", receivedQuery)
		assert.Equal(t, "Bearer pat-token-abc", receivedHeaders.Get("Authorization"))
		assert.Equal(t, "live:customer-care", receivedHeaders.Get("x-cognitive-system"))
		assert.Equal(t, "false", receivedHeaders.Get("x-cognitive-sandbox"))
		assert.Equal(t, "text/event-stream", receivedHeaders.Get("Accept"))

		handler.mu.Lock()
		defer handler.mu.Unlock()
		assert.Equal(t, "dlg-sse-123", handler.createdDialogID)
		assert.Equal(t, []string{"Hi!"}, handler.customerMsgs)
		assert.Equal(t, []string{"Hello human!"}, handler.botMsgs)
		assert.True(t, handler.closedCalled)
	})

	t.Run("handles aborted lifecycle event", func(t *testing.T) {
		pr, pw := io.Pipe()

		client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
			go func() {
				defer pw.Close()
				_, _ = fmt.Fprint(pw, "data: {\"lifecycle\":{\"event\":\"created\",\"dialog_id\":\"dlg-abort-456\"}}\n\n")
				time.Sleep(10 * time.Millisecond)
				_, _ = fmt.Fprint(pw, "data: {\"lifecycle\":{\"event\":\"aborted\"}}\n\n")
			}()

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       pr,
			}, nil
		})

		gw := outbound.NewHTTPSSEGateway(client, "https://dev.lab.aiwave.io")
		handler := &testStreamHandler{}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		cmd := inbound.OpenConversationCommand{
			ExternalID: "ext-abort-user",
		}

		err := gw.OpenSessionStream(ctx, cmd, handler)
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		handler.mu.Lock()
		defer handler.mu.Unlock()
		assert.Equal(t, "dlg-abort-456", handler.createdDialogID)
		assert.True(t, handler.abortedCalled)
	})
}

func TestHTTPSSEGateway_SendCustomerMessage(t *testing.T) {
	var receivedMethod string
	var receivedPath string
	var receivedHost string
	var receivedHeaders http.Header
	var receivedBody []byte
	var postCount int32

	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&postCount, 1)
		receivedMethod = req.Method
		receivedPath = req.URL.Path
		receivedHost = req.URL.Host
		receivedHeaders = req.Header.Clone()
		var err error
		receivedBody, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusCreated,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"status":"ok"}`))),
		}, nil
	})

	gw := outbound.NewHTTPSSEGateway(client, "https://dev.lab.aiwave.io")
	cmd := inbound.AddCustomerMessageCommand{
		ExternalID:  "ext-user-1",
		Message:     "How do I reset my password?",
		Tenant:      "customer-care",
		BearerToken: "pat-tok-123",
		Sandbox:     true,
		BaseURL:     "https://custom.aiwave.io",
		Headers: map[string]string{
			"X-Custom-Trace": "trace-101",
		},
	}

	err := gw.SendCustomerMessage(context.Background(), cmd, "dlg-999")
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&postCount))
	assert.Equal(t, http.MethodPost, receivedMethod)
	assert.Equal(t, "custom.aiwave.io", receivedHost)
	assert.Equal(t, "/dialog/api/conversation/v1.0/message/dlg-999", receivedPath)
	assert.Equal(t, "application/json", receivedHeaders.Get("Content-Type"))
	assert.Equal(t, "application/json", receivedHeaders.Get("Accept"))
	assert.Equal(t, "Bearer pat-tok-123", receivedHeaders.Get("Authorization"))
	assert.Equal(t, "live:customer-care", receivedHeaders.Get("x-cognitive-system"))
	assert.Equal(t, "true", receivedHeaders.Get("x-cognitive-sandbox"))
	assert.Equal(t, "trace-101", receivedHeaders.Get("X-Custom-Trace"))

	expectedBody := `{"external_id":"ext-user-1","command":"addMessage","role":"CUSTOMER","text":"How do I reset my password?"}`
	assert.JSONEq(t, expectedBody, string(receivedBody))
}

func TestHTTPSSEGateway_SendCustomerMessageWithAttachments(t *testing.T) {
	var receivedBody []byte
	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		var err error
		receivedBody, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"status":"ok"}`))),
		}, nil
	})

	gw := outbound.NewHTTPSSEGateway(client, "https://dev.lab.aiwave.io")
	att := model.NewAttachment("test.pdf", "content-ref-99", map[string]string{"toolName": "docViewer"})
	cmd := inbound.AddCustomerMessageCommand{
		ExternalID:  "ext-user-1",
		Message:     "Here is my attachment",
		Attachments: []model.Attachment{att},
	}

	err := gw.SendCustomerMessage(context.Background(), cmd, "dlg-999")
	require.NoError(t, err)

	expectedJSON := `{"external_id":"ext-user-1","command":"addMessage","role":"CUSTOMER","text":"Here is my attachment","attachments":[{"filename":"test.pdf","contentref":"content-ref-99","metadata":{"toolName":"docViewer"}}]}`
	assert.JSONEq(t, expectedJSON, string(receivedBody))
}

func TestHTTPSSEGateway_CloseSession(t *testing.T) {
	var receivedMethod string
	var receivedPath string
	var receivedHeaders http.Header

	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		receivedMethod = req.Method
		receivedPath = req.URL.Path
		receivedHeaders = req.Header.Clone()
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(bytes.NewReader([]byte(``))),
		}, nil
	})

	gw := outbound.NewHTTPSSEGateway(client, "https://dev.lab.aiwave.io")
	cmd := inbound.CloseConversationCommand{
		ExternalID:  "ext-user-1",
		Tenant:      "customer-care",
		BearerToken: "pat-tok-123",
		Sandbox:     false,
	}

	err := gw.CloseSession(context.Background(), cmd, "dlg-999")
	require.NoError(t, err)
	assert.Equal(t, http.MethodDelete, receivedMethod)
	assert.Equal(t, "/dialog/api/conversation/v1.0/ext-user-1/dlg-999", receivedPath)
	assert.Equal(t, "Bearer pat-tok-123", receivedHeaders.Get("Authorization"))
	assert.Equal(t, "live:customer-care", receivedHeaders.Get("x-cognitive-system"))
	assert.Equal(t, "false", receivedHeaders.Get("x-cognitive-sandbox"))
	assert.Equal(t, "application/json", receivedHeaders.Get("Accept"))
}

func TestHTTPSSEGateway_ListSessions(t *testing.T) {
	var receivedPath string
	var receivedQuery string
	var receivedHeaders http.Header

	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		receivedPath = req.URL.Path
		receivedQuery = req.URL.RawQuery
		receivedHeaders = req.Header.Clone()

		jsonResp := `[
			{
				"external_id": "session-1",
				"title": "Account support inquiry",
				"start_time": "2026-08-15T09:00:00Z"
			},
			{
				"external_id": "session-2",
				"title": "Order tracking issue",
				"start_time": "2026-08-14T15:30:00Z"
			}
		]`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(jsonResp))),
		}, nil
	})

	gw := outbound.NewHTTPSSEGateway(client, "https://dev.lab.aiwave.io")
	cmd := inbound.ListSessionsCommand{
		AssistantName: "RocchettoEmbeddingsV2",
		ExternalID:    "user-alpha",
		Limit:         5,
		LastIDFound:   0,
		SortField:     "update_date",
		SortOrder:     "DESC",
		BearerToken:   "pat-token-list",
	}

	activities, err := gw.ListSessions(context.Background(), cmd)
	require.NoError(t, err)
	require.Len(t, activities, 2)

	assert.Equal(t, "session-1", activities[0].ExternalID())
	assert.Equal(t, "Account support inquiry", activities[0].Title())
	assert.Equal(t, "session-2", activities[1].ExternalID())
	assert.Equal(t, "Order tracking issue", activities[1].Title())

	assert.Equal(t, "/dialog/api/chat/sessions/RocchettoEmbeddingsV2", receivedPath)
	assert.Contains(t, receivedQuery, "externalId=user-alpha")
	assert.Contains(t, receivedQuery, "numberOfSessionsToRetrieve=5")
	assert.Contains(t, receivedQuery, "sortField=update_date")
	assert.Contains(t, receivedQuery, "sortOrder=DESC")
	assert.Equal(t, "Bearer pat-token-list", receivedHeaders.Get("Authorization"))
}

func TestHTTPSSEGateway_GetSessionRecording(t *testing.T) {
	var receivedPath string
	var receivedHeaders http.Header

	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		receivedPath = req.URL.Path
		receivedHeaders = req.Header.Clone()

		jsonResp := `[
			{
				"recordingData": "<recording><session><userTurn dateTime=\"15/08/2026 09:00:00.000\"><item id=\"u_u\"><subItem><value>Hello previous session</value></subItem></item></userTurn><systemTurn dateTime=\"15/08/2026 09:00:01.000\"><item id=\"u_m\"><subItem><value>{\"answer\":\"Restored response\",\"sources\":[]}</value></subItem></item></systemTurn></session></recording>"
			}
		]`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(jsonResp))),
		}, nil
	})

	gw := outbound.NewHTTPSSEGateway(client, "https://dev.lab.aiwave.io")
	cmd := inbound.RestoreConversationCommand{
		ExternalID:  "session-restore-1",
		BearerToken: "pat-token-rec",
	}

	messages, err := gw.GetSessionRecording(context.Background(), cmd)
	require.NoError(t, err)
	require.Len(t, messages, 2)

	assert.Equal(t, model.SenderCustomer, messages[0].Sender())
	assert.Equal(t, "Hello previous session", messages[0].Content())
	assert.Equal(t, model.SenderAgent, messages[1].Sender())
	require.NotNil(t, messages[1].Answer())
	assert.Equal(t, "Restored response", messages[1].Answer().Text())

	assert.Equal(t, "/dialog/api/dialogSession/v1.0/_byExternalId/session-restore-1", receivedPath)
	assert.Equal(t, "Bearer pat-token-rec", receivedHeaders.Get("Authorization"))
}

