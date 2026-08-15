package outbound

import (
	"fmt"
	"net/http"
	"strings"
)

// HTTPClient interface for making HTTP requests.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

const (
	defaultLiveEndpoint                           = "/dialog/api/conversation/v1.0/live"
	defaultMessageEndpoint                        = "/dialog/api/conversation/v1.0/message"
	defaultCloseEndpoint                          = "/dialog/api/conversation/v1.0"
	defaultDialogSessionFromFilterEndpoint        = "/dialog/api/dialogSession/v1.0/_fromFilter"
	defaultDialogSessionWithRecordingDataEndpoint = "/dialog/api/dialogSession/v1.0/_withRecordingData"
)

const defaultAPIBaseURL = "https://dev.lab.aiwave.io"

func normalizeBaseURL(baseURL string) string {
	cleaned := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if cleaned == "" {
		return defaultAPIBaseURL
	}
	return cleaned
}

func applyHeaders(
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
