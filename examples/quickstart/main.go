// Package main demonstrates the minimal, canonical way to connect to the AIW platform,
// establish a Server-Sent Events (SSE) conversational session, and exchange messages.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/morphy76/aiw-client/pkg/aiw"
)

func main() {
	// =========================================================================
	// Step 1: Initialize the AIW Client Facade
	// =========================================================================
	// The Client manages shared HTTP transports, timeout configurations, and
	// provides access to conversational and platform services.
	client, err := aiw.New(
		aiw.WithBaseURL(getEnvOrDefault("BASE_URL", "https://dev.lab.aiwave.io")),
		aiw.WithTimeout(30*time.Second),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize AIW client: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = client.Close() }()

	// =========================================================================
	// Step 2: Build the Conversational Context
	// =========================================================================
	// ConversationalContext binds a standard Go context.Context with customer
	// identity, tenant routing, and dialog model parameters.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	convCtx, err := aiw.NewConversationalContextBuilder().
		WithContext(ctx).
		WithExternalID("sample_user_001").
		WithDialogModel(getEnvOrDefault("DIALOG_MODEL_NAME", "RocchettoEmbeddingsV2")).
		WithBearerToken(os.Getenv("PAT")). // Bearer PAT token (tenant is automatically derived from token)
		Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build ConversationalContext: %v\n", err)
		os.Exit(1)
	}

	// =========================================================================
	// Step 3: Define Reactive Callbacks & Open the Conversation Stream
	// =========================================================================
	// AIW uses Server-Sent Events (SSE) for real-time duplex communication.
	// We register callbacks to handle lifecycle events and incoming AI responses.
	convService := client.Conversational()

	// Channel to signal when the conversation has completed
	doneCh := make(chan struct{})

	// onOpen: Triggered when the SSE transport connection is established (HTTP 200).
	onOpen := func(c aiw.ConversationalContext) error {
		fmt.Println("🔗 SSE transport stream connected.")
		return nil
	}

	// onCreated: Triggered when the AIW server assigns a Dialog ID ("created" event).
	onCreated := func(c aiw.ConversationalContext, dialogID string) error {
		fmt.Printf("✅ Connected! Dialog ID: %s\n", dialogID)
		fmt.Println("💬 Sending initial customer message...")

		// Send customer message once the dialog is created and active
		return convService.AddCustomerMessage(c, "Hello! How do I reset my password?")
	}

	// onError: Triggered if any stream, network, or server error occurs.
	onError := func(_ aiw.ConversationalContext, err error, cancel aiw.CancelStreamFunc) {
		fmt.Printf("❌ Error received: %v\n", err)
		cancel(true)
	}

	// onCustomerMessage: Triggered when the customer message is acknowledged.
	onCustomerMessage := func(_ aiw.ConversationalContext, msg string) error {
		fmt.Printf("👤 Customer Sent: %s\n", msg)
		return nil
	}

	// onBotMessage: Triggered whenever the AI bot returns an answer.
	onBotMessage := func(c aiw.ConversationalContext, answer string) error {
		fmt.Printf("🤖 Bot Response: %s\n", answer)

		// After receiving the answer, close the session
		fmt.Println("🔌 Closing conversation session...")
		return convService.CloseConversation(c)
	}

	// onDialogTerminated: Triggered when the dialog ends on the server side.
	onDialogTerminated := func(_ aiw.ConversationalContext, isAborted bool, reason string) error {
		if isAborted {
			fmt.Printf("⚠️ Dialog aborted by server: %s\n", reason)
		} else {
			fmt.Printf("ℹ️ Dialog closed cleanly: %s\n", reason)
		}
		return nil
	}

	// onClose: Triggered when the conversation is gracefully terminated.
	onClose := func(_ aiw.ConversationalContext) error {
		fmt.Println("👋 Conversation closed cleanly.")
		close(doneCh)
		return nil
	}

	// Connect to the live SSE stream
	fmt.Println("⏳ Connecting to AIW conversational stream...")
	err = convService.OpenConversation(
		convCtx,
		onOpen,
		onCreated,
		onError,
		onCustomerMessage,
		onBotMessage,
		onDialogTerminated,
		onClose,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open conversation: %v\n", err)
		os.Exit(1)
	}

	// Wait for the conversation to finish or timeout
	select {
	case <-doneCh:
		fmt.Println("✨ Quickstart example finished successfully.")
	case <-ctx.Done():
		fmt.Println("⚠️ Example timed out waiting for bot response.")
	}
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
