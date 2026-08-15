package outbound

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/morphy76/aiw-client/internal/conversational/application/ports/inbound"
	"github.com/morphy76/aiw-client/internal/conversational/domain/model"
)

type filterDialogSessionPayload struct {
	DateFrom                   string   `json:"dateFrom,omitempty"`
	DateTo                     string   `json:"dateTo,omitempty"`
	ExternalID                 string   `json:"externalId,omitempty"`
	LangSet                    []string `json:"langSet,omitempty"`
	LastIDFound                int      `json:"lastIdFound,omitempty"`
	Models                     []string `json:"models,omitempty"`
	NumberOfSessionsToRetrieve int      `json:"numberOfSessionsToRetrieve,omitempty"`
	RemoveSessionsBlockSize    int      `json:"removeSessionsBlockSize,omitempty"`
	SortField                  string   `json:"sortField,omitempty"`
	SortOrder                  string   `json:"sortOrder,omitempty"`
	StatusSet                  []string `json:"statusSet,omitempty"`
	WithRecordingData          bool     `json:"withRecordingData"`
}

type dialogSessionDTO struct {
	ID                   int64           `json:"id"`
	ExternalID           string          `json:"externalId"`
	ApplicationNamespace string          `json:"applicationNamespace"`
	Model                string          `json:"model"`
	Recording            bool            `json:"recording"`
	RecordingData        string          `json:"recordingData"`
	Sandbox              bool            `json:"sandbox"`
	SessionID            string          `json:"sessionId"`
	Status               string          `json:"status"`
	StartTime            json.RawMessage `json:"startTime"`
	InsertDate           json.RawMessage `json:"insertDate"`
	UpdateDate           json.RawMessage `json:"updateDate"`
	CloseTime            json.RawMessage `json:"closeTime"`
	DeleteDate           json.RawMessage `json:"deleteDate"`
}

// SessionClient handles listing past dialog sessions and restoring recording history.
type SessionClient struct {
	client  HTTPClient
	baseURL string
}

// NewSessionClient creates a new SessionClient.
func NewSessionClient(client HTTPClient, baseURL string) *SessionClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &SessionClient{
		client:  client,
		baseURL: normalizeBaseURL(baseURL),
	}
}

