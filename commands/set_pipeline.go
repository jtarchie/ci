package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jtarchie/ci/backwards"
	"github.com/jtarchie/ci/runtime"
	"github.com/jtarchie/ci/storage"
)

type SetPipeline struct {
	Pipeline  string `arg:""                  help:"Path to pipeline file (JS, TS, or YAML)"  required:"" type:"existingfile"`
	Name      string `help:"Name for the pipeline (defaults to filename without extension)" short:"n"`
	ServerURL string `help:"URL of the CI server"                                           required:"" short:"s"`
	Driver    string `help:"Orchestrator driver DSN (e.g., 'docker', 'native', 'k8s')"      short:"d"`
}

// pipelineRequest matches the server's expected JSON body.
type pipelineRequest struct {
	Name      string `json:"name"`
	Content   string `json:"content"`
	DriverDSN string `json:"driver_dsn"`
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

	logger.Info("pipeline.upload", "url", endpoint)

	reqBody := pipelineRequest{
		Name:      name,
		Content:   finalContent,
		DriverDSN: c.Driver,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("could not marshal request: %w", err)
	}

	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(jsonBody))
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
	fmt.Printf("  Server: %s\n", c.ServerURL)

	if c.Driver != "" {
		fmt.Printf("  Driver: %s\n", c.Driver)
	}

	return nil
}
