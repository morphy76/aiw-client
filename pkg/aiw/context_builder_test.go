package aiw_test

import (
	"context"
	"testing"
	"time"

	"github.com/morphy76/aiw-client/pkg/aiw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConversationalContextBuilder_Build(t *testing.T) {
	t.Run("build with full configuration and token with tenantId and subscriptionId", func(t *testing.T) {
		parentCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		token := createTestJWT(map[string]any{
			"tenantId":       "customer",
			"subscriptionId": "service",
		})

		convCtx, err := aiw.NewConversationalContextBuilder().
			WithContext(parentCtx).
			WithExternalID("user-ext-999").
			WithDialogID("dlg-init-123").
			WithDialogModel("RocchettoEmbeddingsV2").
			WithBearerToken(token).
			WithSandbox(true).
			WithHeader("X-Custom-Trace", "trace-777").
			Build()

		require.NoError(t, err)
		require.NotNil(t, convCtx)

		assert.Equal(t, "user-ext-999", convCtx.ExternalID())
		assert.Equal(t, "dlg-init-123", convCtx.DialogID())
		assert.Equal(t, "customer-service", convCtx.Tenant())
		assert.Equal(t, "RocchettoEmbeddingsV2", convCtx.DialogModel())
		assert.Equal(t, token, convCtx.BearerToken())
		assert.True(t, convCtx.Sandbox())
		assert.Equal(t, "live:customer-service", convCtx.CognitiveSystemHeader())
		assert.Equal(t, "trace-777", convCtx.Headers()["X-Custom-Trace"])

		// Context delegation
		deadline, ok := convCtx.Deadline()
		assert.True(t, ok)
		assert.True(t, deadline.After(time.Now()))
	})

	t.Run("build with token extracting tenant from tenant claim", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"tenant": "finance",
		})

		convCtx, err := aiw.NewConversationalContextBuilder().
			WithContext(context.Background()).
			WithExternalID("ext-builder-helper").
			WithDialogModel("Rocchetto").
			WithBearerToken(token).
			Build()

		require.NoError(t, err)
		assert.Equal(t, "ext-builder-helper", convCtx.ExternalID())
		assert.Equal(t, "finance", convCtx.Tenant())
		assert.Equal(t, "live:finance", convCtx.CognitiveSystemHeader())
	})

	t.Run("build with token extracting tenant from email domain", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"email": "developer@almawave.com",
		})

		convCtx, err := aiw.NewConversationalContextBuilder().
			WithExternalID("ext-email-user").
			WithBearerToken(token).
			Build()

		require.NoError(t, err)
		assert.Equal(t, "ext-email-user", convCtx.ExternalID())
		assert.Equal(t, "almawave.com", convCtx.Tenant())
		assert.Equal(t, "live:almawave.com", convCtx.CognitiveSystemHeader())
	})

	t.Run("build with defaults", func(t *testing.T) {
		convCtx, err := aiw.NewConversationalContextBuilder().
			WithExternalID("user-simple").
			Build()

		require.NoError(t, err)
		require.NotNil(t, convCtx)

		assert.Equal(t, "user-simple", convCtx.ExternalID())
		assert.Empty(t, convCtx.DialogID())
		assert.Equal(t, "default", convCtx.Tenant())
		assert.Empty(t, convCtx.DialogModel())
		assert.Empty(t, convCtx.BearerToken())
		assert.False(t, convCtx.Sandbox())
		assert.Equal(t, "live:default", convCtx.CognitiveSystemHeader())
	})

	t.Run("build with empty external ID fails", func(t *testing.T) {
		convCtx, err := aiw.NewConversationalContextBuilder().
			WithExternalID("").
			Build()

		assert.Error(t, err)
		assert.Nil(t, convCtx)
	})

	t.Run("mutation via WithDialogID preserves configurations and extracted tenant", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"tenant": "support",
		})

		convCtx, err := aiw.NewConversationalContextBuilder().
			WithExternalID("user-immutable").
			WithBearerToken(token).
			Build()

		require.NoError(t, err)

		cloned := convCtx.WithDialogID("dlg-assigned-456")
		require.NotNil(t, cloned)

		assert.Equal(t, "user-immutable", cloned.ExternalID())
		assert.Equal(t, "dlg-assigned-456", cloned.DialogID())
		assert.Equal(t, "support", cloned.Tenant())
		assert.Equal(t, token, cloned.BearerToken())
		assert.Equal(t, "live:support", cloned.CognitiveSystemHeader())
	})
}
