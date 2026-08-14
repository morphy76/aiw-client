package main

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMetricsCollector_RecordAndSummary(t *testing.T) {
	m := NewMetricsCollector("user_test_123")
	assert.Equal(t, "user_test_123", m.ExternalID())

	m.RecordSSEOpenTime(150 * time.Millisecond)
	m.RecordDialogCreated(200 * time.Millisecond, "dialog-98765")
	assert.Equal(t, "dialog-98765", m.DialogID())

	m.RecordMessageSent(50 * time.Millisecond)
	m.RecordCustomerMessageReceived()
	m.RecordBotMessageReceived(1200 * time.Millisecond)

	m.RecordMessageSent(45 * time.Millisecond)
	m.RecordCustomerMessageReceived()
	m.RecordBotMessageReceived(950 * time.Millisecond)

	m.RecordClose(true)
	m.EndConversation(2 * time.Second)

	summary := m.FormatSummary(ConfigSummary{
		BaseURL:     "https://dev.lab.aiwave.io",
		Tenant:      "default",
		DialogModel: "RocchettoEmbeddingsV2",
		Environment: "dev",
		Turns:       2,
	})

	assert.Contains(t, summary, "Test Configuration")
	assert.Contains(t, summary, "user_test_123")
	assert.Contains(t, summary, "dialog-98765")
	assert.Contains(t, summary, "Requested SSE Connections:")
	assert.Contains(t, summary, "Successful SSE Connections:")
	assert.Contains(t, summary, "Created Dialogs:")
	assert.Contains(t, summary, "Closed Dialogs:")
	assert.Contains(t, summary, "Messages Sent:")
	assert.Contains(t, summary, "Customer Messages Received:")
	assert.Contains(t, summary, "Bot Messages Received:")
	assert.Contains(t, summary, "Conversation Success: true")
}

func TestMetricsCollector_ErrorTracking(t *testing.T) {
	m := NewMetricsCollector("user_err_456")
	m.RecordError("sse_error", errors.New("connection reset"))

	assert.Equal(t, 1, m.ErrorCount())
	assert.False(t, m.IsSuccess())
}

func TestMetricsCollector_LatencyCalculations(t *testing.T) {
	latencies := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		300 * time.Millisecond,
		400 * time.Millisecond,
	}

	stats := calculateDurationStats(latencies)
	assert.Equal(t, 100*time.Millisecond, stats.Min)
	assert.Equal(t, 400*time.Millisecond, stats.Max)
	assert.Equal(t, 250*time.Millisecond, stats.Avg)
}
