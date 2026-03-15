package auth

import (
	"strings"

	"github.com/markbates/goth"
	"github.com/markbates/goth/providers/github"
	"github.com/markbates/goth/providers/gitlab"
	"github.com/markbates/goth/providers/microsoftonline"
)

// InitProviders configures goth with the OAuth providers specified in cfg.
// Only providers with both a client ID and secret are registered.
func InitProviders(cfg *Config) {
	var providers []goth.Provider

	callbackBase := strings.TrimSuffix(cfg.CallbackURL, "/")

	if cfg.GithubClientID != "" && cfg.GithubClientSecret != "" {
		providers = append(providers, github.New(
			cfg.GithubClientID,
			cfg.GithubClientSecret,
			callbackBase+"/auth/github/callback",
			"user:email", "read:org",
		))
	}

	if cfg.GitlabClientID != "" && cfg.GitlabClientSecret != "" {
		gitlabURL := cfg.GitlabURL
		if gitlabURL == "" {
			gitlabURL = "https://gitlab.com"
		}

		scopes := []string{"read_user", "openid", "profile", "email"}

		p := gitlab.NewCustomisedURL(
			cfg.GitlabClientID,
			cfg.GitlabClientSecret,
			callbackBase+"/auth/gitlab/callback",
			strings.TrimSuffix(gitlabURL, "/")+"/oauth/authorize",
			strings.TrimSuffix(gitlabURL, "/")+"/oauth/token",
			strings.TrimSuffix(gitlabURL, "/")+"/api/v4/user",
			scopes...,
		)
		providers = append(providers, p)
	}

	if cfg.MicrosoftClientID != "" && cfg.MicrosoftClientSecret != "" {
		providers = append(providers, microsoftonline.New(
			cfg.MicrosoftClientID,
			cfg.MicrosoftClientSecret,
			callbackBase+"/auth/microsoftonline/callback",
		))
	}

	if len(providers) > 0 {
		goth.UseProviders(providers...)
	}
}
