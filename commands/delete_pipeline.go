package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jtarchie/ci/storage"
)

type DeletePipeline struct {
	Name      string `arg:"" help:"Name or ID of the pipeline to delete" required:""`
	ServerURL string `env:"CI_SERVER_URL" help:"URL of the CI server" required:"" short:"s"`
}

func (c *DeletePipeline) Run(logger *slog.Logger) error {
	logger = logger.WithGroup("pipeline.delete")

	serverURL := strings.TrimSuffix(c.ServerURL, "/")
	endpoint := serverURL + "/api/pipelines"

	doRequest := func(method, url string) (*http.Response, error) {
		req, err := http.NewRequest(method, url, nil)
		if err != nil {
			return nil, err
		}

		if req.URL.User != nil {
			password, _ := req.URL.User.Password()
			req.SetBasicAuth(req.URL.User.Username(), password)
		}

		return http.DefaultClient.Do(req)
	}

	// Resolve name → ID: fetch the pipeline list and match by name or ID.
	logger.Info("pipeline.list")

	listResp, err := doRequest(http.MethodGet, endpoint)
	if err != nil {
		return fmt.Errorf("could not list pipelines: %w", err)
	}

	listBody, _ := io.ReadAll(listResp.Body)
	_ = listResp.Body.Close()

	if listResp.StatusCode != http.StatusOK {
		return fmt.Errorf("server error listing pipelines (%d): %s", listResp.StatusCode, string(listBody))
	}

	var pipelines []storage.Pipeline
	if err := json.Unmarshal(listBody, &pipelines); err != nil {
		return fmt.Errorf("could not parse pipeline list: %w", err)
	}

	var matched []storage.Pipeline

	for _, p := range pipelines {
		if p.ID == c.Name || p.Name == c.Name {
			matched = append(matched, p)
		}
	}

	if len(matched) == 0 {
		return fmt.Errorf("no pipeline found with name or ID %q", c.Name)
	}

	for _, p := range matched {
		logger.Info("pipeline.delete", "id", p.ID, "name", p.Name)

		resp, err := doRequest(http.MethodDelete, endpoint+"/"+p.ID)
		if err != nil {
			return fmt.Errorf("could not delete pipeline %q (%s): %w", p.Name, p.ID, err)
		}

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			return fmt.Errorf("server error deleting pipeline %q (%s) (%d): %s", p.Name, p.ID, resp.StatusCode, string(body))
		}

		fmt.Printf("Pipeline '%s' deleted successfully (ID: %s)\n", p.Name, p.ID)
	}

	return nil
}
