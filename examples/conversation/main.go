package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/morphy76/aiw-client/pkg/aiw"
)

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvIntOrDefault(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func getEnvBoolOrDefault(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if boolVal, err := strconv.ParseBool(val); err == nil {
			return boolVal
		}
	}
	return defaultVal
}

func main() {
	// 1. Command-line flags and environment fallbacks
	baseURLFlag := flag.String("url", getEnvOrDefault("BASE_URL", "https://dev.lab.aiwave.io"), "AIW Base URL")
	patFlag := flag.String("token", getEnvOrDefault("PAT", os.Getenv("BEARER_TOKEN")), "Bearer PAT Token")
	tenantFlag := flag.String("tenant", getEnvOrDefault("TENANT", "default"), "Tenant ID (x-cognitive-system)")
	modelFlag := flag.String("model", getEnvOrDefault("DIALOG_MODEL_NAME", "RocchettoEmbeddingsV2"), "Dialog model name")
	turnsFlag := flag.Int("turns", getEnvIntOrDefault("CONVERSATION_TURNS", 2), "Number of conversation turns")
	delayFlag := flag.Duration("delay", 1*time.Second, "Delay between turns")
	sandboxFlag := flag.Bool("sandbox", getEnvBoolOrDefault("SANDBOX_MODE", false), "Enable cognitive sandbox mode")
	datasetFlag := flag.String("dataset", getEnvOrDefault("MESSAGES_FILE", ""), "Path to user messages CSV dataset")
	mockFlag := flag.Bool("mock", getEnvBoolOrDefault("USE_MOCK", false), "Run with in-memory mock gateway (offline mode)")
	verboseFlag := flag.Bool("verbose", getEnvBoolOrDefault("DEBUG", false), "Enable debug logging")
	envNameFlag := flag.String("env", getEnvOrDefault("TEST_ENV", "dev"), "Environment name (dev, integration, prod)")

	flag.Parse()

	// 2. Configure structured logging
	logLevel := zerolog.InfoLevel
	if *verboseFlag {
		logLevel = zerolog.DebugLevel
	}
	output := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}
	logger := zerolog.New(output).With().Timestamp().Logger().Level(logLevel)

	fmt.Println("================================================================================")
	fmt.Println("🚀 AIW Conversational Flow Test & Example Runner")
	fmt.Println("================================================================================")

	// Validate PAT token if not in mock mode
	if *patFlag == "" && !*mockFlag {
		logger.Warn().Msg("⚠️ PAT (Bearer Token) is missing! Running without PAT may result in 401 Unauthorized unless -mock is used.")
	}

	// 3. Load dataset
	dataset, err := LoadDataset(*datasetFlag)
	if err != nil {
		logger.Error().Err(err).Msg("failed to load dataset, falling back to defaults")
		dataset, _ = LoadDataset("")
	}
	logger.Info().Int("prompts_count", len(dataset)).Msg("Loaded customer prompts dataset")

	// 4. Generate unique external ID
	externalID := fmt.Sprintf("test_user_%d_%04d", time.Now().UnixMilli(), rand.Intn(10000))
	logger.Info().Str("external_id", externalID).Int("turns", *turnsFlag).Msg("Initializing conversational session")

	// 5. Context with OS signals and timeout
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Set overall test execution timeout
	overallTimeout := 2*time.Minute + time.Duration(*turnsFlag)*30*time.Second
	ctx, timeoutCancel := context.WithTimeout(ctx, overallTimeout)
	defer timeoutCancel()

	// 6. Build AIW client facade
	var client aiw.Client
	if *mockFlag {
		logger.Info().Msg("🔧 Running in MOCK MODE with in-memory gateway")
		mockSvc, err := aiw.NewConversationalServiceBuilder().
			WithLogger(logger).
			WithInMemoryGateway().
			WithTimeout(15 * time.Second).
			Build()
		if err != nil {
			logger.Fatal().Err(err).Msg("failed to build mock conversational service")
		}
		client, err = aiw.New(
			aiw.WithLogger(logger),
			aiw.WithConversationalService(mockSvc),
		)
		if err != nil {
			logger.Fatal().Err(err).Msg("failed to initialize AIW client")
		}
	} else {
		client, err = aiw.New(
			aiw.WithLogger(logger),
			aiw.WithTimeout(60*time.Second),
		)
		if err != nil {
			logger.Fatal().Err(err).Msg("failed to initialize AIW client")
		}
	}
	defer func() { _ = client.Close() }()

	convService := client.Conversational()

	// 7. Build ConversationalContext
	convCtx, err := aiw.NewConversationalContextBuilder().
		WithContext(ctx).
		WithExternalID(externalID).
		WithTenant(*tenantFlag).
		WithDialogModel(*modelFlag).
		WithBearerToken(*patFlag).
		WithSandbox(*sandboxFlag).
		WithBaseURL(*baseURLFlag).
		Build()
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to build ConversationalContext")
	}

	// 8. Setup metrics collector and reactive turn coordinator
	metrics := NewMetricsCollector(externalID)
	startFlowTime := time.Now()
	sseRequestTime := time.Now()

	var (
		flowDoneOnce   sync.Once
		flowDoneCh     = make(chan struct{})
		turnMu         sync.Mutex
		currentTurn    = 0
		lastTurnSentAt time.Time
	)

	finishFlow := func() {
		flowDoneOnce.Do(func() {
			close(flowDoneCh)
		})
	}

	// Helper function to send customer message for a turn
	sendCustomerMessage := func(turnNum int) {
		prompt := RandomMessage(dataset)
		turnMu.Lock()
		lastTurnSentAt = time.Now()
		turnMu.Unlock()

		preview := prompt.Message
		if len(preview) > 60 {
			preview = preview[:57] + "..."
		}
		fmt.Printf("\n📤 [Turn %d/%d] Sending Customer Message (%s): \"%s\"\n", turnNum, *turnsFlag, prompt.Category, preview)

		msgStart := time.Now()
		if err := convService.AddCustomerMessage(convCtx, prompt.Message); err != nil {
			deliveryTime := time.Since(msgStart)
			metrics.RecordMessageSent(deliveryTime)
			metrics.RecordError("message_send", err)
			fmt.Printf("❌ [Turn %d] Failed to post message: %v\n", turnNum, err)
			return
		}

		deliveryTime := time.Since(msgStart)
		metrics.RecordMessageSent(deliveryTime)
		fmt.Printf("✅ [Turn %d] Customer message delivered in %s\n", turnNum, deliveryTime.Round(time.Millisecond))
	}

	// 9. Register reactive lifecycle callbacks
	onOpen := func(c aiw.ConversationalContext) error {
		openDuration := time.Since(sseRequestTime)
		metrics.RecordSSEOpenTime(openDuration)
		metrics.RecordDialogCreated(openDuration, c.DialogID())

		fmt.Printf("🔗 SSE connection established! Dialog ID: %s (Opened in %s)\n", c.DialogID(), openDuration.Round(time.Millisecond))
		fmt.Printf("📝 Starting message exchange (%d planned turns)...\n", *turnsFlag)

		// Start Turn 1
		turnMu.Lock()
		currentTurn = 1
		turnMu.Unlock()

		go func() {
			time.Sleep(500 * time.Millisecond)
			sendCustomerMessage(1)
		}()
		return nil
	}

	onCustomerMessage := func(c aiw.ConversationalContext, mex string) error {
		metrics.RecordCustomerMessageReceived()
		preview := mex
		if len(preview) > 60 {
			preview = preview[:57] + "..."
		}
		fmt.Printf("👤 Customer message confirmed via SSE: \"%s\"\n", preview)
		return nil
	}

	onBotMessage := func(c aiw.ConversationalContext, mex string) error {
		turnMu.Lock()
		turnAt := currentTurn
		roundTrip := time.Since(lastTurnSentAt)
		turnMu.Unlock()

		metrics.RecordBotMessageReceived(roundTrip)

		preview := strings.ReplaceAll(mex, "\n", " ")
		if len(preview) > 80 {
			preview = preview[:77] + "..."
		}
		fmt.Printf("🤖 [Turn %d] Bot response in %s via SSE: \"%s\"\n", turnAt, roundTrip.Round(time.Millisecond), preview)

		turnMu.Lock()
		hasNextTurn := currentTurn < *turnsFlag
		if hasNextTurn {
			currentTurn++
			nextTurn := currentTurn
			turnMu.Unlock()

			go func() {
				if *delayFlag > 0 {
					time.Sleep(*delayFlag)
				}
				sendCustomerMessage(nextTurn)
			}()
		} else {
			turnMu.Unlock()
			fmt.Printf("\n🔚 Completed all %d turns. Initiating graceful conversation closure...\n", *turnsFlag)
			go func() {
				time.Sleep(500 * time.Millisecond)
				if err := convService.CloseConversation(convCtx); err != nil {
					logger.Error().Err(err).Msg("failed to close conversation")
					metrics.RecordError("conversation_close", err)
				}
				metrics.RecordClose(true)
				finishFlow()
			}()
		}
		return nil
	}

	onError := func(c aiw.ConversationalContext, err error) {
		metrics.RecordError("sse_error", err)
		fmt.Printf("❌ Error encountered: %v\n", err)
	}

	onClose := func(c aiw.ConversationalContext) error {
		metrics.RecordClose(true)
		fmt.Println("🔌 Conversation stream closed by server/client.")
		finishFlow()
		return nil
	}

	// 10. Open Conversation
	fmt.Printf("🔌 Connecting to SSE Live Stream: %s/dialog/api/conversation/v1.0/live/%s?with_dialog_model=%s\n", *baseURLFlag, externalID, *modelFlag)
	if err := convService.OpenConversation(convCtx, onOpen, onError, onCustomerMessage, onBotMessage, onClose); err != nil {
		metrics.RecordSSEFailed()
		metrics.RecordError("sse_open", err)
		fmt.Printf("❌ Failed to initiate conversation stream: %v\n", err)
		finishFlow()
	}

	// 11. Await completion or termination signal
	select {
	case <-flowDoneCh:
		// Normal completion
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			metrics.RecordError("timeout", fmt.Errorf("overall conversation test timeout reached"))
			fmt.Println("⚠️ Conversation test timed out.")
		} else {
			fmt.Println("\n🛑 Interrupted by user signal. Cleaning up...")
		}
		_ = convService.CloseConversation(convCtx)
	}

	metrics.EndConversation(time.Since(startFlowTime))

	// 12. Print formatted summary report
	summaryCfg := ConfigSummary{
		Environment: *envNameFlag,
		BaseURL:     *baseURLFlag,
		DialogModel: *modelFlag,
		Tenant:      *tenantFlag,
		Turns:       *turnsFlag,
		DatasetSize: len(dataset),
		Sandbox:     *sandboxFlag,
		MockMode:    *mockFlag,
	}

	fmt.Print(metrics.FormatSummary(summaryCfg))

	if !metrics.IsSuccess() {
		os.Exit(1)
	}
}
