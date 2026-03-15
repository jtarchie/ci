package auth

import (
	"time"
)

// User represents an authenticated user from any OAuth provider.
// Fields are tagged with `expr` for use in RBAC expression evaluation.
type User struct {
	Email         string         `json:"email"         expr:"Email"`
	Name          string         `json:"name"          expr:"Name"`
	NickName      string         `json:"nick_name"     expr:"NickName"`
	AvatarURL     string         `json:"avatar_url"    expr:"AvatarURL"`
	Provider      string         `json:"provider"      expr:"Provider"`
	UserID        string         `json:"user_id"       expr:"UserID"`
	Organizations []string       `json:"organizations" expr:"Organizations"`
	Groups        []string       `json:"groups"        expr:"Groups"`
	RawData       map[string]any `json:"raw_data"      expr:"RawData"`
}

// Config holds all authentication and authorization settings for the server.
type Config struct {
	// OAuth provider credentials (only providers with both ID and Secret are enabled).
	GithubClientID     string
	GithubClientSecret string

	GitlabClientID     string
	GitlabClientSecret string
	GitlabURL          string // Self-hosted GitLab URL (optional, defaults to https://gitlab.com)

	MicrosoftClientID     string
	MicrosoftClientSecret string
	MicrosoftTenant       string // Azure AD tenant (optional, defaults to "common")

	// Session configuration.
	SessionSecret string        // Secret key for encrypting session cookies.
	CallbackURL   string        // Base URL for OAuth callbacks (e.g., "https://ci.example.com").
	SessionMaxAge time.Duration // How long sessions last (default: 24h).

	// RBAC configuration.
	ServerRBAC string // expr expression for server-level access control.
}

// HasOAuthProviders returns true if at least one OAuth provider is configured.
func (c *Config) HasOAuthProviders() bool {
	return (c.GithubClientID != "" && c.GithubClientSecret != "") ||
		(c.GitlabClientID != "" && c.GitlabClientSecret != "") ||
		(c.MicrosoftClientID != "" && c.MicrosoftClientSecret != "")
}

// EnabledProviders returns the names of all configured OAuth providers.
func (c *Config) EnabledProviders() []string {
	var providers []string

	if c.GithubClientID != "" && c.GithubClientSecret != "" {
		providers = append(providers, "github")
	}

	if c.GitlabClientID != "" && c.GitlabClientSecret != "" {
		providers = append(providers, "gitlab")
	}

	if c.MicrosoftClientID != "" && c.MicrosoftClientSecret != "" {
		providers = append(providers, "microsoftonline")
	}

	return providers
}

// contextKey is a private type for context keys to avoid collisions.
type contextKey string

const userContextKey contextKey = "auth_user"
