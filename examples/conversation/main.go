// Package main demonstrates a full-featured, interactive command-line chat application
// using the aiw-client library. It highlights:
// 1. Client initialization and ConversationalContext construction.
// 2. Listing past conversation sessions with ListSessions().
// 3. Restoring previous chat history with RestoreConversation().
// 4. Reactive SSE streaming with OpenConversation() and real-time callbacks.
// 5. Sending customer messages with AddCustomerMessage() and handling structured AI responses.
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

// -----------------------------------------------------------------------------
// Application Configuration
// -----------------------------------------------------------------------------

type Config struct {
	BaseURL     string
	BearerToken string
	Tenant      string
	DialogModel string
	ExternalID  string
	RestoreID   string
	Sandbox     bool
	Verbose     bool
}

func loadConfig() Config {
	baseURL := flag.String("url", getEnvOrDefault("BASE_URL", "https://dev.lab.aiwave.io"), "AIW platform base URL")
	token := flag.String("token", getEnvOrDefault("PAT", os.Getenv("BEARER_TOKEN")), "Bearer PAT Token")
	tenant := flag.String("tenant", getEnvOrDefault("TENANT", "almawave.com"), "Tenant ID (x-cognitive-system)")
	model := flag.String("model", getEnvOrDefault("DIALOG_MODEL_NAME", "RocchettoEmbeddingsV2"), "Target dialog model name")
	externalID := flag.String("external-id", getEnvOrDefault("EXTERNAL_ID", ""), "External customer user ID")
	restore := flag.String("restore", "", "Specific external ID to restore conversation history from")
	sandbox := flag.Bool("sandbox", getEnvBoolOrDefault("SANDBOX_MODE", false), "Enable cognitive sandbox mode")
	verbose := flag.Bool("verbose", getEnvBoolOrDefault("DEBUG", false), "Enable verbose debug logs")

	flag.Parse()

	return Config{
		BaseURL:     *baseURL,
		BearerToken: *token,
		Tenant:      *tenant,
		DialogModel: *model,
		ExternalID:  *externalID,
		RestoreID:   *restore,
		Sandbox:     *sandbox,
		Verbose:     *verbose,
	}
}

// -----------------------------------------------------------------------------
// Terminal Output Coordinator (Thread-Safe)
// -----------------------------------------------------------------------------

// terminalPrinter ensures asynchronous bot SSE responses and user prompts do not interleave on the CLI.
type terminalPrinter struct {
	mu sync.Mutex
}

func newTerminalPrinter() *terminalPrinter {
	return &terminalPrinter{}
}

func (p *terminalPrinter) Prompt() {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Print("💬 You > ")
}

func (p *terminalPrinter) BotMessage(rawMessage string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	answer, sources := parseBotAnswer(rawMessage)
	fmt.Printf("\r🤖 Bot > %s\n", answer)
	for i, s := range sources {
		if s.Title != "" {
			fmt.Printf("   📚 [%d] %s (%s)\n", i+1, s.Title, s.ID)
		} else {
			fmt.Printf("   📚 [%d] Source ID: %s\n", i+1, s.ID)
		}
	}
	fmt.Print("💬 You > ")
}

func (p *terminalPrinter) Error(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Printf("\r❌ Error: %v\n💬 You > ", err)
}

func (p *terminalPrinter) Info(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Printf("\r%s\n", msg)
}

// -----------------------------------------------------------------------------
// Main Entrypoint & Chat Session Orchestrator
// -----------------------------------------------------------------------------