// ListSessions retrieves recent activity sessions for an external ID from /dialogSession/v1.0/_fromFilter.
func (c *SessionClient) ListSessions(
	ctx context.Context,
	cmd inbound.ListSessionsCommand,
) ([]model.RecentActivity, error) {
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

	payload := filterDialogSessionPayload{
		ExternalID:                 cmd.ExternalID,
		NumberOfSessionsToRetrieve: limit,
		LastIDFound:                offset,
		SortField:                  sortField,
		SortOrder:                  sortOrder,
		WithRecordingData:          false,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal filter dialog session payload: %w", err)
	}

	targetURL := fmt.Sprintf("%s%s", c.baseURL, defaultDialogSessionFromFilterEndpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create list sessions request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	applyHeaders(req, cmd.Tenant, cmd.BearerToken, cmd.Sandbox, cmd.Headers)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list sessions request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("failed to list sessions: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var dtos []dialogSessionDTO
	if err := json.NewDecoder(resp.Body).Decode(&dtos); err != nil {
		return nil, fmt.Errorf("failed to decode sessions response: %w", err)
	}

	activities := make([]model.RecentActivity, 0, len(dtos))
	for _, dto := range dtos {
		startTime := parseFlexibleTimestamp(dto.StartTime)
		if startTime.IsZero() {
			startTime = parseFlexibleTimestamp(dto.InsertDate)
		}
		if startTime.IsZero() {
			startTime = parseFlexibleTimestamp(dto.UpdateDate)
		}

		title := ""
		if dto.RecordingData != "" {
			if msgs, err := ParseRecordingData(dto.RecordingData); err == nil && len(msgs) > 0 {
				for _, m := range msgs {
					if m.Sender() == model.SenderCustomer && strings.TrimSpace(m.Content()) != "" {
						title = m.Content()
						break
					}
				}
				if title == "" && msgs[0].Answer() != nil && msgs[0].Answer().Text() != "" {
					title = msgs[0].Answer().Text()
				} else if title == "" {
					title = msgs[0].Content()
				}
			}
		}
		if title == "" {
			if dto.SessionID != "" {
				title = fmt.Sprintf("Session %s", dto.SessionID)
			} else if dto.Model != "" {
				title = fmt.Sprintf("Session (%s)", dto.Model)
			} else if dto.ID > 0 {
				title = fmt.Sprintf("Session %d", dto.ID)
			} else {
				title = "(no title)"
			}
		}

		act, err := model.NewRecentActivity(dto.ID, dto.ExternalID, title, startTime)
		if err == nil {
			activities = append(activities, act)
		}
	}

	return activities, nil
}

// GetSessionRecording retrieves past XML dialog recording data from /dialogSession/v1.0/_withRecordingData.
func (c *SessionClient) GetSessionRecording(
	ctx context.Context,
	cmd inbound.RestoreConversationCommand,
) ([]model.Message, error) {
	var sessionIDs []int64
	if len(cmd.SessionIDs) > 0 {
		sessionIDs = cmd.SessionIDs
	} else if cmd.ExternalID != "" {
		if id, err := strconv.ParseInt(cmd.ExternalID, 10, 64); err == nil && id > 0 {
			sessionIDs = []int64{id}
		}
	}

	// If sessionIDs are not directly known, query sessions matching external ID via _fromFilter
	if len(sessionIDs) == 0 && cmd.ExternalID != "" {
		filterPayload := filterDialogSessionPayload{
			ExternalID:                 cmd.ExternalID,
			NumberOfSessionsToRetrieve: 10,
			SortField:                  "update_date",
			SortOrder:                  "DESC",
			WithRecordingData:          true,
		}
		bodyBytes, err := json.Marshal(filterPayload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal filter dialog session payload: %w", err)
		}
		filterURL := fmt.Sprintf("%s%s", c.baseURL, defaultDialogSessionFromFilterEndpoint)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, filterURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("failed to create list sessions request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		applyHeaders(req, cmd.Tenant, cmd.BearerToken, cmd.Sandbox, cmd.Headers)

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list sessions request failed: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode == http.StatusOK {
			var filterDTOs []dialogSessionDTO
			if err := json.NewDecoder(resp.Body).Decode(&filterDTOs); err == nil {
				for _, dto := range filterDTOs {
					if dto.ID > 0 {
						sessionIDs = append(sessionIDs, dto.ID)
					}
				}
			}
		}
	}

	if len(sessionIDs) == 0 {
		return nil, nil
	}

	targetURL := fmt.Sprintf("%s%s", c.baseURL, defaultDialogSessionWithRecordingDataEndpoint)
	bodyBytes, err := json.Marshal(sessionIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal session IDs: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create get session recording request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	applyHeaders(req, cmd.Tenant, cmd.BearerToken, cmd.Sandbox, cmd.Headers)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get session recording request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("failed to get session recording: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var dtos []dialogSessionDTO
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

func parseFlexibleTimestamp(raw json.RawMessage) time.Time {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return time.Time{}
	}

	if strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`) {
		var str string
		if err := json.Unmarshal(raw, &str); err != nil {
			return time.Time{}
		}
		str = strings.TrimSpace(str)
		if str == "" {
			return time.Time{}
		}
		layouts := []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02T15:04:05.000Z",
			"2006-01-02T15:04:05.000-07:00",
			"2006-01-02T15:04:05Z",
			"2006-01-02 15:04:05.000",
			"2006-01-02 15:04:05",
			"2006-01-02",
			"02/01/2006 15:04:05.000",
			"02/01/2006 15:04:05",
			"02/01/2006",
		}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, str); err == nil {
				return t.UTC()
			}
		}
		return time.Time{}
	}

	var num int64
	if err := json.Unmarshal(raw, &num); err == nil && num > 0 {
		if num > 1e11 {
			return time.UnixMilli(num).UTC()
		}
		return time.Unix(num, 0).UTC()
	}

	return time.Time{}
}
