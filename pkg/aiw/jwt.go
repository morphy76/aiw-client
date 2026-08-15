package aiw

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// ExtractUsernameFromToken attempts to decode a JWT token's payload segment and extract
// the caller's username from token claims without verifying signature.
// It checks standard claim keys: "preferred_username", "username", "user_name", "sub", "name", and "email".
func ExtractUsernameFromToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}

	payloadSegment := parts[1]
	data, err := base64.RawURLEncoding.DecodeString(payloadSegment)
	if err != nil {
		if data, err = base64.URLEncoding.DecodeString(payloadSegment); err != nil {
			if data, err = base64.RawStdEncoding.DecodeString(payloadSegment); err != nil {
				if data, err = base64.StdEncoding.DecodeString(payloadSegment); err != nil {
					return ""
				}
			}
		}
	}

	var claims map[string]any
	if err := json.Unmarshal(data, &claims); err != nil {
		return ""
	}

	claimKeys := []string{
		"preferred_username",
		"username",
		"user_name",
		"sub",
		"name",
		"email",
	}

	for _, key := range claimKeys {
		if val, ok := claims[key]; ok {
			if strVal, ok := val.(string); ok {
				strVal = strings.TrimSpace(strVal)
				if strVal != "" {
					return strVal
				}
			}
		}
	}

	return ""
}
