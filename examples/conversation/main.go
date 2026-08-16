// Package main demonstrates a full-featured, interactive command-line chat application
// using the aiw-client library. It highlights:
// 1. Client initialization and ConversationalContext construction via CLI flags and interactive prompts.
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

	"github.com/google/uuid"
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
	Username    string
	ExternalID  string
	RestoreID   string
	Sandbox     bool
	Verbose     bool
}

func parseFlags() Config {
	baseURL := flag.String("url", "https://portal.aiwave.ai", "AIW platform base URL (e.g. https://portal.aiwave.ai)")
	token := flag.String("token", "", "Bearer PAT Token")
	model := flag.String("model", "", "Target dialog model name (e.g. Rocchetto)")
	user := flag.String("user", "", "Customer username (forms <tenant>-<username>-<model>)")
	username := flag.String("username", "", "Alias for -user")
	externalID := flag.String("external-id", "", "Explicit full External customer user ID")
	restore := flag.String("restore", "", "Specific external ID to restore conversation history from")
	sandbox := flag.Bool("sandbox", false, "Enable cognitive sandbox mode")
	verbose := flag.Bool("verbose", false, "Enable verbose debug logs")

	flag.Parse()

	u := *user
	if u == "" {
		u = *username
	}
	if u == "" {
		u = *externalID
	}
	if u == "" && *token != "" {
		u = aiw.ExtractUsernameFromToken(*token)
	}

	tenant := ""
	if *token != "" {
		tenant = aiw.ExtractTenantFromToken(*token)
	}

	return Config{
		BaseURL:     *baseURL,
		BearerToken: *token,
		Tenant:      tenant,
		DialogModel: *model,
		Username:    u,
		ExternalID:  *externalID,
		RestoreID:   *restore,
		Sandbox:     *sandbox,
		Verbose:     *verbose,
	}
}

func ensureConfig(scanner *bufio.Scanner, cfg *Config) bool {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		fmt.Print("Enter AIW Base URL [https://portal.aiwave.ai]: ")
		if !scanner.Scan() {
			return false
		}
		val := strings.TrimSpace(scanner.Text())
		if val == "" {
			val = "https://portal.aiwave.ai"
		}
		cfg.BaseURL = val
	}

	if strings.TrimSpace(cfg.BearerToken) == "" {
		for {
			fmt.Print("Enter Bearer PAT Token: ")
			if !scanner.Scan() {
				return false
			}
			val := strings.TrimSpace(scanner.Text())
			if val != "" {
				cfg.BearerToken = val
				break
			}
			fmt.Println("❌ Bearer PAT Token is required.")
		}
	}

	if strings.TrimSpace(cfg.Tenant) == "" && cfg.BearerToken != "" {
		cfg.Tenant = aiw.ExtractTenantFromToken(cfg.BearerToken)
	}
	if cfg.Tenant == "" {
		cfg.Tenant = "default"
	}

	if strings.TrimSpace(cfg.Username) == "" && cfg.BearerToken != "" {
		cfg.Username = aiw.ExtractUsernameFromToken(cfg.BearerToken)
	}

	if strings.TrimSpace(cfg.DialogModel) == "" {
		fmt.Print("Enter Dialog Model Name [Rocchetto]: ")
		if !scanner.Scan() {
			return false
		}
		val := strings.TrimSpace(scanner.Text())
		if val == "" {
			val = "Rocchetto"
		}
		cfg.DialogModel = val
	}

	return true
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
	cfg := parseFlags()
	scanner := bufio.NewScanner(os.Stdin)

	// Ensure core configuration parameters are provided before entering the app
	if !ensureConfig(scanner, &cfg) {
		fmt.Println("👋 Exiting.")
		return
	}

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

	// Main session loop (allows returning to menu after /close)
	for {
		select {
		case <-ctx.Done():
			fmt.Println("👋 Exiting.")
			return
		default:
		}

		// 2. Determine session ID (Start new vs Restore past session)
		externalID, shouldRestore := resolveSessionIdentity(ctx, cfg, convService, scanner)
		if externalID == "" {
			fmt.Println("👋 Exiting.")
			return
		}

		// Clear one-shot restore flag after first resolution
		cfg.RestoreID = ""

		// 3. Build the ConversationalContext
		convCtx, err := aiw.NewConversationalContextBuilder().
			WithContext(ctx).
			WithExternalID(externalID).
			WithDialogModel(cfg.DialogModel).
			WithBearerToken(cfg.BearerToken).
			WithSandbox(cfg.Sandbox).
			Build()
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed to build ConversationalContext: %v\n", err)
			continue
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
			continue
		}

		// Wait for the server to establish dialog session
		established := false
		select {
		case <-sessionReady:
			established = true
		case <-ctx.Done():
			printer.Info("\n🛑 Connection canceled.")
			return
		case <-sessionClosed:
			printer.Info("\n🛑 Session closed before establishment.")
		}

		if !established {
			continue
		}

		// 6. Run interactive CLI input loop
		exitApp := runInteractiveChatLoop(ctx, convService, convCtx, printer, scanner, sessionClosed, cfg.DialogModel)
		if exitApp {
			fmt.Println("👋 Exiting.")
			return
		}
	}
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
		return buildExternalID(cfg.Tenant, cfg.RestoreID, cfg.DialogModel), true
	}

	for {
		fmt.Println("\nChoose session mode:")
		fmt.Println("  [1] 🆕 Start a new conversation")
		fmt.Println("  [2] 📋 List past sessions & select one to restore")
		fmt.Println("  [3] 🔍 Restore conversation by Username / External ID")
		fmt.Println("  [q] 🚪 Quit")
		fmt.Print("\nChoice > ")

		if !scanner.Scan() {
			return "", false
		}

		switch strings.TrimSpace(scanner.Text()) {
		case "1":
			user := cfg.Username
			if user == "" {
				user = generateRandomUserID()
			}
			extID := buildNewChatExternalID(cfg.Tenant, user, cfg.DialogModel)
			fmt.Printf("🆕 Starting conversation with Session External ID: %s\n", extID)
			return extID, false

		case "2":
			selectedID, ok := promptListAndSelectSession(ctx, cfg, convService, scanner)
			if ok {
				return selectedID, true
			}
			continue

		case "3":
			fmt.Print("Enter Username or External ID to restore: ")
			if scanner.Scan() {
				id := strings.TrimSpace(scanner.Text())
				if id != "" {
					return buildExternalID(cfg.Tenant, id, cfg.DialogModel), true
				}
			}
			fmt.Println("⚠️ No Username / External ID provided.")
			continue

		case "q", "quit", "exit":
			return "", false

		default:
			fmt.Println("❌ Invalid choice. Please select 1, 2, 3, or q.")
		}
	}
}

