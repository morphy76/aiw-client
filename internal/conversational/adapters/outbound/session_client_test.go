package outbound_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/morphy76/aiw-client/internal/conversational/adapters/outbound"
	"github.com/morphy76/aiw-client/internal/conversational/application/ports/inbound"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionClient_ListSessions(t *testing.T) {
	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, req.Method)
		assert.Equal(t, "/dialog/api/dialogSession/v1.0/_fromFilter", req.URL.Path)

		jsonResp := `[
			{
				"id": 999,
				"externalId": "user-sub-client",
				"recordingData": "<recording><session><userTurn dateTime=\"15/08/2026 09:00:00.000\"><item id=\"u_u\"><subItem><value>Inquiry on billing</value></subItem></item></userTurn></session></recording>"
			}
		]`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(jsonResp))),
		}, nil
	})

	sessionClient := outbound.NewSessionClient(client, "https://dev.lab.aiwave.io")
	activities, err := sessionClient.ListSessions(context.Background(), inbound.ListSessionsCommand{
		AssistantName: "Rocchetto",
		ExternalID:    "user-sub-client",
	})

	require.NoError(t, err)
	require.Len(t, activities, 1)
	assert.Equal(t, int64(999), activities[0].ID())
	assert.Equal(t, "user-sub-client", activities[0].ExternalID())
	assert.Equal(t, "Inquiry on billing", activities[0].Title())
}

func TestSessionClient_GetSessionRecording(t *testing.T) {
	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, req.Method)
		assert.Equal(t, "/dialog/api/dialogSession/v1.0/_withRecordingData", req.URL.Path)

		jsonResp := `[
			{
				"id": 999,
				"recordingData": "<recording><session><userTurn dateTime=\"15/08/2026 09:00:00.000\"><item id=\"u_u\"><subItem><value>Question text</value></subItem></item></userTurn></session></recording>"
			}
		]`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader([]byte(jsonResp))),
		}, nil
	})

	sessionClient := outbound.NewSessionClient(client, "https://dev.lab.aiwave.io")
	messages, err := sessionClient.GetSessionRecording(context.Background(), inbound.RestoreConversationCommand{
		SessionIDs: []int64{999},
	})

	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, "Question text", messages[0].Content())
}
