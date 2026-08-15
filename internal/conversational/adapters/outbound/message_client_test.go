package outbound_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/morphy76/aiw-client/internal/conversational/adapters/outbound"
	"github.com/morphy76/aiw-client/internal/conversational/application/ports/inbound"
	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageClient_SendCustomerMessage(t *testing.T) {
	var receivedBody []byte
	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, req.Method)
		assert.Equal(t, "/dialog/api/conversation/v1.0/message/dlg-123", req.URL.Path)
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

	msgClient := outbound.NewMessageClient(client, "https://dev.lab.aiwave.io")
	att := model.NewAttachment("doc.pdf", "ref-100", nil)
	cmd := inbound.AddCustomerMessageCommand{
		ExternalID:  "user-100",
		Message:     "Hello world",
		Attachments: []model.Attachment{att},
	}

	err := msgClient.SendCustomerMessage(context.Background(), cmd, "dlg-123")
	require.NoError(t, err)
	assert.JSONEq(t, `{"external_id":"user-100","command":"addMessage","role":"CUSTOMER","text":"Hello world","attachments":[{"filename":"doc.pdf","contentref":"ref-100"}]}`, string(receivedBody))
}

func TestMessageClient_CloseSession(t *testing.T) {
	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodDelete, req.Method)
		assert.Equal(t, "/dialog/api/conversation/v1.0/user-100/dlg-123", req.URL.Path)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
		}, nil
	})

	msgClient := outbound.NewMessageClient(client, "https://dev.lab.aiwave.io")
	err := msgClient.CloseSession(context.Background(), inbound.CloseConversationCommand{
		ExternalID: "user-100",
	}, "dlg-123")
	require.NoError(t, err)
}
