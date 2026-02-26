package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jtarchie/ci/backwards"
	"github.com/jtarchie/ci/runtime"
	"github.com/jtarchie/ci/storage"
)

type SetPipeline struct {
	Pipeline      string `arg:""                  help:"Path to pipeline file (JS, TS, or YAML)"  required:"" type:"existingfile"`
	Name          string `help:"Name for the pipeline (defaults to filename without extension)" short:"n"`
	ServerURL     string `env:"CI_SERVER_URL"      help:"URL of the CI server"                                           required:"" short:"s"`
	Driver        string `env:"CI_DRIVER"          help:"Orchestrator driver DSN (e.g., 'docker', 'native', 'k8s')"      short:"d"`
	WebhookSecret string `env:"CI_WEBHOOK_SECRET"  help:"Secret for webhook signature validation"                        short:"w"`
}

// pipelineRequest matches the server's expected JSON body.
type pipelineRequest struct {
	Name          string `json:"name"`
	Content       string `json:"content"`
	DriverDSN     string `json:"driver_dsn"`
	WebhookSecret string `json:"webhook_secret"`
}

func (c *SetPipeline) Run(logger *slog.Logger) error {
	logger = logger.WithGroup("pipeline.set")

	// Determine pipeline name from filename if not provided
	name := c.Name
	if name == "" {
		base := filepath.Base(c.Pipeline)
		ext := filepath.Ext(base)
		name = strings.TrimSuffix(base, ext)
	}

	logger.Info("pipeline.read", "file", c.Pipeline, "name", name)

	// Read the pipeline file
	content, err := os.ReadFile(c.Pipeline)
	if err != nil {
		return fmt.Errorf("could not read pipeline file: %w", err)
	}

	// Determine the file type and process accordingly
	ext := strings.ToLower(filepath.Ext(c.Pipeline))

	var finalContent string

	switch ext {
	case ".yml", ".yaml":
		// Transpile YAML to TypeScript first
		logger.Info("pipeline.transpile")

		tsContent, err := backwards.NewPipeline(c.Pipeline)
		if err != nil {
			return fmt.Errorf("could not transpile YAML: %w", err)
		}

		finalContent = tsContent

	case ".ts":
		// TypeScript - will be stored as-is, server can transpile if needed
		finalContent = string(content)

	case ".js":
		// JavaScript - use as-is
		finalContent = string(content)

	default:
		return fmt.Errorf("unsupported file extension %q: expected .js, .ts, .yml, or .yaml", ext)
	}

	// Validate the pipeline syntax locally before uploading
	logger.Info("pipeline.validate")

	_, err = runtime.TranspileAndValidate(finalContent)
	if err != nil {
		return fmt.Errorf("pipeline validation failed: %w", err)
	}

	logger.Info("pipeline.validate.success")

	// Upload to server
	serverURL := strings.TrimSuffix(c.ServerURL, "/")
	endpoint := serverURL + "/api/pipelines"

	// Helper: make an authenticated request, honouring basic auth in the server URL.
	doRequest := func(method, url string, bodyBytes []byte) (*http.Response, error) {
		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequest(method, url, bodyReader)
		if err != nil {
			return nil, err
		}

		if bodyBytes != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		if req.URL.User != nil {
			password, _ := req.URL.User.Password()
			req.SetBasicAuth(req.URL.User.Username(), password)
		}

		return http.DefaultClient.Do(req)
	}

	// List existing pipelines and delete any with the same name (idempotent upsert
	// for servers that do not yet support ON CONFLICT upsert).
	logger.Info("pipeline.list")

	listResp, err := doRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("could not list pipelines: %w", err)
	}

	listBody, _ := io.ReadAll(listResp.Body)
	_ = listResp.Body.Close()

	if listResp.StatusCode == http.StatusOK {
		var existing []storage.Pipeline
		if json.Unmarshal(listBody, &existing) == nil {
			for _, p := range existing {
				if p.Name == name {
					logger.Info("pipeline.delete.existing", "id", p.ID)

					delResp, err := doRequest(http.MethodDelete, endpoint+"/"+p.ID, nil)
					if err != nil {
						return fmt.Errorf("could not delete existing pipeline: %w", err)
					}

					_ = delResp.Body.Close()
				}
			}
		}
	}

	logger.Info("pipeline.upload", "url", endpoint)

	reqBody := pipelineRequest{
		Name:          name,
		Content:       finalContent,
		DriverDSN:     c.Driver,
		WebhookSecret: c.WebhookSecret,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("could not marshal request: %w", err)
	}

	resp, err := doRequest(http.MethodPost, endpoint, jsonBody)
	if err != nil {
		return fmt.Errorf("could not connect to server: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("could not read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		var errResp map[string]string
		if json.Unmarshal(body, &errResp) == nil {
			if msg, ok := errResp["error"]; ok {
				return fmt.Errorf("server error (%d): %s", resp.StatusCode, msg)
			}
		}

		return fmt.Errorf("server error (%d): %s", resp.StatusCode, string(body))
	}

	// Parse the successful response
	var pipeline storage.Pipeline
	if err := json.Unmarshal(body, &pipeline); err != nil {
		return fmt.Errorf("could not parse response: %w", err)
	}

	logger.Info("pipeline.upload.success",
		"id", pipeline.ID,
		"name", pipeline.Name,
	)

	fmt.Printf("Pipeline '%s' uploaded successfully!\n", pipeline.Name)
	fmt.Printf("  ID: %s\n", pipeline.ID)

	displayURL := c.ServerURL
	if parsed, err := url.Parse(c.ServerURL); err == nil && parsed.User != nil {
		parsed.User = nil
		displayURL = parsed.String()
	}

	fmt.Printf("  Server: %s\n", displayURL)

	if c.Driver != "" {
		fmt.Printf("  Driver: %s\n", c.Driver)
	}

	return nil
}
