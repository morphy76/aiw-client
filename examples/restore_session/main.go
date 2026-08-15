// Package main demonstrates how to list past user sessions and restore
// full conversation history (turns, answers, citations) using the AIW client.
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
	// Step 1: Initialize the AIW Client
	// =========================================================================
	client, err := aiw.New(
		aiw.WithBaseURL(getEnvOrDefault("BASE_URL", "https://dev.lab.aiwave.io")),
		aiw.WithTimeout(30*time.Second),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize AIW client: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = client.Close() }()

	convService := client.Conversational()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// External user ID whose sessions we want to query and restore
	targetUser := getEnvOrDefault("EXTERNAL_ID", "sample_user_001")
	assistantModel := getEnvOrDefault("DIALOG_MODEL_NAME", "RocchettoEmbeddingsV2")

	// =========================================================================
	// Step 2: List Past User Sessions / Activities
	// =========================================================================
	// ListSessions retrieves past conversation metadata such as ExternalID,
	// Session Title, and Start Timestamp.
	fmt.Printf("🔍 Querying past sessions for user '%s'...\n", targetUser)

	queryCtx := aiw.NewConversationalContext(ctx, targetUser)
	sessions, err := convService.ListSessions(queryCtx, aiw.ListSessionsQuery{
		AssistantName: assistantModel,
		Limit:         5,
		SortField:     "update_date",
		SortOrder:     "DESC",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list sessions: %v\n", err)
		os.Exit(1)
	}

	if len(sessions) == 0 {
		fmt.Println("ℹ️  No previous sessions found for this user.")
		return
	}

	fmt.Printf("📋 Found %d recent session(s):\n", len(sessions))
	for i, s := range sessions {
		fmt.Printf("  [%d] External ID : %s\n", i+1, s.ExternalID)
		fmt.Printf("      Title       : %s\n", s.Title)
		fmt.Printf("      Started At  : %s\n\n", s.StartTime.Format(time.RFC3339))
	}

	// =========================================================================
	// Step 3: Restore Conversation History for a Selected Session
	// =========================================================================
	// RestoreConversation retrieves the XML recording data from AIW and returns
	// all historical turns, customer queries, bot answers, and document citations.
	selectedSessionID := sessions[0].ExternalID
	fmt.Printf("⏳ Restoring conversation history for session: %s\n", selectedSessionID)

	restoreCtx, err := aiw.NewConversationalContextBuilder().
		WithContext(ctx).
		WithExternalID(selectedSessionID).
		WithBearerToken(os.Getenv("PAT")).
		Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build restore context: %v\n", err)
		os.Exit(1)
	}

	history, err := convService.RestoreConversation(restoreCtx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to restore conversation: %v\n", err)
		os.Exit(1)
	}

	// Display restored conversation turns
	fmt.Println("================================================================================")
	fmt.Printf("📜 Restored History (%d messages):\n", len(history))
	fmt.Println("================================================================================")
	for i, msg := range history {
		ts := msg.Timestamp.Format("2006-01-02 15:04:05")
		switch msg.Sender {
		case "CUSTOMER":
			fmt.Printf("[%d] %s 👤 Customer: %s\n", i+1, ts, msg.Content)
		case "AGENT", "BOT":
			if msg.Answer != nil {
				fmt.Printf("[%d] %s 🤖 Bot: %s\n", i+1, ts, msg.Answer.Text)
				for j, src := range msg.Answer.Sources {
					fmt.Printf("       📚 Source [%d]: %s (ID: %s)\n", j+1, src.Title, src.ID)
				}
			} else {
				fmt.Printf("[%d] %s 🤖 Bot: %s\n", i+1, ts, msg.Content)
			}
		}
	}
	fmt.Println("================================================================================")
	fmt.Println("✨ Session restore example completed successfully.")
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
