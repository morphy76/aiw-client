# AIW Client

[![Go Version](https://img.shields.io/badge/Go-1.26-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

`aiw-client` is a robust, concurrent Go client library for the AIW platform. Designed with **Hexagonal Architecture (Ports & Adapters)**, **Domain-Driven Design (DDD)**, and **SOLID clean code principles**, it cleanly isolates public API contracts (`pkg/`) from internal domain models and infrastructure adapters (`internal/`).

---

## Architecture Overview

```
github.com/morphy76/aiw-client/
├── pkg/aiw/                              # Public API (Contracts, Facade, Builders, Context)
│   ├── client.go                         # Main AIW Client Facade
│   ├── client_builder.go                 # ClientBuilder (Facade Builder & DI)
│   ├── conversational.go                 # ConversationalService & Callbacks (OnOpen, OnError, OnCustomerMessage, OnBotMessage, OnClose)
│   ├── conversational_service.go         # Driving Adapter Implementation
│   ├── conversational_service_builder.go # ConversationalServiceBuilder
│   ├── context.go                        # ConversationalContext
│   ├── context_builder.go                # ConversationalContextBuilder
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
            └── outbound/                 # Driven Adapters (HTTPSSEGateway, InMemory Gateway/Repo)
```

---

## Features

- **Facade Pattern & Fluent ClientBuilder**: Clean top-level entry point exposing conversational and platform services, constructed via `NewClientBuilder()` or functional options `New()`.
- **Flexible Dependency Injection**: Inject custom `ConversationalService` implementations or pre-configured `ConversationalServiceBuilder` instances directly into the facade.
- **SSE Stream Protocol**: Replicates full Server-Sent Events (SSE) protocol from the AIW platform (`/dialog/api/conversation/v1.0/live/${externalId}?with_dialog_model=${dialogModel}`), streaming events asynchronously and dispatching to registered callbacks.
- **Reactive Lifecycle & Message Callbacks**:
  - `OnOpenFn`: Called when `lifecycle.event == "created"` with session `dialog_id`.
  - `OnCustomerMessageFn`: Called when `message.event == "messageAdded"` with role `CUSTOMER`.
  - `OnBotMessageFn`: Called when `message.event == "messageAdded"` with role `BOT` / `AGENT`.
  - `OnErrorFn`: Called upon network/stream failures, abort events (`lifecycle.event == "aborted"`), or callback errors.
  - `OnCloseFn`: Called when session closes (`lifecycle.event == "closed"`).
- **Decoupled Service Builder**: `ConversationalServiceBuilder` to assemble conversational services with custom HTTP clients, timeouts, base URLs, and loggers without coupling the client facade to internal dependencies.
- **Fluent Context Builder**: `ConversationalContextBuilder` to configure customer external ID, tenant (`x-cognitive-system`), target dialog model, bearer token (PAT), sandbox mode, and custom headers.
- **Conversational Context**: Wraps standard Go `context.Context` (for timeout/cancellation propagation) with customer metadata (`ExternalID`) and thread-safe session tracking (`DialogID`).
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
	"time"

	"github.com/morphy76/aiw-client/pkg/aiw"
)

func main() {
	// 1. Initialize the client facade via ClientBuilder
	client, err := aiw.NewClientBuilder().
		WithTimeout(30 * time.Second).
		Build()
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// 2. Build ConversationalContext with tenant, dialog model, and auth
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	convCtx, err := aiw.NewConversationalContextBuilder().
		WithContext(ctx).
		WithExternalID("customer-12345").
		WithTenant("default").
		WithDialogModel("RocchettoEmbeddingsV2").
		WithBearerToken("your-pat-bearer-token").
		WithSandbox(false).
		WithBaseURL("https://dev.lab.aiwave.io").
		Build()
	if err != nil {
		panic(err)
	}

	// 3. Obtain the conversational service
	convService := client.Conversational()

	// 4. Open a conversation with reactive lifecycle & message callbacks
	err = convService.OpenConversation(
		convCtx,
		// onOpenFn
		func(c aiw.ConversationalContext) error {
			fmt.Printf("Session established! Dialog ID: %s, Model: %s\n", c.DialogID(), c.DialogModel())
			// Dispatch initial greeting
			return convService.AddCustomerMessage(c, "Hello! How can I track my order?")
		},
		// onErrorFn
		func(c aiw.ConversationalContext, err error) {
			fmt.Printf("Error encountered for %s: %v\n", c.ExternalID(), err)
		},
		// onCustomerMessageFn (echo / confirmation)
		func(c aiw.ConversationalContext, mex string) error {
			fmt.Printf("[%s] Customer Sent: %s\n", c.DialogID(), mex)
			return nil
		},
		// onBotMessageFn (AI agent response)
		func(c aiw.ConversationalContext, mex string) error {
			fmt.Printf("[%s] AI Bot Response: %s\n", c.DialogID(), mex)
			return nil
		},
		// onCloseFn
		func(c aiw.ConversationalContext) error {
			fmt.Println("Conversation closed.")
			return nil
		},
	)
	if err != nil {
		panic(err)
	}
}
```

### Dependency Injection with ClientBuilder

```go
// Option A: Direct service injection
customService, err := aiw.NewConversationalServiceBuilder().
    WithHTTPClient(&http.Client{Timeout: 15 * time.Second}).
    WithBaseURL("https://dev.lab.aiwave.io").
    WithTimeout(20 * time.Second).
    Build()

client, err := aiw.NewClientBuilder().
    WithConversationalService(customService).
    Build()

// Option B: Builder-based service configuration
client, err := aiw.NewClientBuilder().
    WithBaseURL("https://dev.lab.aiwave.io").
    WithHTTPClient(customHTTPClient).
    WithTimeout(20 * time.Second).
    Build()
```

### Interactive CLI / Shell Chat

An interactive terminal chat application is included in `examples/conversation`:

```bash
# Run against live AIW platform with PAT token
go run ./examples/conversation -token "your-pat-token" -tenant "default" -model "RocchettoEmbeddingsV2"

# Run offline in mock mode
go run ./examples/conversation -mock
```

Commands available during the chat session:
- `/help` — Display session details and available commands.
- `/clear` — Clear the terminal screen.
- `/exit` or `exit` — Gracefully terminate the conversation session and exit.

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
