package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// DurationStats encapsulates min, max, avg, p90, and p95 latency statistics.
type DurationStats struct {
	Count int
	Min   time.Duration
	Max   time.Duration
	Avg   time.Duration
	P90   time.Duration
	P95   time.Duration
}

// ConfigSummary holds metadata for the formatted summary output.
type ConfigSummary struct {
	Environment string
	BaseURL     string
	DialogModel string
	Tenant      string
	Turns       int
	DatasetSize int
	Sandbox     bool
	MockMode    bool
}

// ErrorRecord captures an operational error.
type ErrorRecord struct {
	Category string
	Err      error
	Time     time.Time
}

// MetricsCollector gathers execution timing, round-trip latencies, and protocol counters.
type MetricsCollector struct {
	mu                       sync.RWMutex
	externalID               string
	dialogID                 string
	startTime                time.Time
	totalDuration            time.Duration
	requestedSSEConnections  int
	successfulSSEConnections int
	failedSSEConnections     int
	createdDialogs           int
	closedDialogs            int
	sseOpenDuration          time.Duration
	dialogCreationDuration   time.Duration
	messagesSent             int
	messageDeliveryLatencies []time.Duration
	customerMessagesReceived int
	botMessagesReceived      int
	botAnswerLatencies       []time.Duration
	errors                   []ErrorRecord
	success                  bool
}

// NewMetricsCollector initializes a new collector for a conversation session.
func NewMetricsCollector(externalID string) *MetricsCollector {
	return &MetricsCollector{
		externalID:              externalID,
		startTime:               time.Now(),
		requestedSSEConnections: 1,
		success:                 true,
	}
}

// ExternalID returns the target external ID.
func (m *MetricsCollector) ExternalID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.externalID
}

// DialogID returns the associated dialog ID.
func (m *MetricsCollector) DialogID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dialogID
}

// RecordSSEOpenTime records the time taken to open the SSE connection.
func (m *MetricsCollector) RecordSSEOpenTime(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sseOpenDuration = d
	m.successfulSSEConnections++
}

// RecordSSEFailed marks an SSE connection attempt failure.
func (m *MetricsCollector) RecordSSEFailed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failedSSEConnections++
	m.success = false
}

// RecordDialogCreated records the dialog creation event and its latency.
func (m *MetricsCollector) RecordDialogCreated(d time.Duration, dialogID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dialogCreationDuration = d
	m.dialogID = dialogID
	m.createdDialogs++
}

// RecordMessageSent records a customer message POST request delivery time.
func (m *MetricsCollector) RecordMessageSent(deliveryTime time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messagesSent++
	m.messageDeliveryLatencies = append(m.messageDeliveryLatencies, deliveryTime)
}

// RecordCustomerMessageReceived records an SSE customer message confirmation.
func (m *MetricsCollector) RecordCustomerMessageReceived() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.customerMessagesReceived++
}

// RecordBotMessageReceived records a bot answer and the full turn round-trip latency.
func (m *MetricsCollector) RecordBotMessageReceived(answerTime time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.botMessagesReceived++
	m.botAnswerLatencies = append(m.botAnswerLatencies, answerTime)
}

// RecordClose records conversation closure.
func (m *MetricsCollector) RecordClose(closed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if closed {
		m.closedDialogs++
	}
}

// RecordError records an error event.
func (m *MetricsCollector) RecordError(category string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.errors = append(m.errors, ErrorRecord{
			Category: category,
			Err:      err,
			Time:     time.Now(),
		})
		m.success = false
	}
}

// EndConversation finalizes the conversation run and sets total execution duration.
func (m *MetricsCollector) EndConversation(totalDuration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalDuration = totalDuration
}

// ErrorCount returns the number of captured errors.
func (m *MetricsCollector) ErrorCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.errors)
}

// IsSuccess returns whether the conversation flow completed without unhandled errors.
func (m *MetricsCollector) IsSuccess() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.success && len(m.errors) == 0
}

// calculateDurationStats computes min, max, avg, p90, and p95 for a slice of durations.
func calculateDurationStats(durations []time.Duration) DurationStats {
	if len(durations) == 0 {
		return DurationStats{}
	}

	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	var sum time.Duration
	for _, d := range sorted {
		sum += d
	}

	p90Idx := int(float64(len(sorted)-1) * 0.90)
	p95Idx := int(float64(len(sorted)-1) * 0.95)

	return DurationStats{
		Count: len(sorted),
		Min:   sorted[0],
		Max:   sorted[len(sorted)-1],
		Avg:   sum / time.Duration(len(sorted)),
		P90:   sorted[p90Idx],
		P95:   sorted[p95Idx],
	}
}

func formatDurationMS(d time.Duration) string {
	if d == 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000.0)
}

