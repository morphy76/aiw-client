# AIW Client

[![Go Version](https://img.shields.io/badge/Go-1.26-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

`aiw-client` is a robust, concurrent Go client library for the AIW platform. Designed with **Hexagonal Architecture (Ports & Adapters)**, **Domain-Driven Design (DDD)**, and **SOLID clean code principles**, it cleanly isolates public API contracts (`pkg/`) from internal domain models and infrastructure adapters (`internal/`).

---

## Objective & Scope

`aiw-client` is **not** an exhaustive, low-level administrative client API for the AIW platform. Instead, it aims to be a **high-level, use-case-focused client library** tailored specifically for end-user runtime applications rather than administrative operations:

- **End-User Focused**: Designed for end-user runtime purposes (e.g. streaming live chats, restoring conversation turns, querying search results) rather than platform management, model creation, or tenant provisioning.
- **Use-Case Driven**:
  - **Sfera**: Conversational use cases (real-time Server-Sent Events streaming, interactive messaging, dialog session restoration, structured answer & citation parsing).
  - **Prisma**: Search and retrieval use cases.

---

## Architecture Overview

```
github.com/morphy76/aiw-client/
├── pkg/aiw/                              # Public API (Contracts, Facade, Builders, Context)
│   ├── client.go                         # Main AIW Client Facade & HTTPClient interface
│   ├── client_builder.go                 # ClientBuilder (Facade Builder & DI)
│   ├── conversational.go                 # ConversationalService & Callbacks (OnOpen, OnError, OnCustomerMessage, OnBotMessage, OnClose)
│   ├── conversational_service.go         # Driving Adapter Implementation
│   ├── context.go                        # ConversationalContext
│   ├── context_builder.go                # ConversationalContextBuilder
│   ├── jwt.go                            # JWT Token Claims & Username Parsing Utilities
│   └── options.go                        # Client Configuration Options
│
└── internal/
    ├── version/                          # Build-time injected version metadata
    └── conversational/                   # Conversational Component (Hexagonal)
        ├── domain/                       # Pure Domain Layer
        │   ├── model/                    # Aggregates, Entities, Value Objects, Errors
        │   └── event/                    # Domain Events
        ├── application/                  # Application Layer (Use Cases)
        │   ├── ports/
        │   │   ├── inbound/              # Driving Ports (ConversationalUseCase, StreamEventHandler)
        │   │   └── outbound/             # Driven Ports (ConversationRepository, AIWGateway)
        │   └── service/                  # Use Case Orchestration & Zerolog Logging
        └── adapters/
            └── outbound/                 # Driven Adapters (HTTPGateway, LiveStreamClient, MessageClient, SessionClient, InMemory ConversationRepo)
```

---

## Features

- **Facade Pattern & Fluent ClientBuilder**: Clean top-level entry point exposing conversational and platform services, constructed via `NewClientBuilder()` or functional options `New()`.
- **Flexible Dependency Injection**: Inject custom `ConversationalService` mock implementations directly into the facade for seamless unit testing.
- **SSE Stream Protocol**: Replicates full Server-Sent Events (SSE) protocol from the AIW platform (`/dialog/api/conversation/v1.0/live/${externalId}?with_dialog_model=${dialogModel}`), streaming events asynchronously and dispatching to registered callbacks.
- **Conversation Restoration & History**: Restore past conversational turns and citation sources from AIW recording data (`/dialog/api/dialogSession/v1.0/_withRecordingData`).
- **Session & Activity Listing**: List and paginate past user sessions and activities (`/dialog/api/dialogSession/v1.0/_fromFilter`).
- **Attachments & Structured Answers**: Support message attachments and automatic parsing of structured bot answers with supporting document citations (`Source`).
- **Reactive Lifecycle & Message Callbacks**: Detailed in **[Conversational Flow Documentation](file:///Users/R.Pasquini/Projects/side/aiw-client/docs/conversational_flow.md)**:
  - `OnOpenFn`: Called when the SSE transport stream is connected (HTTP 200).
  - `OnCreatedFn`: Called when `lifecycle.event == "created"` with assigned session `dialog_id`.
  - `OnErrorFn`: Called on network/parsing/functional failures, providing a `CancelStreamFunc` for stream teardown and optional remote dialog termination.
  - `OnCustomerMessageFn`: Called when `message.event == "messageAdded"` with role `CUSTOMER`.
  - `OnBotMessageFn`: Called when `message.event == "messageAdded"` with role `BOT` / `AGENT`.
  - `OnDialogTerminatedFn`: Called when the dialog terminates on the backend (`lifecycle.event == "aborted"` or `"closed"`), with `isAborted` flag.
  - `OnCloseFn`: Called when the SSE transport stream terminates (idempotent via `sync.Once`).
- **Fluent Context Builder**: `ConversationalContextBuilder` to configure customer external ID, target dialog model, bearer token (PAT), sandbox mode, and custom headers (tenant is automatically derived from the Bearer token).
- **Conversational Context**: Wraps standard Go `context.Context` (for timeout/cancellation propagation) with customer metadata (`ExternalID`), automatically resolved tenant (`Tenant()`), and thread-safe session tracking (`DialogID`).
- **JWT & Token Utilities**: Decode JWT payloads safely to automatically extract tenant namespaces (`ExtractTenantFromToken`), caller usernames (`ExtractUsernameFromToken`), and email addresses (`ExtractEmailFromToken`) following hierarchical fallback rules.
- **Hexagonal / DDD Structure**: Decoupled domain models, strict boundary interfaces, and swappable outbound adapters.
- **Structured Logging**: Context-aware `zerolog` structured logging on service boundaries with execution duration tracking.
- **Concurrency & Race-Condition Safe**: Fully tested with Go race detector (`-race`).


---

## Installation

```bash
go get github.com/morphy76/aiw-client
```

---

## Quick Start

### Standard Facade & SSE Conversational Flow
```go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/morphy76/aiw-client/pkg/aiw"
)

func main() {
	// 1. Initialize the client facade with Base URL and timeout
	client, err := aiw.NewClientBuilder().
		WithBaseURL("https://dev.lab.aiwave.io").
		WithTimeout(30 * time.Second).
		Build()
	if err != nil {
		panic(err)
	}
	defer func() { _ = client.Close() }()

	// 2. Build ConversationalContext (tenant is automatically resolved from Bearer PAT token)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	convCtx, err := aiw.NewConversationalContextBuilder().
		WithContext(ctx).
		WithExternalID("sample_user_001").
		WithDialogModel("RocchettoEmbeddingsV2").
		WithBearerToken(os.Getenv("PAT")).
		Build()
	if err != nil {
		panic(err)
	}

	// 3. Obtain the conversational service
	convService := client.Conversational()

	// Channel to wait for the asynchronous conversational flow to complete
	doneCh := make(chan struct{})

	// 4. Open a conversation with reactive lifecycle & message callbacks
	err = convService.OpenConversation(
		convCtx,
		// onOpenFn: SSE transport connected (HTTP 200)
		func(c aiw.ConversationalContext) error {
			fmt.Println("🔗 SSE transport stream connected.")
			return nil
		},
		// onCreatedFn: Dialog created on server with assigned Dialog ID
		func(c aiw.ConversationalContext, dialogID string) error {
			fmt.Printf("Connected! Dialog ID: %s, Model: %s, Tenant: %s\n", dialogID, c.DialogModel(), c.Tenant())
			return convService.AddCustomerMessage(c, "Hello! How do I reset my password?")
		},
		// onErrorFn: Handle stream or network errors with cancellation capability
		func(c aiw.ConversationalContext, err error, cancel aiw.CancelStreamFunc) {
			fmt.Printf("Error encountered for %s: %v\n", c.ExternalID(), err)
			cancel(true)
		},
		// onCustomerMessageFn: Message acknowledgment
		func(c aiw.ConversationalContext, mex string) error {
			fmt.Printf("[%s] Customer Sent: %s\n", c.DialogID(), mex)
			return nil
		},
		// onBotMessageFn: AI bot response; close session when done
		func(c aiw.ConversationalContext, answer string) error {
			fmt.Printf("[%s] AI Bot Response: %s\n", c.DialogID(), answer)
			return convService.CloseConversation(c)
		},
		// onDialogTerminatedFn: Dialog aborted or closed on server
		func(c aiw.ConversationalContext, isAborted bool, reason string) error {
			if isAborted {
				fmt.Printf("Dialog aborted by server: %s\n", reason)
			} else {
				fmt.Printf("Dialog closed cleanly: %s\n", reason)
			}
			return nil
		},
		// onCloseFn: Conversation transport terminated cleanly
		func(c aiw.ConversationalContext) error {
			fmt.Println("Conversation stream closed.")
			close(doneCh)
			return nil
		},
	)
	if err != nil {
		panic(err)
	}

	// 5. Await session completion or context timeout
	select {
	case <-doneCh:
		fmt.Println("Conversational flow finished successfully.")
	case <-ctx.Done():
		fmt.Println("Conversation timed out.")
	}
}
```

### Dependency Injection & Custom Configuration with ClientBuilder
 
```go
// Option A: Client facade with custom base URL, HTTP client, and timeouts
client, err := aiw.NewClientBuilder().
    WithBaseURL("https://portal.aiwave.ai").
    WithHTTPClient(customHTTPClient).
    WithTimeout(20 * time.Second).
    Build()

// Option B: Functional options initialization
client, err := aiw.New(
    aiw.WithBaseURL("https://portal.aiwave.ai"),
    aiw.WithHTTPClient(customHTTPClient),
    aiw.WithTimeout(20 * time.Second),
)

// Option C: Injecting a mock ConversationalService for unit testing
client, err := aiw.NewClientBuilder().
    WithConversationalService(mockService).
    Build()
```

### Examples

The repository includes runnable, educational examples under `examples/`:

1. **[Quickstart (`examples/quickstart`)](file:///Users/R.Pasquini/Projects/side/aiw-client/examples/quickstart/main.go)**:
   Minimal, canonical example to initialize the client, build `ConversationalContext`, register reactive SSE callbacks, exchange messages, and close cleanly.
   ```bash
   PAT="your-pat-token" go run ./examples/quickstart
   ```

2. **[Session Restore & Activity Listing (`examples/restore_session`)](file:///Users/R.Pasquini/Projects/side/aiw-client/examples/restore_session/main.go)**:
   Demonstrates how to query past sessions (`ListSessions`) and restore historical turns and document citation sources (`RestoreConversation`).
   ```bash
   PAT="your-pat-token" go run ./examples/restore_session
   ```

3. **[Interactive CLI Chat (`examples/conversation`)](file:///Users/R.Pasquini/Projects/side/aiw-client/examples/conversation/main.go)**:
   Full-featured terminal chat with interactive session selector (start new vs restore past session), real-time SSE streaming, and in-chat slash commands (`/history`, `/sessions`, `/help`, `/clear`, `/exit`).
   ```bash
   go run ./examples/conversation -token "your-pat-token" -model "RocchettoEmbeddingsV2"
   ```


---

## Testing & Quality

Run all unit tests:
```bash
make test
```

Run tests with race detection:
```bash
make test-race
```

Run benchmarks:
```bash
make test-bench
```

Run linter:
```bash
make lint
```
