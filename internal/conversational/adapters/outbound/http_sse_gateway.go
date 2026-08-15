package outbound

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/morphy76/aiw-client/internal/conversational/application/ports/inbound"
	outboundPorts "github.com/morphy76/aiw-client/internal/conversational/application/ports/outbound"
	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
)

// HTTPClient interface for making HTTP requests.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

const (
	defaultLiveEndpoint        = "/dialog/api/conversation/v1.0/live"
	defaultMessageEndpoint     = "/dialog/api/conversation/v1.0/message"
	defaultCloseEndpoint       = "/dialog/api/conversation/v1.0"
	defaultSessionsEndpoint    = "/dialog/api/chat/sessions"
	defaultDialogSessionEndpoint = "/dialog/api/dialogSession/v1.0/_byExternalId"
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

type attachmentPayload struct {
	Filename   string            `json:"filename"`
	ContentRef string            `json:"contentref"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// customerMessagePayload is the JSON payload sent to add a message.
type customerMessagePayload struct {
	ExternalID  string              `json:"external_id"`
	Command     string              `json:"command"`
	Role        string              `json:"role"`
	Text        string              `json:"text"`
	Attachments []attachmentPayload `json:"attachments,omitempty"`
}

type sessionResponseDTO struct {
	ExternalID string `json:"external_id"`
	Title      string `json:"title"`
	StartTime  string `json:"start_time"`
}

type sessionRecordingDTO struct {
	RecordingData string `json:"recordingData"`
}

// HTTPSSEGateway implements AIWGateway over HTTP and Server-Sent Events (SSE).
type HTTPSSEGateway struct {
	client  HTTPClient
	baseURL string
}

// NewHTTPSSEGateway creates a new HTTPSSEGateway.
func NewHTTPSSEGateway(client HTTPClient, baseURL string) *HTTPSSEGateway {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPSSEGateway{
		client:  client,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

// OpenSessionStream initiates an SSE connection, awaits dialog creation, and processes SSE events in background.
func (g *HTTPSSEGateway) OpenSessionStream(
	ctx context.Context,
	cmd inbound.OpenConversationCommand,
	handler outboundPorts.StreamEventHandler,
) error {
	baseURL := g.resolveBaseURL(cmd.BaseURL)

	targetURL := fmt.Sprintf("%s%s/%s", baseURL, defaultLiveEndpoint, url.PathEscape(cmd.ExternalID))
	if cmd.DialogModel != "" {
		targetURL = fmt.Sprintf("%s?with_dialog_model=%s", targetURL, url.QueryEscape(cmd.DialogModel))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create SSE request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	g.applyHeaders(req, cmd.Tenant, cmd.BearerToken, cmd.Sandbox, cmd.Headers)

	resp, err := g.client.Do(req)
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

// SendCustomerMessage sends POST /dialog/api/conversation/v1.0/message/{dialogId}.
func (g *HTTPSSEGateway) SendCustomerMessage(
	ctx context.Context,
	cmd inbound.AddCustomerMessageCommand,
	dialogID string,
) error {
	baseURL := g.resolveBaseURL(cmd.BaseURL)
	targetURL := fmt.Sprintf("%s%s/%s", baseURL, defaultMessageEndpoint, url.PathEscape(dialogID))

	var attachments []attachmentPayload
	if len(cmd.Attachments) > 0 {
		attachments = make([]attachmentPayload, 0, len(cmd.Attachments))
		for _, a := range cmd.Attachments {
			attachments = append(attachments, attachmentPayload{
				Filename:   a.Filename(),
				ContentRef: a.ContentRef(),
				Metadata:   a.Metadata(),
			})
		}
	}

	payload := customerMessagePayload{
		ExternalID:  cmd.ExternalID,
		Command:     "addMessage",
		Role:        "CUSTOMER",
		Text:        cmd.Message,
		Attachments: attachments,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal customer message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create message request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	g.applyHeaders(req, cmd.Tenant, cmd.BearerToken, cmd.Sandbox, cmd.Headers)

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("message POST request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("failed to send message: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// CloseSession sends DELETE /dialog/api/conversation/v1.0/{externalId}/{dialogId}.
func (g *HTTPSSEGateway) CloseSession(
	ctx context.Context,
	cmd inbound.CloseConversationCommand,
	dialogID string,
) error {
	baseURL := g.resolveBaseURL(cmd.BaseURL)
	targetURL := fmt.Sprintf("%s%s/%s/%s", baseURL, defaultCloseEndpoint, url.PathEscape(cmd.ExternalID), url.PathEscape(dialogID))

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, targetURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create close request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	g.applyHeaders(req, cmd.Tenant, cmd.BearerToken, cmd.Sandbox, cmd.Headers)

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("close DELETE request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("failed to close conversation: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// ListSessions retrieves recent activity sessions for an external ID from /chat/sessions/{assistantName}.
func (g *HTTPSSEGateway) ListSessions(
	ctx context.Context,
	cmd inbound.ListSessionsCommand,
) ([]model.RecentActivity, error) {
	baseURL := g.resolveBaseURL(cmd.BaseURL)

	limit := cmd.Limit
	if limit <= 0 {
		limit = 10
	}
	offset := cmd.LastIDFound
	if offset < 0 {
		offset = 0
	}
	sortField := cmd.SortField
	if sortField == "" {
		sortField = "update_date"
	}
	sortOrder := cmd.SortOrder
	if sortOrder == "" {
		sortOrder = "DESC"
	}

	assistantName := cmd.AssistantName
	if assistantName == "" {
		assistantName = "default"
	}

	queryParams := url.Values{}
	queryParams.Set("numberOfSessionsToRetrieve", strconv.Itoa(limit))
	queryParams.Set("lastIdFound", strconv.Itoa(offset))
	queryParams.Set("externalId", cmd.ExternalID)
	queryParams.Set("sortField", sortField)
	queryParams.Set("sortOrder", sortOrder)

	targetURL := fmt.Sprintf("%s%s/%s?%s", baseURL, defaultSessionsEndpoint, url.PathEscape(assistantName), queryParams.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create list sessions request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	g.applyHeaders(req, cmd.Tenant, cmd.BearerToken, cmd.Sandbox, cmd.Headers)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list sessions request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("failed to list sessions: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var dtos []sessionResponseDTO
	if err := json.NewDecoder(resp.Body).Decode(&dtos); err != nil {
		return nil, fmt.Errorf("failed to decode sessions response: %w", err)
	}

	activities := make([]model.RecentActivity, 0, len(dtos))
	for _, dto := range dtos {
		var startTime time.Time
		if dto.StartTime != "" {
			if t, err := time.Parse(time.RFC3339, dto.StartTime); err == nil {
				startTime = t
			} else if t, err := time.Parse("2006-01-02 15:04:05", dto.StartTime); err == nil {
				startTime = t
			}
		}
		act, err := model.NewRecentActivity(dto.ExternalID, dto.Title, startTime)
		if err == nil {
			activities = append(activities, act)
		}
	}

	return activities, nil
}

// GetSessionRecording retrieves past XML dialog recording data from /dialogSession/v1.0/_byExternalId/{externalId}.
func (g *HTTPSSEGateway) GetSessionRecording(
	ctx context.Context,
	cmd inbound.RestoreConversationCommand,
) ([]model.Message, error) {
	baseURL := g.resolveBaseURL(cmd.BaseURL)
	targetURL := fmt.Sprintf("%s%s/%s", baseURL, defaultDialogSessionEndpoint, url.PathEscape(cmd.ExternalID))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get session recording request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	g.applyHeaders(req, cmd.Tenant, cmd.BearerToken, cmd.Sandbox, cmd.Headers)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get session recording request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("failed to get session recording: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var dtos []sessionRecordingDTO
	if err := json.NewDecoder(resp.Body).Decode(&dtos); err != nil {
		return nil, fmt.Errorf("failed to decode session recordings JSON: %w", err)
	}

	var allMessages []model.Message
	for _, item := range dtos {
		if item.RecordingData != "" {
			parsed, err := ParseRecordingData(item.RecordingData)
			if err != nil {
				return nil, fmt.Errorf("failed to parse recording XML: %w", err)
			}
			allMessages = append(allMessages, parsed...)
		}
	}

	return allMessages, nil
}

func (g *HTTPSSEGateway) resolveBaseURL(customURL string) string {
	if customURL != "" {
		return strings.TrimRight(customURL, "/")
	}
	if g.baseURL != "" {
		return g.baseURL
	}
	return "https://dev.lab.aiwave.io"
}

func (g *HTTPSSEGateway) applyHeaders(
	req *http.Request,
	tenant string,
	bearerToken string,
	sandbox bool,
	customHeaders map[string]string,
) {
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	if tenant == "" {
		tenant = "default"
	}
	req.Header.Set("x-cognitive-system", "live:"+tenant)
	req.Header.Set("x-cognitive-sandbox", fmt.Sprintf("%t", sandbox))

	for k, v := range customHeaders {
		req.Header.Set(k, v)
	}
}