func main() {
	cfg := loadConfig()
	logger := configureLogger(cfg.Verbose)

	printBanner(cfg)

	// Listen for OS interrupt / termination signals
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// 1. Initialize the AIW client facade
	client, err := aiw.New(
		aiw.WithBaseURL(cfg.BaseURL),
		aiw.WithLogger(logger),
		aiw.WithTimeout(60*time.Second),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to initialize AIW client: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = client.Close() }()

	convService := client.Conversational()
	printer := newTerminalPrinter()
	scanner := bufio.NewScanner(os.Stdin)

	// 2. Determine session ID (Start new vs Restore past session)
	externalID, shouldRestore := resolveSessionIdentity(ctx, cfg, convService, scanner)
	if externalID == "" {
		fmt.Println("👋 Exiting.")
		return
	}

	// 3. Build the ConversationalContext
	convCtx, err := aiw.NewConversationalContextBuilder().
		WithContext(ctx).
		WithExternalID(externalID).
		WithTenant(cfg.Tenant).
		WithDialogModel(cfg.DialogModel).
		WithBearerToken(cfg.BearerToken).
		WithSandbox(cfg.Sandbox).
		WithBaseURL(cfg.BaseURL).
		Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to build ConversationalContext: %v\n", err)
		os.Exit(1)
	}

	// 4. If restoring, fetch previous messages and display history
	if shouldRestore {
		printer.Info(fmt.Sprintf("⏳ Restoring conversation history for '%s'...", externalID))
		if history, err := convService.RestoreConversation(convCtx); err != nil {
			printer.Error(fmt.Errorf("could not restore history: %w", err))
		} else {
			renderConversationHistory(history)
		}
	}

	// 5. Open live SSE stream and register lifecycle callbacks
	sessionReady, sessionClosed, err := openStream(convService, convCtx, printer, cfg.Verbose, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to open live conversation: %v\n", err)
		os.Exit(1)
	}

	// Wait for the server to establish dialog session
	select {
	case <-sessionReady:
	case <-ctx.Done():
		printer.Info("\n🛑 Connection canceled.")
		return
	case <-sessionClosed:
		printer.Info("\n🛑 Session closed before establishment.")
		return
	}

	// 6. Run interactive CLI input loop
	runInteractiveChatLoop(ctx, convService, convCtx, printer, scanner, sessionClosed, cfg.DialogModel)
}

// -----------------------------------------------------------------------------
// Interactive Menu & Session Resolution
// -----------------------------------------------------------------------------

// resolveSessionIdentity decides whether to start a new chat or restore a past one based on flags or interactive menu.
func resolveSessionIdentity(
	ctx context.Context,
	cfg Config,
	convService aiw.ConversationalService,
	scanner *bufio.Scanner,
) (externalID string, shouldRestore bool) {
	if cfg.RestoreID != "" {
		return cfg.RestoreID, true
	}
	if cfg.ExternalID != "" {
		return cfg.ExternalID, false
	}

	fmt.Println("\nChoose session mode:")
	fmt.Println("  [1] 🆕 Start a new conversation")
	fmt.Println("  [2] 📋 List past sessions & select one to restore")
	fmt.Println("  [3] 🔍 Restore conversation by External ID")
	fmt.Println("  [q] 🚪 Quit")
	fmt.Print("\nChoice > ")

	if !scanner.Scan() {
		return "", false
	}

	switch strings.TrimSpace(scanner.Text()) {
	case "1":
		newID := generateRandomUserID()
		fmt.Printf("🆕 Generated new Session External ID: %s\n", newID)
		return newID, false

	case "2":
		return promptListAndSelectSession(ctx, convService, scanner, cfg.DialogModel)

	case "3":
		fmt.Print("Enter External ID to restore: ")
		if scanner.Scan() {
			id := strings.TrimSpace(scanner.Text())
			if id != "" {
				return id, true
			}
		}
		return generateRandomUserID(), false

	case "q", "quit", "exit":
		return "", false

	default:
		return generateRandomUserID(), false
	}
}

func promptListAndSelectSession(
	ctx context.Context,
	convService aiw.ConversationalService,
	scanner *bufio.Scanner,
	dialogModel string,
) (string, bool) {
	fmt.Print("Enter customer user ID prefix (or press Enter for all): ")
	var filter string
	if scanner.Scan() {
		filter = strings.TrimSpace(scanner.Text())
	}

	queryCtx := aiw.NewConversationalContext(ctx, filter)
	sessions, err := convService.ListSessions(queryCtx, aiw.ListSessionsQuery{
		AssistantName: dialogModel,
		Limit:         10,
		SortField:     "update_date",
		SortOrder:     "DESC",
	})
	if err != nil {
		fmt.Printf("❌ Failed to list sessions: %v\n", err)
		return generateRandomUserID(), false
	}

	if len(sessions) == 0 {
		fmt.Println("ℹ️  No past sessions found. Starting a new session instead.")
		return generateRandomUserID(), false
	}

	fmt.Println("\n📋 Past Sessions:")
	for i, s := range sessions {
		dateStr := s.StartTime.Format("2006-01-02 15:04")
		title := s.Title
		if title == "" {
			title = "(no title)"
		}
		fmt.Printf("  [%d] %-30s | %s | %s\n", i+1, s.ExternalID, dateStr, title)
	}

	fmt.Print("\nSelect session number to restore (or Enter to cancel): ")
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if idx, err := strconv.Atoi(input); err == nil && idx >= 1 && idx <= len(sessions) {
			return sessions[idx-1].ExternalID, true
		}
	}

	return generateRandomUserID(), false
}

