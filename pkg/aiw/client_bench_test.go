package aiw_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/morphy76/aiw-client/pkg/aiw"
)

func BenchmarkClient_AddCustomerMessage(b *testing.B) {
	clientMock := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Accept") == "text/event-stream" {
			pr, pw := io.Pipe()
			go func() {
				defer pw.Close()
				_, _ = fmt.Fprint(pw, "data: {\"lifecycle\":{\"event\":\"created\",\"dialog_id\":\"dialog-bench-101\"}}\n\n")
			}()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       pr,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(http.NoBody),
		}, nil
	})

	client, err := aiw.New(aiw.WithHTTPClient(clientMock))
	if err != nil {
		b.Fatalf("failed to create client: %v", err)
	}
	defer func() { _ = client.Close() }()

	convSvc := client.Conversational()
	ctx := context.Background()
	convCtx := aiw.NewConversationalContext(ctx, "bench-user-1")

	err = convSvc.OpenConversation(convCtx, nil, nil, nil, nil, nil, nil, nil)
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
			Build()
		if err != nil {
			b.Fatalf("build error: %v", err)
		}
		_ = client.Close()
	}
}

