package aiw

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// parseClaims decodes a JWT payload segment without verifying signature and unmarshals its claims.
func parseClaims(token string) map[string]any {
	token = strings.TrimSpace(token)
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}

	payloadSegment := parts[1]
	data, err := base64.RawURLEncoding.DecodeString(payloadSegment)
	if err != nil {
		if data, err = base64.URLEncoding.DecodeString(payloadSegment); err != nil {
			if data, err = base64.RawStdEncoding.DecodeString(payloadSegment); err != nil {
				if data, err = base64.StdEncoding.DecodeString(payloadSegment); err != nil {
					return nil
				}
			}
		}
	}

	var claims map[string]any
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil
	}

	return claims
}

func getClaimString(claims map[string]any, key string) string {
	if claims == nil {
		return ""
	}
	if val, ok := claims[key]; ok {
		if strVal, ok := val.(string); ok {
			return strings.TrimSpace(strVal)
		}
	}
	return ""
}

// ExtractUsernameFromToken attempts to decode a JWT token's payload segment and extract
// the caller's username from token claims without verifying signature.
// It checks standard claim keys: "preferred_username", "username", "user_name", "sub", "name", and "email".
func ExtractUsernameFromToken(token string) string {
	claims := parseClaims(token)
	if claims == nil {
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
		if val := getClaimString(claims, key); val != "" {
			return val
		}
	}

	return ""
}

// ExtractEmailFromToken extracts the email address from a JWT token's claims without verifying signature.
func ExtractEmailFromToken(token string) string {
	claims := parseClaims(token)
	if claims == nil {
		return ""
	}
	return getClaimString(claims, "email")
}

// ExtractTenantFromToken extracts the tenant namespace from a JWT token's claims based on the following precedence rules:
// 1. If claims have tenantId and subscriptionId, tenant is "<tenantId>-<subscriptionId>". If subscriptionId is not present or empty, tenant is "<tenantId>".
// 2. If tenantId is not present, but claim tenant is present, tenant is "<tenant>".
// 3. As a final fallback, tenant is the domain portion of the email claim (e.g. "user@domain.com" -> "domain.com").
// Returns an empty string if no tenant can be derived or token is invalid.
func ExtractTenantFromToken(token string) string {
	claims := parseClaims(token)
	if claims == nil {
		return ""
	}

	// Rule 1: tenantId and subscriptionId
	tenantID := getClaimString(claims, "tenantId")
	if tenantID != "" {
		subscriptionID := getClaimString(claims, "subscriptionId")
		if subscriptionID != "" {
			return tenantID + "-" + subscriptionID
		}
		return tenantID
	}

	// Rule 2: tenant claim
	tenantClaim := getClaimString(claims, "tenant")
	if tenantClaim != "" {
		return tenantClaim
	}

	// Rule 3: email domain fallback
	email := getClaimString(claims, "email")
	if email != "" {
		parts := strings.Split(email, "@")
		if len(parts) == 2 {
			domain := strings.TrimSpace(parts[1])
			if domain != "" {
				return domain
			}
		}
	}

	return ""
}