// -----------------------------------------------------------------------------
// SSE Stream Setup & Reactive Callbacks
// -----------------------------------------------------------------------------

func openStream(
	convService aiw.ConversationalService,
	convCtx aiw.ConversationalContext,
	printer *terminalPrinter,
	verbose bool,
	logger zerolog.Logger,
) (readyCh <-chan struct{}, closedCh <-chan struct{}, err error) {
	ready := make(chan struct{})
	closed := make(chan struct{})
	var closeOnce sync.Once

	finish := func() {
		closeOnce.Do(func() {
			close(closed)
		})
	}

	onOpen := func(c aiw.ConversationalContext) error {
		printer.Info(fmt.Sprintf("🔗 Session Connected! Dialog ID: %s", c.DialogID()))
		printer.Info("💡 Type a message and press Enter to chat.")
		printer.Info("💡 Commands: '/history', '/sessions', '/help', '/clear', '/exit'")
		printer.Info("--------------------------------------------------------------------------------")
		close(ready)
		return nil
	}

	onBotMessage := func(_ aiw.ConversationalContext, msg string) error {
		printer.BotMessage(msg)
		return nil
	}

	onCustomerMessage := func(_ aiw.ConversationalContext, msg string) error {
		if verbose {
			logger.Debug().Str("message", msg).Msg("Customer message acknowledged by server")
		}
		return nil
	}

	onError := func(_ aiw.ConversationalContext, err error) {
		printer.Error(err)
	}

	onClose := func(_ aiw.ConversationalContext) error {
		printer.Info("🔌 Conversation stream closed by server.")
		finish()
		return nil
	}

	printer.Info("🔌 Connecting to live conversation stream...")
	err = convService.OpenConversation(convCtx, onOpen, onError, onCustomerMessage, onBotMessage, onClose)
	return ready, closed, err
}

// -----------------------------------------------------------------------------
// Interactive Chat Loop
// -----------------------------------------------------------------------------

func runInteractiveChatLoop(
	ctx context.Context,
	convService aiw.ConversationalService,
	convCtx aiw.ConversationalContext,
	printer *terminalPrinter,
	scanner *bufio.Scanner,
	sessionClosed <-chan struct{},
	dialogModel string,
) {
	printer.Prompt()

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
			printer.Info("\n🛑 Signal received. Closing conversation...")
			_ = convService.CloseConversation(convCtx)
			printer.Info("👋 Goodbye!")
			return

		case <-sessionClosed:
			printer.Info("\n👋 Session ended. Goodbye!")
			return

		case err := <-inputErrCh:
			if err != io.EOF {
				printer.Info(fmt.Sprintf("\n❌ Input error: %v", err))
			}
			printer.Info("Closing conversation...")
			_ = convService.CloseConversation(convCtx)
			printer.Info("👋 Goodbye!")
			return

		case input := <-inputCh:
			text := strings.TrimSpace(input)
			if text == "" {
				printer.Prompt()
				continue
			}

			// Handle slash commands
			if handleSlashCommand(convService, convCtx, printer, text, dialogModel) {
				return
			}

			// Dispatch customer message
			if err := convService.AddCustomerMessage(convCtx, text); err != nil {
				printer.Error(fmt.Errorf("failed to send message: %w", err))
			}
		}
	}
}