func promptListAndSelectSession(
	ctx context.Context,
	cfg Config,
	convService aiw.ConversationalService,
	scanner *bufio.Scanner,
) (string, bool) {
	filterExternalID := ""
	if cfg.Username != "" {
		filterExternalID = buildExternalID(cfg.Tenant, cfg.Username, cfg.DialogModel)
	}

	queryCtx, err := aiw.NewConversationalContextBuilder().
		WithContext(ctx).
		WithExternalID(filterExternalID).
		WithBearerToken(cfg.BearerToken).
		WithDialogModel(cfg.DialogModel).
		WithSandbox(cfg.Sandbox).
		Build()
	if err != nil {
		fmt.Printf("❌ Failed to build query context: %v\n", err)
		return "", false
	}

	if filterExternalID != "" {
		fmt.Printf("⏳ Retrieving past sessions for '%s'...\n", filterExternalID)
	} else {
		fmt.Println("⏳ Retrieving past sessions...")
	}

	sessions, err := convService.ListSessions(queryCtx, aiw.ListSessionsQuery{
		Limit:     10,
		SortField: "update_date",
		SortOrder: "DESC",
	})
	if err != nil {
		fmt.Printf("❌ Failed to list sessions: %v\n", err)
		return "", false
	}

	if len(sessions) == 0 {
		fmt.Println("ℹ️  No past sessions found.")
		return "", false
	}

	fmt.Println("\n📋 Past Sessions:")
	for i, s := range sessions {
		dateStr := s.StartTime.Format("2006-01-02 15:04")
		title := s.Title
		if title == "" {
			title = "(no title)"
		}
		fmt.Printf("  [%d] (ID: %d) %-40s | %s | %s\n", i+1, s.ID, s.ExternalID, dateStr, title)
	}

	fmt.Print("\nSelect session number to restore (or Enter to cancel): ")
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if idx, err := strconv.Atoi(input); err == nil && idx >= 1 && idx <= len(sessions) {
			return sessions[idx-1].ExternalID, true
		}
	}

	return "", false
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
		printer.Info("💡 Commands: '/close', '/history', '/sessions', '/help', '/clear', '/exit'")
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

type chatAction int

const (
	actionSendMessage chatAction = iota
	actionHandledStayInChat
	actionReturnToMenu
	actionExitApp
)

