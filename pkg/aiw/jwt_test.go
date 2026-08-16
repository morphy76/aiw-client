package aiw_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/morphy76/aiw-client/pkg/aiw"
)

func createTestJWT(claims map[string]any) string {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	hBytes, _ := json.Marshal(header)
	cBytes, _ := json.Marshal(claims)

	hB64 := base64.RawURLEncoding.EncodeToString(hBytes)
	cB64 := base64.RawURLEncoding.EncodeToString(cBytes)

	return hB64 + "." + cB64 + ".fake-signature"
}

func TestExtractUsernameFromToken(t *testing.T) {
	t.Run("extracts preferred_username", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"preferred_username": "john.doe",
			"sub":                "sub-12345",
			"email":              "john@example.com",
		})

		username := aiw.ExtractUsernameFromToken(token)
		assert.Equal(t, "john.doe", username)
	})

	t.Run("extracts username if preferred_username is absent", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"username": "alice",
			"sub":      "sub-99999",
		})

		username := aiw.ExtractUsernameFromToken("Bearer " + token)
		assert.Equal(t, "alice", username)
	})

	t.Run("extracts user_name", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"user_name": "bob_builder",
		})

		username := aiw.ExtractUsernameFromToken(token)
		assert.Equal(t, "bob_builder", username)
	})

	t.Run("extracts sub when no username claim", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"sub": "user-guid-7788",
		})

		username := aiw.ExtractUsernameFromToken(token)
		assert.Equal(t, "user-guid-7788", username)
	})

	t.Run("extracts email", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"email": "customer@aiwave.io",
		})

		username := aiw.ExtractUsernameFromToken(token)
		assert.Equal(t, "customer@aiwave.io", username)
	})

	t.Run("returns empty string for invalid token format", func(t *testing.T) {
		assert.Empty(t, aiw.ExtractUsernameFromToken("not-a-jwt"))
		assert.Empty(t, aiw.ExtractUsernameFromToken(""))
		assert.Empty(t, aiw.ExtractUsernameFromToken("abc.invalid-base64.sig"))
	})
}

func TestExtractEmailFromToken(t *testing.T) {
	t.Run("extracts valid email claim", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"email": "user@example.com",
		})
		assert.Equal(t, "user@example.com", aiw.ExtractEmailFromToken(token))
	})

	t.Run("extracts email with Bearer prefix", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"email": "alice@almawave.it",
		})
		assert.Equal(t, "alice@almawave.it", aiw.ExtractEmailFromToken("Bearer "+token))
	})

	t.Run("returns empty string when email claim is missing or invalid", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"preferred_username": "john.doe",
		})
		assert.Empty(t, aiw.ExtractEmailFromToken(token))
		assert.Empty(t, aiw.ExtractEmailFromToken("invalid-jwt"))
	})
}

func TestExtractTenantFromToken(t *testing.T) {
	t.Run("rule 1: tenantId and subscriptionId present returns tenantId-subscriptionId", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"tenantId":       "tenant-100",
			"subscriptionId": "sub-xyz",
		})
		assert.Equal(t, "tenant-100-sub-xyz", aiw.ExtractTenantFromToken(token))
	})

	t.Run("rule 1: tenantId present and subscriptionId empty returns tenantId", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"tenantId":       "tenant-100",
			"subscriptionId": "",
		})
		assert.Equal(t, "tenant-100", aiw.ExtractTenantFromToken(token))
	})

	t.Run("rule 1: tenantId present without subscriptionId claim returns tenantId", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"tenantId": "my-tenant",
		})
		assert.Equal(t, "my-tenant", aiw.ExtractTenantFromToken(token))
	})

	t.Run("rule 2: tenantId absent but tenant present returns tenant", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"tenant": "almawave.com",
		})
		assert.Equal(t, "almawave.com", aiw.ExtractTenantFromToken(token))
	})

	t.Run("rule 3: fallback to email domain when tenantId and tenant are absent", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"email": "user@almawave.com",
		})
		assert.Equal(t, "almawave.com", aiw.ExtractTenantFromToken(token))
	})

	t.Run("rule 3: email with subdomain returns full domain", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"email": "developer@corp.almawave.it",
		})
		assert.Equal(t, "corp.almawave.it", aiw.ExtractTenantFromToken(token))
	})

	t.Run("precedence: tenantId and subscriptionId takes precedence over tenant and email", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"tenantId":       "primary-tenant",
			"subscriptionId": "sub-1",
			"tenant":         "secondary-tenant",
			"email":          "user@fallback.com",
		})
		assert.Equal(t, "primary-tenant-sub-1", aiw.ExtractTenantFromToken(token))
	})

	t.Run("precedence: tenant claim takes precedence over email domain", func(t *testing.T) {
		token := createTestJWT(map[string]any{
			"tenant": "explicit-tenant",
			"email":  "user@fallback.com",
		})
		assert.Equal(t, "explicit-tenant", aiw.ExtractTenantFromToken(token))
	})

	t.Run("returns empty string when no tenant, tenantId, or valid email domain exists", func(t *testing.T) {
		tokenWithoutClaims := createTestJWT(map[string]any{
			"preferred_username": "john",
		})
		assert.Empty(t, aiw.ExtractTenantFromToken(tokenWithoutClaims))

		tokenWithInvalidEmail := createTestJWT(map[string]any{
			"email": "no-domain-string",
		})
		assert.Empty(t, aiw.ExtractTenantFromToken(tokenWithInvalidEmail))

		assert.Empty(t, aiw.ExtractTenantFromToken("invalid-token"))
		assert.Empty(t, aiw.ExtractTenantFromToken(""))
	})
}

