package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/jtarchie/ci/backwards"
	"github.com/jtarchie/ci/runtime"
	"github.com/jtarchie/ci/storage"
)

type SetPipeline struct {
	Pipeline      string   `arg:""                  help:"Path to pipeline file (JS, TS, or YAML)"  required:"" type:"existingfile"`
	Name          string   `help:"Name for the pipeline (defaults to filename without extension)" short:"n"`
	ServerURL     string   `env:"CI_SERVER_URL"      help:"URL of the CI server"                                           required:"" short:"s"`
	Driver        string   `env:"CI_DRIVER"          help:"Orchestrator driver DSN (e.g., 'docker', 'native', 'k8s')"      short:"d"`
	WebhookSecret string   `env:"CI_WEBHOOK_SECRET"  help:"Secret for webhook signature validation"                        short:"w"`
	Secret        []string `help:"Set a pipeline-scoped secret as KEY=VALUE (can be repeated)" short:"e"`
	SecretFile    string   `help:"Path to a file containing secrets in KEY=VALUE format (one per line)" type:"existingfile"`
}

// pipelineRequest matches the server's expected JSON body for PUT /api/pipelines/:name.
type pipelineRequest struct {
	Content       string            `json:"content"`
	DriverDSN     string            `json:"driver_dsn"`
	WebhookSecret string            `json:"webhook_secret"`
	Secrets       map[string]string `json:"secrets,omitempty"`
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

	// Parse secrets from --secret-file and --secret flags
	secretsMap, err := c.parseSecrets()
	if err != nil {
		return err
	}

	// Upload to server via PUT /api/pipelines/:name
	serverURL := strings.TrimSuffix(c.ServerURL, "/")
	endpoint := serverURL + "/api/pipelines/" + url.PathEscape(name)

	logger.Info("pipeline.upload", "url", redactURL(endpoint))

	reqBody := pipelineRequest{
		Content:       finalContent,
		DriverDSN:     c.Driver,
		WebhookSecret: c.WebhookSecret,
		Secrets:       secretsMap,
	}

	client := resty.New()

	// Extract basic auth from URL if present and strip it from the endpoint.
	if parsed, err := url.Parse(serverURL); err == nil && parsed.User != nil {
		password, _ := parsed.User.Password()
		client.SetBasicAuth(parsed.User.Username(), password)
		parsed.User = nil
		endpoint = parsed.String() + "/api/pipelines/" + url.PathEscape(name)
	}

	resp, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(reqBody).
		Put(endpoint)
	if err != nil {
		return fmt.Errorf("could not connect to server: %w", err)
	}

	body := resp.Body()

	if resp.StatusCode() != 200 {
		var errResp map[string]string
		if json.Unmarshal(body, &errResp) == nil {
			if msg, ok := errResp["error"]; ok {
				return fmt.Errorf("server error (%d): %s", resp.StatusCode(), msg)
			}
		}

		return fmt.Errorf("server error (%d): %s", resp.StatusCode(), string(body))
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

	if len(secretsMap) > 0 {
		fmt.Printf("  Secrets: %d key(s) set\n", len(secretsMap))
	}

	return nil
}

// parseSecrets merges secrets from --secret-file and --secret flags.
// Flag values take precedence over file values on key collision.
func (c *SetPipeline) parseSecrets() (map[string]string, error) {
	result := make(map[string]string)

	// Parse --secret-file first (lower priority)
	if c.SecretFile != "" {
		f, err := os.Open(c.SecretFile)
		if err != nil {
			return nil, fmt.Errorf("could not open secret file: %w", err)
		}
		defer func() { _ = f.Close() }()

		scanner := bufio.NewScanner(f)
		lineNum := 0

		for scanner.Scan() {
			lineNum++
			line := strings.TrimSpace(scanner.Text())

			// Skip empty lines and comments
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			key, value, found := parseSecretFlag(line)
			if !found {
				return nil, fmt.Errorf("invalid secret in file %q line %d: expected KEY=VALUE format, got %q", c.SecretFile, lineNum, line)
			}

			result[key] = value
		}

		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("could not read secret file: %w", err)
		}
	}

	// Parse --secret flags (higher priority, overwrite file values)
	for _, s := range c.Secret {
		key, value, found := parseSecretFlag(s)
		if !found {
			return nil, fmt.Errorf("invalid --secret flag %q: expected KEY=VALUE format", s)
		}

		result[key] = value
	}

	if len(result) == 0 {
		return nil, nil //nolint:nilnil
	}

	return result, nil
}
