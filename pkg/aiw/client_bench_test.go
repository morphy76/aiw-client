package aiw_test

import (
	"context"
	"testing"

	"github.com/morphy76/aiw-client/pkg/aiw"
)

func BenchmarkClient_AddCustomerMessage(b *testing.B) {
	convService, err := aiw.NewConversationalServiceBuilder().
		WithInMemoryGateway().
		Build()
	if err != nil {
		b.Fatalf("failed to build conversational service: %v", err)
	}

	client, err := aiw.New(aiw.WithConversationalService(convService))
	if err != nil {
		b.Fatalf("failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	convSvc := client.Conversational()
	ctx := context.Background()
	convCtx := aiw.NewConversationalContext(ctx, "bench-user-1")

	err = convSvc.OpenConversation(convCtx, nil, nil, nil, nil, nil)
	if err != nil {
		b.Fatalf("failed to open conversation: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = convSvc.AddCustomerMessage(convCtx, "benchmark message payload")
	}
}

func BenchmarkConversationalContext_Accessors(b *testing.B) {
	ctx := aiw.NewConversationalContext(context.Background(), "bench-user-2")
	ctx.SetDialogID("dialog-bench-123")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = ctx.ExternalID()
		_ = ctx.DialogID()
	}
}

func BenchmarkClientBuilder_Build(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		client, err := aiw.NewClientBuilder().
			WithInMemoryGateway().
			Build()
		if err != nil {
			b.Fatalf("build error: %v", err)
		}
		_ = client.Close()
	}
}

