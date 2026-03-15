package commands

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/jtarchie/pocketci/storage"
)

type DeletePipeline struct {
	Name       string `arg:"" help:"Name or ID of the pipeline to delete" required:""`
	ServerURL  string `env:"CI_SERVER_URL" help:"URL of the CI server" required:"" short:"s"`
	AuthToken  string `env:"CI_AUTH_TOKEN"  help:"Bearer token for OAuth-authenticated servers" short:"t"`
	ConfigFile string `env:"CI_AUTH_CONFIG" help:"Path to auth config file (default: ~/.pocketci/auth.config)" short:"c"`
}

func (c *DeletePipeline) Run(logger *slog.Logger) error {
	logger = logger.WithGroup("pipeline.delete")

	serverURL := strings.TrimSuffix(c.ServerURL, "/")
	endpoint := serverURL + "/api/pipelines"

	client := resty.New()

	// Extract basic auth from URL if present and strip it from the base URL.
	if parsed, err := url.Parse(serverURL); err == nil && parsed.User != nil {
		password, _ := parsed.User.Password()
		client.SetBasicAuth(parsed.User.Username(), password)
		parsed.User = nil
		endpoint = parsed.String() + "/api/pipelines"
	}

	// Resolve auth token: explicit flag > config file lookup.
	token := ResolveAuthToken(c.AuthToken, c.ConfigFile, c.ServerURL)
	if token != "" {
		client.SetAuthToken(token)
	}

	// Resolve name → ID: fetch the pipeline list and match by name or ID.
	logger.Info("pipeline.list")

	listResp, err := client.R().Get(endpoint)
	if err != nil {
		return fmt.Errorf("could not list pipelines: %w", err)
	}

	if listResp.StatusCode() == 401 {
		return authRequiredError(serverURL)
	}

	if listResp.StatusCode() == 403 {
		return accessDeniedError(serverURL)
	}

	if listResp.StatusCode() != 200 {
		return fmt.Errorf("server error listing pipelines (%d): %s", listResp.StatusCode(), listResp.String())
	}

	var result storage.PaginationResult[storage.Pipeline]
	if err := json.Unmarshal(listResp.Body(), &result); err != nil {
		return fmt.Errorf("could not parse pipeline list: %w", err)
	}

	var matched []storage.Pipeline

	for _, p := range result.Items {
		if p.ID == c.Name || p.Name == c.Name {
			matched = append(matched, p)
		}
	}

	if len(matched) == 0 {
		return fmt.Errorf("no pipeline found with name or ID %q", c.Name)
	}

	for _, p := range matched {
		logger.Info("pipeline.delete", "id", p.ID, "name", p.Name)

		resp, err := client.R().Delete(endpoint + "/" + p.ID)
		if err != nil {
			return fmt.Errorf("could not delete pipeline %q (%s): %w", p.Name, p.ID, err)
		}

		if resp.StatusCode() == 401 {
			return authRequiredError(serverURL)
		}

		if resp.StatusCode() == 403 {
			return accessDeniedError(serverURL)
		}

		if resp.StatusCode() != 204 {
			return fmt.Errorf("server error deleting pipeline %q (%s) (%d): %s", p.Name, p.ID, resp.StatusCode(), resp.String())
		}

		fmt.Printf("Pipeline '%s' deleted successfully (ID: %s)\n", p.Name, p.ID)
	}

	return nil
}
