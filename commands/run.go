package commands

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// Run is the `ci run` command. It triggers a stored pipeline by name on a
// remote CI server and streams the result back. All execution, secrets, and
// driver configuration remain server-side.
type Run struct {
	Name      string        `arg:""           help:"Pipeline name to execute"`
	Args      []string      `arg:""           help:"Arguments passed to the pipeline via pipelineContext.args" optional:"" passthrough:""`
	ServerURL string        `env:"CI_SERVER_URL" help:"URL of the CI server" required:"" short:"s"`
	Timeout   time.Duration `env:"CI_TIMEOUT"    help:"Client-side timeout for the full execution (0 = no timeout)"`
}

// runRequest is the JSON body sent to the server.
type runRequest struct {
	Args []string `json:"args"`
}

// sseEvent is parsed from a `data: {...}` SSE line.
type sseEvent struct {
	Event   string `json:"event"`
	Code    int    `json:"code"`
	RunID   string `json:"run_id"`
	Stream  string `json:"stream"`
	Data    string `json:"data"`
	Message string `json:"message"`
}

func (c *Run) Run(logger *slog.Logger) error {
	serverURL := strings.TrimSuffix(c.ServerURL, "/")
	endpoint := serverURL + "/api/pipelines/" + c.Name + "/run"

	body, err := json.Marshal(runRequest{Args: c.Args})
	if err != nil {
		return fmt.Errorf("could not encode request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// Honour basic auth embedded in the server URL (mirrors set-pipeline pattern).
	if u := req.URL.User; u != nil {
		p, _ := u.Password()
		req.SetBasicAuth(u.Username(), p)
	}

	client := &http.Client{}
	if c.Timeout > 0 {
		client.Timeout = c.Timeout
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not connect to server: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	// Read SSE stream line-by-line.
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")
		var evt sseEvent
		if err := json.Unmarshal([]byte(payload), &evt); err != nil {
			logger.Debug("run.sse.unparseable", "line", payload)
			continue
		}

		switch evt.Event {
		case "exit":
			os.Exit(evt.Code) //nolint:gocritic // intentional: propagate exit code
		case "error":
			fmt.Fprintln(os.Stderr, "error:", evt.Message)
			os.Exit(1) //nolint:gocritic
		case "":
			// stdout/stderr data event
			switch evt.Stream {
			case "stderr":
				fmt.Fprint(os.Stderr, evt.Data)
			default:
				fmt.Fprint(os.Stdout, evt.Data)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stream: %w", err)
	}

	return nil
}