// FormatSummary generates a comprehensive report replicating section 7 metrics of conversation-test.js.
func (m *MetricsCollector) FormatSummary(cfg ConfigSummary) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var sb strings.Builder

	sb.WriteString("\n================================================================================\n")
	sb.WriteString("📋 Test Configuration:\n")
	sb.WriteString("================================================================================\n")
	sb.WriteString(fmt.Sprintf("  Environment:       %s\n", cfg.Environment))
	sb.WriteString(fmt.Sprintf("  Base URL:          %s\n", cfg.BaseURL))
	sb.WriteString(fmt.Sprintf("  Dialog Model:      %s\n", cfg.DialogModel))
	sb.WriteString(fmt.Sprintf("  Tenant:            %s\n", cfg.Tenant))
	sb.WriteString(fmt.Sprintf("  Sandbox Mode:      %t\n", cfg.Sandbox))
	sb.WriteString(fmt.Sprintf("  Mock Mode:         %t\n", cfg.MockMode))
	sb.WriteString(fmt.Sprintf("  Target Turns:      %d\n", cfg.Turns))
	sb.WriteString(fmt.Sprintf("  External ID:       %s\n", m.externalID))
	sb.WriteString(fmt.Sprintf("  Dialog ID:         %s\n", m.dialogID))
	if cfg.DatasetSize > 0 {
		sb.WriteString(fmt.Sprintf("  Dataset Prompts:   %d\n", cfg.DatasetSize))
	}

	sb.WriteString("\n📊 Detailed Client-Side Metrics:\n")
	sb.WriteString("================================================================================\n")

	sb.WriteString("🔗 Connection & Session Lifecycle:\n")
	sb.WriteString(fmt.Sprintf("  - Requested SSE Connections:   %d\n", m.requestedSSEConnections))
	sb.WriteString(fmt.Sprintf("  - Successful SSE Connections:  %d\n", m.successfulSSEConnections))
	sb.WriteString(fmt.Sprintf("  - Failed SSE Connections:      %d\n", m.failedSSEConnections))
	sb.WriteString(fmt.Sprintf("  - Created Dialogs:             %d\n", m.createdDialogs))
	sb.WriteString(fmt.Sprintf("  - Closed Dialogs:              %d\n", m.closedDialogs))
	sb.WriteString(fmt.Sprintf("  - SSE Open Time:               %s\n", formatDurationMS(m.sseOpenDuration)))
	sb.WriteString(fmt.Sprintf("  - Dialog Created Event Time:   %s\n", formatDurationMS(m.dialogCreationDuration)))

	postStats := calculateDurationStats(m.messageDeliveryLatencies)
	sb.WriteString("\n⏱️ Message Delivery Latency (POST /message):\n")
	if postStats.Count > 0 {
		sb.WriteString(fmt.Sprintf("  - Min: %s | Max: %s | Avg: %s | P90: %s | P95: %s\n",
			formatDurationMS(postStats.Min), formatDurationMS(postStats.Max),
			formatDurationMS(postStats.Avg), formatDurationMS(postStats.P90),
			formatDurationMS(postStats.P95)))
	} else {
		sb.WriteString("  - N/A\n")
	}

	answerStats := calculateDurationStats(m.botAnswerLatencies)
	sb.WriteString("\n🤖 Bot Answer Round-Trip Latency (POST -> SSE Bot Message):\n")
	if answerStats.Count > 0 {
		sb.WriteString(fmt.Sprintf("  - Min: %s | Max: %s | Avg: %s | P90: %s | P95: %s\n",
			formatDurationMS(answerStats.Min), formatDurationMS(answerStats.Max),
			formatDurationMS(answerStats.Avg), formatDurationMS(answerStats.P90),
			formatDurationMS(answerStats.P95)))
	} else {
		sb.WriteString("  - N/A\n")
	}

	sb.WriteString("\n📨 Message Throughput & Verification:\n")
	sb.WriteString(fmt.Sprintf("  - Messages Sent:               %d\n", m.messagesSent))
	sb.WriteString(fmt.Sprintf("  - Customer Messages Received:  %d\n", m.customerMessagesReceived))
	sb.WriteString(fmt.Sprintf("  - Bot Messages Received:       %d\n", m.botMessagesReceived))
	sb.WriteString(fmt.Sprintf("  - Total Conversation Duration: %s\n", formatDurationMS(m.totalDuration)))

	sb.WriteString("\n🎯 Protocol Compliance & Assertions:\n")
	allSSEAccounted := (m.successfulSSEConnections + m.failedSSEConnections) == m.requestedSSEConnections
	allDialogsClosed := m.createdDialogs == m.closedDialogs
	customerMatchesTurns := m.customerMessagesReceived == cfg.Turns
	botMatchesTurns := m.botMessagesReceived == cfg.Turns

	sb.WriteString(fmt.Sprintf("  [%s] All requested SSE connections accounted for\n", checkMark(allSSEAccounted)))
	sb.WriteString(fmt.Sprintf("  [%s] All created dialogs closed properly\n", checkMark(allDialogsClosed)))
	sb.WriteString(fmt.Sprintf("  [%s] Customer received messages equal expected turns (%d/%d)\n", checkMark(customerMatchesTurns), m.customerMessagesReceived, cfg.Turns))
	sb.WriteString(fmt.Sprintf("  [%s] Bot received messages equal expected turns (%d/%d)\n", checkMark(botMatchesTurns), m.botMessagesReceived, cfg.Turns))

	if len(m.errors) > 0 {
		sb.WriteString("\n❌ Encountered Errors:\n")
		for i, errRec := range m.errors {
			sb.WriteString(fmt.Sprintf("  %d. [%s] %v\n", i+1, errRec.Category, errRec.Err))
		}
	}

	overallSuccess := m.success && len(m.errors) == 0 && allSSEAccounted && allDialogsClosed && botMatchesTurns
	sb.WriteString("\n================================================================================\n")
	if overallSuccess {
		sb.WriteString("✅ Overall Flow Status: SUCCESS\n")
	} else {
		sb.WriteString("❌ Overall Flow Status: FAILED\n")
	}
	sb.WriteString(fmt.Sprintf("  Conversation Success: %t\n", overallSuccess))
	sb.WriteString("================================================================================\n")

	return sb.String()
}

func checkMark(ok bool) string {
	if ok {
		return "✓"
	}
	return "✗"
}
