package outbound

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/morphy76/aiw-client/internal/conversational/application/ports/inbound"
)

type attachmentPayload struct {
	Filename   string            `json:"filename"`
	ContentRef string            `json:"contentref"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type customerMessagePayload struct {
	ExternalID  string              `json:"external_id"`
	Command     string              `json:"command"`
	Role        string              `json:"role"`
	Text        string              `json:"text"`
	Attachments []attachmentPayload `json:"attachments,omitempty"`
}

// MessageClient handles sending customer messages and closing active dialog sessions.
type MessageClient struct {
	client  HTTPClient
	baseURL string
}

// NewMessageClient creates a new MessageClient.
func NewMessageClient(client HTTPClient, baseURL string) *MessageClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &MessageClient{
		client:  client,
		baseURL: normalizeBaseURL(baseURL),
	}
}

// SendCustomerMessage sends POST /dialog/api/conversation/v1.0/message/{dialogId}.
func (c *MessageClient) SendCustomerMessage(
	ctx context.Context,
	cmd inbound.AddCustomerMessageCommand,
	dialogID string,
) error {
	targetURL := fmt.Sprintf("%s%s/%s", c.baseURL, defaultMessageEndpoint, url.PathEscape(dialogID))

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
	applyHeaders(req, cmd.Tenant, cmd.BearerToken, cmd.Sandbox, cmd.Headers)

	resp, err := c.client.Do(req)
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
func (c *MessageClient) CloseSession(
	ctx context.Context,
	cmd inbound.CloseConversationCommand,
	dialogID string,
) error {
	targetURL := fmt.Sprintf("%s%s/%s/%s", c.baseURL, defaultCloseEndpoint, url.PathEscape(cmd.ExternalID), url.PathEscape(dialogID))

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, targetURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create close request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	applyHeaders(req, cmd.Tenant, cmd.BearerToken, cmd.Sandbox, cmd.Headers)

	resp, err := c.client.Do(req)
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
