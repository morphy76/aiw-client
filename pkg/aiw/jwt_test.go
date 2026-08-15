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