// handleSlashCommand processes commands like /exit, /history, /sessions, /help. Returns true if application should exit.
func handleSlashCommand(
	convService aiw.ConversationalService,
	convCtx aiw.ConversationalContext,
	printer *terminalPrinter,
	text string,
	dialogModel string,
) (shouldExit bool) {
	lower := strings.ToLower(text)

	switch lower {
	case "/exit", "exit", "/quit", "quit":
		printer.Info("👋 Closing conversation session...")
		_ = convService.CloseConversation(convCtx)
		printer.Info("🔌 Session closed. Goodbye!")
		return true

	case "/history":
		history, err := convService.RestoreConversation(convCtx)
		if err != nil {
			printer.Error(fmt.Errorf("failed to fetch history: %w", err))
		} else {
			renderConversationHistory(history)
		}
		printer.Prompt()
		return false

	case "/sessions":
		sessions, err := convService.ListSessions(convCtx, aiw.ListSessionsQuery{
			AssistantName: dialogModel,
			Limit:         10,
		})
		if err != nil {
			printer.Error(fmt.Errorf("failed to list sessions: %w", err))
		} else {
			printer.Info("\n📋 Recent Past Sessions:")
			for i, s := range sessions {
				dateStr := s.StartTime.Format("2006-01-02 15:04")
				printer.Info(fmt.Sprintf("  [%d] %-30s | %s | %s", i+1, s.ExternalID, dateStr, s.Title))
			}
			printer.Info("")
		}
		printer.Prompt()
		return false

	case "/help":
		printer.Info("\nℹ️  Session Info & Available Commands:")
		printer.Info(fmt.Sprintf("  Dialog ID   : %s", convCtx.DialogID()))
		printer.Info(fmt.Sprintf("  External ID : %s", convCtx.ExternalID()))
		printer.Info(fmt.Sprintf("  Tenant      : %s", convCtx.Tenant()))
		printer.Info(fmt.Sprintf("  Dialog Model: %s", convCtx.DialogModel()))
		printer.Info("  Commands    : '/history' (view history), '/sessions' (list past chats), '/clear' (clear screen), '/exit' (quit)\n")
		printer.Prompt()
		return false

	case "/clear":
		fmt.Print("\033[H\033[2J")
		printer.Prompt()
		return false

	default:
		return false
	}
}

// -----------------------------------------------------------------------------
// Formatters and Helpers
// -----------------------------------------------------------------------------

type structuredBotPayload struct {
	Answer  string `json:"answer"`
	Sources []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"sources"`
}

func parseBotAnswer(raw string) (string, []aiw.Source) {
	var payload structuredBotPayload
	if err := json.Unmarshal([]byte(raw), &payload); err == nil && (payload.Answer != "" || len(payload.Sources) > 0) {
		sources := make([]aiw.Source, 0, len(payload.Sources))
		for _, s := range payload.Sources {
			sources = append(sources, aiw.Source{ID: s.ID, Title: s.Title})
		}
		return payload.Answer, sources
	}
	return raw, nil
}

func renderConversationHistory(messages []aiw.Message) {
	if len(messages) == 0 {
		fmt.Println("ℹ️  No previous messages recorded in this session.")
		return
	}
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("📜 Restored Conversation History:")
	fmt.Println("--------------------------------------------------------------------------------")
	for _, m := range messages {
		tsStr := m.Timestamp.Format("2006-01-02 15:04:05")
		if m.Sender == "CUSTOMER" {
			fmt.Printf("  [%s] 👤 You: %s\n", tsStr, m.Content)
		} else {
			if m.Answer != nil {
				fmt.Printf("  [%s] 🤖 Bot: %s\n", tsStr, m.Answer.Text)
				for idx, src := range m.Answer.Sources {
					fmt.Printf("          📚 [%d] %s (%s)\n", idx+1, src.Title, src.ID)
				}
			} else {
				answer, sources := parseBotAnswer(m.Content)
				fmt.Printf("  [%s] 🤖 Bot: %s\n", tsStr, answer)
				for idx, src := range sources {
					fmt.Printf("          📚 [%d] %s (%s)\n", idx+1, src.Title, src.ID)
				}
			}
		}
	}
	fmt.Println("--------------------------------------------------------------------------------")
}

func generateRandomUserID() string {
	return fmt.Sprintf("user_%d_%04d", time.Now().Unix(), rand.Intn(10000))
}

func printBanner(cfg Config) {
	fmt.Println("================================================================================")
	fmt.Println("💬 AIW Interactive CLI Chat & Session Manager")
	fmt.Println("================================================================================")
	fmt.Printf("  Base URL    : %s\n", cfg.BaseURL)
	fmt.Printf("  Tenant      : %s\n", cfg.Tenant)
	fmt.Printf("  Dialog Model: %s\n", cfg.DialogModel)
	fmt.Printf("  Sandbox     : %t\n", cfg.Sandbox)
	fmt.Println("================================================================================")

	if cfg.BearerToken == "" {
		fmt.Println("⚠️  Warning: PAT (Bearer Token) is missing. Live requests may return 401 Unauthorized.")
		fmt.Println("   Pass -token <PAT> or set the PAT environment variable.")
	}
}

func configureLogger(verbose bool) zerolog.Logger {
	if !verbose {
		return zerolog.Nop()
	}
	output := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
	}
	return zerolog.New(output).With().Timestamp().Logger().Level(zerolog.DebugLevel)
}

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
