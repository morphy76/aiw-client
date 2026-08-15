package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
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

func getEnvBoolOrDefault(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if boolVal, err := strconv.ParseBool(val); err == nil {
			return boolVal
		}
	}
	return defaultVal
}

// promptWriter coordinates terminal output to prevent interleaving between asynchronous bot responses and user prompts.
type promptWriter struct {
	mu sync.Mutex
}

func newPromptWriter() *promptWriter {
	return &promptWriter{}
}

func (p *promptWriter) PrintPrompt() {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Print("💬 You > ")
}

func (p *promptWriter) PrintBotMessage(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Printf("\r🤖 Bot > %s\n💬 You > ", msg)
}

func (p *promptWriter) PrintError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Printf("\r❌ Error: %v\n💬 You > ", err)
}

func (p *promptWriter) PrintInfo(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Printf("\r%s\n", msg)
}

func main() {
	// 1. Command-line flags and environment fallbacks
	baseURLFlag := flag.String("url", getEnvOrDefault("BASE_URL", "https://dev.lab.aiwave.io"), "AIW Base URL")
	patFlag := flag.String("token", getEnvOrDefault("PAT", os.Getenv("BEARER_TOKEN")), "Bearer PAT Token")
	tenantFlag := flag.String("tenant", getEnvOrDefault("TENANT", "almawave.com"), "Tenant ID (x-cognitive-system)")
	modelFlag := flag.String("model", getEnvOrDefault("DIALOG_MODEL_NAME", "RocchettoEmbeddingsV2"), "Dialog model name")
	externalIDFlag := flag.String("external-id", getEnvOrDefault("EXTERNAL_ID", ""), "External customer user ID")
	sandboxFlag := flag.Bool("sandbox", getEnvBoolOrDefault("SANDBOX_MODE", false), "Enable cognitive sandbox mode")
	verboseFlag := flag.Bool("verbose", getEnvBoolOrDefault("DEBUG", false), "Enable debug logging")

	flag.Parse()

	// 2. Configure structured logging
	var logger zerolog.Logger
	if *verboseFlag {
		output := zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: time.RFC3339,
		}
		logger = zerolog.New(output).With().Timestamp().Logger().Level(zerolog.DebugLevel)
	} else {
		logger = zerolog.Nop()
	}

	externalID := *externalIDFlag
	if externalID == "" {
		externalID = fmt.Sprintf("user_%d_%04d", time.Now().Unix(), rand.Intn(10000))
	}

	modeStr := "Live AIW Platform"

	fmt.Println("================================================================================")
	fmt.Println("💬 AIW Interactive CLI Chat")
	fmt.Println("================================================================================")
	fmt.Printf("  Base URL    : %s\n", *baseURLFlag)
	fmt.Printf("  Tenant      : %s\n", *tenantFlag)
	fmt.Printf("  Dialog Model: %s\n", *modelFlag)
	fmt.Printf("  External ID : %s\n", externalID)
	fmt.Printf("  Sandbox     : %t\n", *sandboxFlag)
	fmt.Printf("  Mode        : %s\n", modeStr)
	fmt.Println("================================================================================")

	if *patFlag == "" {
		fmt.Println("⚠️  Warning: PAT (Bearer Token) is missing. Live requests may return 401 Unauthorized.")
		fmt.Println("   Use -token <PAT> to authenticate.")
	}

	// 3. Setup root context with OS signals
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// 4. Build AIW client facade
	var client aiw.Client
	var err error
	client, err = aiw.New(
		aiw.WithBaseURL(*baseURLFlag),
		aiw.WithLogger(logger),
		aiw.WithTimeout(60*time.Second),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to initialize AIW client: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = client.Close() }()

	convService := client.Conversational()

	// 5. Build ConversationalContext
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
		fmt.Fprintf(os.Stderr, "❌ Failed to build ConversationalContext: %v\n", err)
		os.Exit(1)
	}

	pw := newPromptWriter()
	sessionReady := make(chan struct{})
	sessionClosed := make(chan struct{})
	var closeOnce sync.Once

	finishSession := func() {
		closeOnce.Do(func() {
			close(sessionClosed)
		})
	}

	// 6. Reactive lifecycle callbacks
	onOpen := func(c aiw.ConversationalContext) error {
		pw.PrintInfo(fmt.Sprintf("🔗 Connected! Session Dialog ID: %s", c.DialogID()))
		pw.PrintInfo("💡 Type your message and press Enter to chat.")
		pw.PrintInfo("💡 Commands: '/help' for options, '/exit' or 'exit' to quit.")
		pw.PrintInfo("--------------------------------------------------------------------------------")
		close(sessionReady)
		return nil
	}

	onBotMessage := func(_ aiw.ConversationalContext, mex string) error {
		pw.PrintBotMessage(mex)
		return nil
	}

	onCustomerMessage := func(_ aiw.ConversationalContext, mex string) error {
		if *verboseFlag {
			logger.Debug().Str("message", mex).Msg("Customer message acknowledged")
		}
		return nil
	}

	onError := func(_ aiw.ConversationalContext, err error) {
		pw.PrintError(err)
	}

	onClose := func(_ aiw.ConversationalContext) error {
		pw.PrintInfo("🔌 Conversation stream closed by server.")
		finishSession()
		return nil
	}

	// 7. Open Conversation Stream
	pw.PrintInfo("🔌 Connecting to live conversation stream...")
	if err := convService.OpenConversation(convCtx, onOpen, onError, onCustomerMessage, onBotMessage, onClose); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to open conversation: %v\n", err)
		os.Exit(1)
	}

	// 8. Wait for session to be opened or interrupted
	select {
	case <-sessionReady:
	case <-ctx.Done():
		pw.PrintInfo("\n🛑 Connection canceled.")
		return
	case <-sessionClosed:
		pw.PrintInfo("\n🛑 Session ended before establishment.")
		return
	}

	// 9. Interactive CLI input loop
	scanner := bufio.NewScanner(os.Stdin)
	pw.PrintPrompt()

	inputCh := make(chan string)
	inputErrCh := make(chan error)

	go func() {
		for scanner.Scan() {
			inputCh <- scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			inputErrCh <- err
		} else {
			inputErrCh <- io.EOF
		}
	}()

	for {
		select {
		case <-ctx.Done():
			pw.PrintInfo("\n🛑 Termination signal received. Closing conversation...")
			_ = convService.CloseConversation(convCtx)
			pw.PrintInfo("👋 Goodbye!")
			return

		case <-sessionClosed:
			pw.PrintInfo("\n👋 Session finished. Goodbye!")
			return

		case err := <-inputErrCh:
			if err != io.EOF {
				pw.PrintInfo(fmt.Sprintf("\n❌ Input error: %v", err))
			} else {
				pw.PrintInfo("\n👋 EOF detected.")
			}
			pw.PrintInfo("Closing conversation...")
			_ = convService.CloseConversation(convCtx)
			pw.PrintInfo("👋 Goodbye!")
			return

		case input := <-inputCh:
			text := strings.TrimSpace(input)
			if text == "" {
				pw.PrintPrompt()
				continue
			}

			// Handle slash commands / exit
			lower := strings.ToLower(text)
			if lower == "/exit" || lower == "exit" || lower == "/quit" || lower == "quit" {
				pw.PrintInfo("👋 Closing conversation session...")
				if err := convService.CloseConversation(convCtx); err != nil {
					logger.Error().Err(err).Msg("failed to close conversation cleanly")
				}
				pw.PrintInfo("🔌 Conversation closed. Goodbye!")
				return
			}

			if lower == "/help" {
				pw.PrintInfo("\nℹ️  Session Info & Commands:")
				pw.PrintInfo(fmt.Sprintf("  Dialog ID   : %s", convCtx.DialogID()))
				pw.PrintInfo(fmt.Sprintf("  External ID : %s", convCtx.ExternalID()))
				pw.PrintInfo(fmt.Sprintf("  Tenant      : %s", convCtx.Tenant()))
				pw.PrintInfo(fmt.Sprintf("  Dialog Model: %s", convCtx.DialogModel()))
				pw.PrintInfo("  Commands    : '/help' (info), '/clear' (clear screen), '/exit' or 'exit' (quit)\n")
				pw.PrintPrompt()
				continue
			}

			if lower == "/clear" {
				fmt.Print("\033[H\033[2J")
				pw.PrintPrompt()
				continue
			}

			// Send customer message
			if err := convService.AddCustomerMessage(convCtx, text); err != nil {
				pw.PrintError(fmt.Errorf("failed to send message: %w", err))
			}
		}
	}
}
