package outbound

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/morphy76/aiw-client/internal/conversational/application/ports/inbound"
	outboundPorts "github.com/morphy76/aiw-client/internal/conversational/application/ports/outbound"
	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
)

// sseRawEvent mirrors the JSON SSE event payload structure from AIW.
type sseRawEvent struct {
	Lifecycle *struct {
		Event    string `json:"event"`
		DialogID string `json:"dialog_id"`
	} `json:"lifecycle,omitempty"`
	Message *struct {
		Event string `json:"event"`
		Role  string `json:"role"`
		Text  string `json:"text"`
	} `json:"message,omitempty"`
}

// LiveStreamClient handles Server-Sent Events (SSE) connections and real-time streaming.
type LiveStreamClient struct {
	client  HTTPClient
	baseURL string
}

// NewLiveStreamClient creates a new LiveStreamClient.
func NewLiveStreamClient(client HTTPClient, baseURL string) *LiveStreamClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &LiveStreamClient{
		client:  client,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

// OpenSessionStream initiates an SSE connection, awaits dialog creation, and processes SSE events in background.
func (c *LiveStreamClient) OpenSessionStream(
	ctx context.Context,
	cmd inbound.OpenConversationCommand,
	handler outboundPorts.StreamEventHandler,
) error {
	targetBaseURL := resolveBaseURL(cmd.BaseURL, c.baseURL)

	targetURL := fmt.Sprintf("%s%s/%s", targetBaseURL, defaultLiveEndpoint, url.PathEscape(cmd.ExternalID))
	if cmd.DialogModel != "" {
		targetURL = fmt.Sprintf("%s?with_dialog_model=%s", targetURL, url.QueryEscape(cmd.DialogModel))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create SSE request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	applyHeaders(req, cmd.Tenant, cmd.BearerToken, cmd.Sandbox, cmd.Headers)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", model.ErrSSEConnectionFailed, err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		return fmt.Errorf("%w: HTTP status %d: %s", model.ErrSSEConnectionFailed, resp.StatusCode, string(body))
	}

	createdCh := make(chan struct{})
	errCh := make(chan error, 1)
	var once sync.Once
	var closedCleanly bool

	// Background reader for SSE stream
	go func() {
		defer func() {
			_ = resp.Body.Close()
			if !closedCleanly {
				handler.OnClosed()
			}
		}()

		scanner := bufio.NewScanner(resp.Body)
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "" || strings.HasPrefix(line, ":") {
				continue
			}

			if !strings.HasPrefix(line, "data:") {
				continue
			}

			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "{}" {
				continue
			}

			var event sseRawEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				handler.OnError(fmt.Errorf("failed to parse SSE json: %w", err))
				continue
			}

			if event.Lifecycle != nil {
				switch event.Lifecycle.Event {
				case "created":
					if err := handler.OnCreated(event.Lifecycle.DialogID); err != nil {
						once.Do(func() {
							errCh <- err
						})
						return
					}
					once.Do(func() {
						close(createdCh)
					})
				case "aborted":
					closedCleanly = true
					handler.OnAborted("server aborted conversation")
					once.Do(func() {
						errCh <- model.ErrConversationAborted
					})
					return
				case "closed":
					closedCleanly = true
					handler.OnClosed()
					return
				}
			}

			if event.Message != nil && event.Message.Event == "messageAdded" {
				switch event.Message.Role {
				case "BOT", "AGENT":
					if err := handler.OnBotMessage(event.Message.Text); err != nil {
						handler.OnError(err)
					}
				case "CUSTOMER":
					if err := handler.OnCustomerMessage(event.Message.Text); err != nil {
						handler.OnError(err)
					}
				}
			}
		}

		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			handler.OnError(err)
			once.Do(func() {
				errCh <- err
			})
		}
	}()

	// Wait for created event or error/timeout
	select {
	case <-createdCh:
		return nil
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(15 * time.Second):
		return fmt.Errorf("%w: timeout waiting for created event", model.ErrSSEConnectionFailed)
	}
}
