package aiw_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/morphy76/aiw-client/pkg/aiw"
)

func TestClientBuilder_DefaultBuild(t *testing.T) {
	client, err := aiw.NewClientBuilder().Build()
	require.NoError(t, err)
	require.NotNil(t, client)
	defer func() {
		require.NoError(t, client.Close())
	}()

	convSvc := client.Conversational()
	require.NotNil(t, convSvc)
}

func TestClientBuilder_NewBuilderAlias(t *testing.T) {
	client, err := aiw.NewBuilder().Build()
	require.NoError(t, err)
	require.NotNil(t, client)
	defer func() {
		require.NoError(t, client.Close())
	}()

	require.NotNil(t, client.Conversational())
}

func TestClientBuilder_WithConversationalService(t *testing.T) {
	clientMock := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	})

	mockSvc, err := aiw.NewConversationalServiceBuilder().
		WithHTTPClient(clientMock).
		Build()
	require.NoError(t, err)

	client, err := aiw.NewClientBuilder().
		WithConversationalService(mockSvc).
		Build()
	require.NoError(t, err)
	require.NotNil(t, client)
	defer func() {
		require.NoError(t, client.Close())
	}()

	assert.Same(t, mockSvc, client.Conversational())
}

func TestClientBuilder_WithConversationalServiceBuilder(t *testing.T) {
	clientMock := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	})

	convBuilder := aiw.NewConversationalServiceBuilder().
		WithHTTPClient(clientMock).
		WithTimeout(10 * time.Second)

	client, err := aiw.NewClientBuilder().
		WithConversationalServiceBuilder(convBuilder).
		WithTimeout(15 * time.Second).
		Build()
	require.NoError(t, err)
	require.NotNil(t, client)
	defer func() {
		require.NoError(t, client.Close())
	}()

	require.NotNil(t, client.Conversational())
}

func TestClientBuilder_WithCustomDependenciesAndFlow(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	clientMock := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Accept") == "text/event-stream" {
			go func() {
				_, _ = fmt.Fprint(pw, "data: {\"lifecycle\":{\"event\":\"created\",\"dialog_id\":\"diag-builder-test-202\"}}\n\n")
			}()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       pr,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(http.NoBody),
		}, nil
	})

	logger := zerolog.Nop()

	client, err := aiw.NewClientBuilder().
		WithHTTPClient(clientMock).
		WithBaseURL("https://dev.lab.aiwave.io/").
		WithLogger(logger).
		WithTimeout(10 * time.Second).
		Build()
	require.NoError(t, err)
	require.NotNil(t, client)
	defer func() {
		require.NoError(t, client.Close())
	}()

	convCtx, err := aiw.NewConversationalContextBuilder().
		WithContext(context.Background()).
		WithExternalID("user-facade-builder-flow").
		Build()
	require.NoError(t, err)

	var onOpenCalled int32
	err = client.Conversational().OpenConversation(
		convCtx,
		func(c aiw.ConversationalContext) error {
			atomic.AddInt32(&onOpenCalled, 1)
			return nil
		},
		nil,
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&onOpenCalled))
	assert.Equal(t, "diag-builder-test-202", convCtx.DialogID())

	err = client.Conversational().AddCustomerMessage(convCtx, "Testing message via built facade")
	require.NoError(t, err)
}

func TestClientBuilder_WithOptionsDirectly(t *testing.T) {
	clientMock := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK}, nil
	})

	client, err := aiw.New(
		aiw.WithHTTPClient(clientMock),
		aiw.WithBaseURL("https://api.aiwave.io"),
		aiw.WithTimeout(20*time.Second),
	)
	require.NoError(t, err)
	require.NotNil(t, client)
	defer func() {
		require.NoError(t, client.Close())
	}()

	require.NotNil(t, client.Conversational())
}

