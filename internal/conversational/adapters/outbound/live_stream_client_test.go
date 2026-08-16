package outbound_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/morphy76/aiw-client/internal/conversational/adapters/outbound"
	"github.com/morphy76/aiw-client/internal/conversational/application/ports/inbound"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiveStreamClient_OpenSessionStream(t *testing.T) {
	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "text/event-stream", req.Header.Get("Accept"))
		assert.Equal(t, "/dialog/api/conversation/v1.0/live/user-777", req.URL.Path)

		sseBody := "data: {\"lifecycle\":{\"event\":\"created\",\"dialog_id\":\"dlg-777\"}}\n\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(sseBody))),
		}, nil
	})

	streamClient := outbound.NewLiveStreamClient(client, "https://dev.lab.aiwave.io")
	handler := &testStreamHandler{}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := streamClient.OpenSessionStream(ctx, inbound.OpenConversationCommand{
		ExternalID: "user-777",
	}, handler)

	require.NoError(t, err)
	assert.True(t, handler.openCalled)
	assert.Equal(t, "dlg-777", handler.createdDialogID)
}
