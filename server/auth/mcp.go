package auth

import (
	"context"
	"net/http"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
)

// MCPTokenVerifier returns an MCP SDK auth.TokenVerifier that validates
// PocketCI JWT tokens. The returned TokenInfo includes scopes from the JWT
// claims and maps the user ID for session hijacking prevention.
func MCPTokenVerifier(secret string) mcpauth.TokenVerifier {
	return func(_ context.Context, token string, _ *http.Request) (*mcpauth.TokenInfo, error) {
		claims, err := validateTokenClaims(token, secret)
		if err != nil {
			return nil, mcpauth.ErrInvalidToken
		}

		return &mcpauth.TokenInfo{
			Scopes:     claims.Scopes,
			Expiration: claims.ExpiresAt.Time,
			UserID:     claims.Subject,
		}, nil
	}
}
