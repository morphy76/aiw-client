package main

import (
	"bufio"
	"context"
	"encoding/json"
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

	answer, sources := parseBotPayload(msg)
	fmt.Printf("\r🤖 Bot > %s\n", answer)
	if len(sources) > 0 {
		for i, s := range sources {
			if s.Title != "" {
				fmt.Printf("   📚 [%d] %s (%s)\n", i+1, s.Title, s.ID)
			} else {
				fmt.Printf("   📚 [%d] Source ID: %s\n", i+1, s.ID)
			}
		}
	}
	fmt.Print("💬 You > ")
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

type botPayloadJSON struct {
	Answer  string `json:"answer"`
	Sources []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"sources"`
}

func parseBotPayload(raw string) (string, []aiw.Source) {
	var payload botPayloadJSON
	if err := json.Unmarshal([]byte(raw), &payload); err == nil && (payload.Answer != "" || len(payload.Sources) > 0) {
		sources := make([]aiw.Source, 0, len(payload.Sources))
		for _, s := range payload.Sources {
			sources = append(sources, aiw.Source{ID: s.ID, Title: s.Title})
		}
		return payload.Answer, sources
	}
	return raw, nil
}

func printHistory(messages []aiw.Message) {
	if len(messages) == 0 {
		fmt.Println("ℹ️  No previous messages recorded in this session.")
		return
	}
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("📜 Conversation History:")
	fmt.Println("--------------------------------------------------------------------------------")
	for _, m := range messages {
		tsStr := m.Timestamp.Format("2006-01-02 15:04:05")
		if m.Sender == "CUSTOMER" {
			fmt.Printf("  [%s] 👤 You: %s\n", tsStr, m.Content)
		} else {
			if m.Answer != nil {
				fmt.Printf("  [%s] 🤖 Bot: %s\n", tsStr, m.Answer.Text)
				if len(m.Answer.Sources) > 0 {
					for idx, src := range m.Answer.Sources {
						fmt.Printf("          📚 [%d] %s (%s)\n", idx+1, src.Title, src.ID)
					}
				}
			} else {
				answer, sources := parseBotPayload(m.Content)
				fmt.Printf("  [%s] 🤖 Bot: %s\n", tsStr, answer)
				for idx, src := range sources {
					fmt.Printf("          📚 [%d] %s (%s)\n", idx+1, src.Title, src.ID)
				}
			}
		}
	}
	fmt.Println("--------------------------------------------------------------------------------")
}

func main() {
	// 1. Command-line flags and environment fallbacks
	baseURLFlag := flag.String("url", getEnvOrDefault("BASE_URL", "https://dev.lab.aiwave.io"), "AIW Base URL")
	patFlag := flag.String("token", getEnvOrDefault("PAT", os.Getenv("BEARER_TOKEN")), "Bearer PAT Token")
	tenantFlag := flag.String("tenant", getEnvOrDefault("TENANT", "almawave.com"), "Tenant ID (x-cognitive-system)")
	modelFlag := flag.String("model", getEnvOrDefault("DIALOG_MODEL_NAME", "RocchettoEmbeddingsV2"), "Dialog model name")
	externalIDFlag := flag.String("external-id", getEnvOrDefault("EXTERNAL_ID", ""), "External customer user ID")
	restoreFlag := flag.String("restore", "", "Specific external ID to restore conversation history from")
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

	fmt.Println("================================================================================")
	fmt.Println("💬 AIW Interactive CLI Chat & Session Manager")
	fmt.Println("================================================================================")
	fmt.Printf("  Base URL    : %s\n", *baseURLFlag)
	fmt.Printf("  Tenant      : %s\n", *tenantFlag)
	fmt.Printf("  Dialog Model: %s\n", *modelFlag)
	fmt.Printf("  Sandbox     : %t\n", *sandboxFlag)
	fmt.Println("================================================================================")

	if *patFlag == "" {
		fmt.Println("⚠️  Warning: PAT (Bearer Token) is missing. Live requests may return 401 Unauthorized.")
		fmt.Println("   Use -token <PAT> to authenticate.")
	}

	// 3. Setup root context with OS signals
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// 4. Build AIW client facade
	client, err := aiw.New(
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
	scanner := bufio.NewScanner(os.Stdin)

	externalID := *externalIDFlag
	shouldRestore := *restoreFlag != ""
	if shouldRestore {
		externalID = *restoreFlag
	}

	// 5. If external ID or restore not explicitly specified, show interactive session menu
	if externalID == "" {
		fmt.Println("\nSelect an option:")
		fmt.Println("  [1] Start a New Conversation")
		fmt.Println("  [2] List Past Sessions & Select One to Restore")
		fmt.Println("  [3] Restore Conversation by External ID")
		fmt.Println("  [q] Quit")
		fmt.Print("\nChoice > ")

		if scanner.Scan() {
			choice := strings.TrimSpace(scanner.Text())
			switch choice {
			case "1":
				externalID = fmt.Sprintf("user_%d_%04d", time.Now().Unix(), rand.Intn(10000))
				fmt.Printf("🆕 Starting new session with External ID: %s\n", externalID)

			case "2":
				fmt.Print("Enter username/external ID prefix to list (or press Enter for default prefix): ")
				var userFilter string
				if scanner.Scan() {
					userFilter = strings.TrimSpace(scanner.Text())
				}
				queryCtx := aiw.NewConversationalContext(ctx, userFilter)
				sessions, err := convService.ListSessions(queryCtx, aiw.ListSessionsQuery{
					AssistantName: *modelFlag,
					Limit:         10,
				})
				if err != nil {
					fmt.Printf("❌ Failed to list sessions: %v\n", err)
					externalID = fmt.Sprintf("user_%d_%04d", time.Now().Unix(), rand.Intn(10000))
				} else if len(sessions) == 0 {
					fmt.Println("ℹ️  No previous sessions found. Starting a new session instead.")
					externalID = fmt.Sprintf("user_%d_%04d", time.Now().Unix(), rand.Intn(10000))
				} else {
					fmt.Println("\n📋 Past Sessions:")
					for i, s := range sessions {
						dateStr := s.StartTime.Format("2006-01-02 15:04")
						title := s.Title
						if title == "" {
							title = "(no title)"
						}
						fmt.Printf("  [%d] %-30s | %s | %s\n", i+1, s.ExternalID, dateStr, title)
					}
					fmt.Print("\nSelect session number to restore (or enter to cancel): ")
					if scanner.Scan() {
						idxInput := strings.TrimSpace(scanner.Text())
						if idx, err := strconv.Atoi(idxInput); err == nil && idx >= 1 && idx <= len(sessions) {
							externalID = sessions[idx-1].ExternalID
							shouldRestore = true
						} else {
							externalID = fmt.Sprintf("user_%d_%04d", time.Now().Unix(), rand.Intn(10000))
						}
					}
				}

			case "3":
				fmt.Print("Enter External ID to restore: ")
				if scanner.Scan() {
					externalID = strings.TrimSpace(scanner.Text())
					shouldRestore = true
				}
				if externalID == "" {
					externalID = fmt.Sprintf("user_%d_%04d", time.Now().Unix(), rand.Intn(10000))
					shouldRestore = false
				}

			case "q", "quit", "exit":
				fmt.Println("👋 Goodbye!")
				return

			default:
				externalID = fmt.Sprintf("user_%d_%04d", time.Now().Unix(), rand.Intn(10000))
			}
		}
	}

	// 6. Build ConversationalContext
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

	// 7. If restoring, fetch history before opening live stream
	if shouldRestore {
		fmt.Printf("⏳ Restoring conversation history for '%s'...\n", externalID)
		history, err := convService.RestoreConversation(convCtx)
		if err != nil {
			fmt.Printf("⚠️  Could not restore history: %v\n", err)
		} else {
			printHistory(history)
		}
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

	// 8. Reactive lifecycle callbacks
	onOpen := func(c aiw.ConversationalContext) error {
		pw.PrintInfo(fmt.Sprintf("🔗 Connected! Session Dialog ID: %s", c.DialogID()))
		pw.PrintInfo("💡 Type your message and press Enter to chat.")
		pw.PrintInfo("💡 Commands: '/help', '/history', '/sessions', '/exit'")
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

	// 9. Open Conversation Stream
	pw.PrintInfo("🔌 Connecting to live conversation stream...")
	if err := convService.OpenConversation(convCtx, onOpen, onError, onCustomerMessage, onBotMessage, onClose); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to open conversation: %v\n", err)
		os.Exit(1)
	}

	// 10. Wait for session to be opened or interrupted
	select {
	case <-sessionReady:
	case <-ctx.Done():
		pw.PrintInfo("\n🛑 Connection canceled.")
		return
	case <-sessionClosed:
		pw.PrintInfo("\n🛑 Session ended before establishment.")
		return
	}

	// 11. Interactive CLI input loop
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

			lower := strings.ToLower(text)
			if lower == "/exit" || lower == "exit" || lower == "/quit" || lower == "quit" {
				pw.PrintInfo("👋 Closing conversation session...")
				if err := convService.CloseConversation(convCtx); err != nil {
					logger.Error().Err(err).Msg("failed to close conversation cleanly")
				}
				pw.PrintInfo("🔌 Conversation closed. Goodbye!")
				return
			}

			if lower == "/history" {
				history, err := convService.RestoreConversation(convCtx)
				if err != nil {
					pw.PrintError(fmt.Errorf("failed to fetch history: %w", err))
				} else {
					printHistory(history)
				}
				pw.PrintPrompt()
				continue
			}

			if lower == "/sessions" {
				sessions, err := convService.ListSessions(convCtx, aiw.ListSessionsQuery{
					AssistantName: *modelFlag,
					Limit:         10,
				})
				if err != nil {
					pw.PrintError(fmt.Errorf("failed to list sessions: %w", err))
				} else {
					pw.PrintInfo("\n📋 Recent Past Sessions:")
					for i, s := range sessions {
						dateStr := s.StartTime.Format("2006-01-02 15:04")
						pw.PrintInfo(fmt.Sprintf("  [%d] %-30s | %s | %s", i+1, s.ExternalID, dateStr, s.Title))
					}
					pw.PrintInfo("")
				}
				pw.PrintPrompt()
				continue
			}

			if lower == "/help" {
				pw.PrintInfo("\nℹ️  Session Info & Commands:")
				pw.PrintInfo(fmt.Sprintf("  Dialog ID   : %s", convCtx.DialogID()))
				pw.PrintInfo(fmt.Sprintf("  External ID : %s", convCtx.ExternalID()))
				pw.PrintInfo(fmt.Sprintf("  Tenant      : %s", convCtx.Tenant()))
				pw.PrintInfo(fmt.Sprintf("  Dialog Model: %s", convCtx.DialogModel()))
				pw.PrintInfo("  Commands    : '/history', '/sessions', '/help', '/clear', '/exit'\n")
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