func runInteractiveChatLoop(
	ctx context.Context,
	convService aiw.ConversationalService,
	convCtx aiw.ConversationalContext,
	printer *terminalPrinter,
	scanner *bufio.Scanner,
	sessionClosed <-chan struct{},
	dialogModel string,
) bool {
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
			return true

		case <-sessionClosed:
			printer.Info("\n👋 Session ended. Returning to menu...")
			return false

		case err := <-inputErrCh:
			if err != io.EOF {
				printer.Info(fmt.Sprintf("\n❌ Input error: %v", err))
			}
			printer.Info("Closing conversation...")
			_ = convService.CloseConversation(convCtx)
			return true

		case input := <-inputCh:
			text := strings.TrimSpace(input)
			if text == "" {
				printer.Prompt()
				continue
			}

			// Handle in-chat slash commands
			action := handleSlashCommand(convService, convCtx, printer, text, dialogModel)
			switch action {
			case actionExitApp:
				return true
			case actionReturnToMenu:
				return false
			case actionHandledStayInChat:
				continue
			case actionSendMessage:
				// Dispatch customer message
				if err := convService.AddCustomerMessage(convCtx, text); err != nil {
					printer.Error(fmt.Errorf("failed to send message: %w", err))
				}
			}
		}
	}
}

// handleSlashCommand processes in-chat slash commands (e.g. /close, /exit, /history, /sessions, /help, /clear).
// If the input is not a slash command, it returns actionSendMessage.
func handleSlashCommand(
	convService aiw.ConversationalService,
	convCtx aiw.ConversationalContext,
	printer *terminalPrinter,
	text string,
	dialogModel string,
) chatAction {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "/") {
		return actionSendMessage
	}

	lower := strings.ToLower(trimmed)
	switch lower {
	case "/close":
		printer.Info("👋 Closing conversation and returning to menu...")
		_ = convService.CloseConversation(convCtx)
		return actionReturnToMenu

	case "/exit", "/quit":
		printer.Info("👋 Closing conversation session...")
		_ = convService.CloseConversation(convCtx)
		printer.Info("🔌 Session closed. Goodbye!")
		return actionExitApp

	case "/history":
		history, err := convService.RestoreConversation(convCtx)
		if err != nil {
			printer.Error(fmt.Errorf("failed to fetch history: %w", err))
		} else {
			renderConversationHistory(history)
		}
		printer.Prompt()
		return actionHandledStayInChat

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
		return actionHandledStayInChat

	case "/help":
		printer.Info("\nℹ️  Session Info & Available Commands:")
		printer.Info(fmt.Sprintf("  Dialog ID   : %s", convCtx.DialogID()))
		printer.Info(fmt.Sprintf("  External ID : %s", convCtx.ExternalID()))
		printer.Info(fmt.Sprintf("  Tenant      : %s", convCtx.Tenant()))
		printer.Info(fmt.Sprintf("  Dialog Model: %s", convCtx.DialogModel()))
		printer.Info("  Commands    : '/close' (close & return to menu), '/history' (view history), '/sessions' (list past chats), '/clear' (clear screen), '/exit' (quit application)\n")
		printer.Prompt()
		return actionHandledStayInChat

	case "/clear":
		fmt.Print("\033[H\033[2J")
		printer.Prompt()
		return actionHandledStayInChat

	default:
		printer.Info(fmt.Sprintf("⚠️ Unknown command '%s'. Type '/help' for available commands.", trimmed))
		printer.Prompt()
		return actionHandledStayInChat
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

func buildExternalID(tenant, user, model string) string {
	tenant = strings.TrimSpace(tenant)
	user = strings.TrimSpace(user)
	model = strings.TrimSpace(model)
	if user == "" {
		return ""
	}
	if strings.HasPrefix(user, tenant+"-") && strings.HasSuffix(user, "-"+model) {
		return user
	}
	return fmt.Sprintf("%s-%s-%s", tenant, user, model)
}

func buildNewChatExternalID(tenant, user, model string) string {
	base := buildExternalID(tenant, user, model)
	return fmt.Sprintf("%s-%s", base, uuid.NewString())
}

func printBanner(cfg Config) {
	fmt.Println("================================================================================")
	fmt.Println("💬 AIW Interactive CLI Chat & Session Manager")
	fmt.Println("================================================================================")
	fmt.Printf("  Base URL    : %s\n", cfg.BaseURL)
	fmt.Printf("  Tenant      : %s\n", cfg.Tenant)
	fmt.Printf("  Dialog Model: %s\n", cfg.DialogModel)
	if cfg.Username != "" {
		fmt.Printf("  Username    : %s\n", cfg.Username)
		fmt.Printf("  External ID : %s\n", buildExternalID(cfg.Tenant, cfg.Username, cfg.DialogModel))
	}
	fmt.Printf("  Sandbox     : %t\n", cfg.Sandbox)
	if len(cfg.BearerToken) > 8 {
		fmt.Printf("  Token       : %s...%s\n", cfg.BearerToken[:4], cfg.BearerToken[len(cfg.BearerToken)-4:])
	} else if cfg.BearerToken != "" {
		fmt.Printf("  Token       : [provided]\n")
	}
	fmt.Println("================================================================================")
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
