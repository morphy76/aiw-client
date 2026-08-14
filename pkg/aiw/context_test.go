package aiw_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/morphy76/aiw-client/pkg/aiw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConversationalContext_CreationAndAccessors(t *testing.T) {
	parentCtx := context.Background()
	extID := "ext-12345"

	convCtx := aiw.NewConversationalContext(parentCtx, extID)
	require.NotNil(t, convCtx)

	assert.Equal(t, extID, convCtx.ExternalID())
	assert.Empty(t, convCtx.DialogID())

	// Setting dialog ID
	convCtx.SetDialogID("dialog-999")
	assert.Equal(t, "dialog-999", convCtx.DialogID())
}

func TestConversationalContext_ContextPropagation(t *testing.T) {
	type testKey string
	const key testKey = "user_id"

	ctxWithValue := context.WithValue(context.Background(), key, "user-abc")
	ctxWithTimeout, cancel := context.WithTimeout(ctxWithValue, 500*time.Millisecond)
	defer cancel()

	convCtx := aiw.NewConversationalContext(ctxWithTimeout, "ext-100")

	// Verify standard context.Context method delegation
	assert.Equal(t, "user-abc", convCtx.Value(key))

	deadline, ok := convCtx.Deadline()
	assert.True(t, ok)
	assert.True(t, deadline.After(time.Now()))

	assert.NoError(t, convCtx.Err())
	select {
	case <-convCtx.Done():
		t.Fatal("context should not be done yet")
	default:
	}

	// Cancel and check Done channel
	cancel()
	<-convCtx.Done()
	assert.Error(t, convCtx.Err())
}

func TestConversationalContext_WithDialogID(t *testing.T) {
	convCtx := aiw.NewConversationalContext(context.Background(), "ext-200")
	convCtxWithDialog := convCtx.WithDialogID("dialog-111")

	assert.Equal(t, "ext-200", convCtxWithDialog.ExternalID())
	assert.Equal(t, "dialog-111", convCtxWithDialog.DialogID())
}

func TestConversationalContext_ConcurrentAccess(t *testing.T) {
	convCtx := aiw.NewConversationalContext(context.Background(), "ext-concurrent")

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			convCtx.SetDialogID("dialog-shared")
		}(i)
		go func() {
			defer wg.Done()
			_ = convCtx.DialogID()
			_ = convCtx.ExternalID()
		}()
	}
	wg.Wait()

	assert.Equal(t, "dialog-shared", convCtx.DialogID())
}
