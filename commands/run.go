package commands

import (
	"archive/tar"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/klauspost/compress/zstd"
)

// Run is the `ci run` command. It triggers a stored pipeline by name on a
// remote CI server and streams the result back. All execution, secrets, and
// driver configuration remain server-side.
type Run struct {
	Name      string        `arg:""           help:"Pipeline name to execute"`
	Args      []string      `arg:""           help:"Arguments passed to the pipeline via pipelineContext.args" optional:"" passthrough:""`
	ServerURL string        `env:"CI_SERVER_URL" help:"URL of the CI server" required:"" short:"s"`
	Timeout   time.Duration `env:"CI_TIMEOUT"    help:"Client-side timeout for the full execution (0 = no timeout)"`
	NoWorkdir bool          `help:"Skip uploading the current working directory"`
	Ignore    []string      `help:"Glob patterns to exclude from the workdir upload (comma-separated)" default:".git/**/*" sep:","`
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
	logger = logger.WithGroup("pipeline.run")

	serverURL := strings.TrimSuffix(c.ServerURL, "/")
	endpoint := serverURL + "/api/pipelines/" + c.Name + "/run"

	// Build multipart body via a pipe so the HTTP client streams outbound data
	// as we write it, without buffering the entire tar to memory.
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	go func() {
		var writeErr error

		defer func() {
			_ = mw.Close()
			pw.CloseWithError(writeErr)
		}()

		// Field: args — JSON-encoded array of strings.
		argsData, err := json.Marshal(c.Args)
		if err != nil {
			writeErr = fmt.Errorf("could not encode args: %w", err)
			return
		}

		fw, err := mw.CreateFormField("args")
		if err != nil {
			writeErr = fmt.Errorf("could not create args field: %w", err)
			return
		}

		if _, err = fw.Write(argsData); err != nil {
			writeErr = fmt.Errorf("could not write args: %w", err)
			return
		}

		// File: workdir — zstd-compressed tar archive of the current working directory.
		if !c.NoWorkdir {
			cwd, err := os.Getwd()
			if err != nil {
				writeErr = fmt.Errorf("could not determine working directory: %w", err)
				return
			}

			logger.Info("pipeline.run.workdir", "path", cwd, "ignore", c.Ignore)

			ff, err := mw.CreateFormFile("workdir", "workdir.tar.zst")
			if err != nil {
				writeErr = fmt.Errorf("could not create workdir part: %w", err)
				return
			}

			zw, err := zstd.NewWriter(ff, zstd.WithEncoderLevel(zstd.SpeedFastest))
			if err != nil {
				writeErr = fmt.Errorf("could not create zstd writer: %w", err)
				return
			}

			if err := tarDirectory(cwd, zw, c.Ignore); err != nil {
				writeErr = fmt.Errorf("could not tar working directory: %w", err)
				return
			}

			if err := zw.Close(); err != nil {
				writeErr = fmt.Errorf("could not flush zstd stream: %w", err)
				return
			}
		}
	}()

	logger.Info("pipeline.run.trigger", "name", c.Name, "url", endpoint, "args", c.Args)

	req, err := http.NewRequest(http.MethodPost, endpoint, pr)
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Content-Type", mw.FormDataContentType())
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

	logger.Info("pipeline.run.streaming")

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
			if evt.Message != "" {
				fmt.Fprintln(os.Stderr, evt.Message)
			}

			logger.Info("pipeline.run.exit", "code", evt.Code, "run_id", evt.RunID)
			os.Exit(evt.Code) //nolint:gocritic // intentional: propagate exit code
		case "error":
			fmt.Fprintln(os.Stderr, "error:", evt.Message)
			os.Exit(1) //nolint:gocritic
		case "":
			// stdout/stderr data event
			switch evt.Stream {
			case "stderr":
				fmt.Fprint(os.Stderr, evt.Data) //nolint:errcheck
			default:
				fmt.Fprint(os.Stdout, evt.Data) //nolint:errcheck
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stream: %w", err)
	}

	return nil
}

// tarDirectory writes a tar archive of dir to w, compressing with zstd.
// ignorePatterns is a list of doublestar glob patterns (relative to dir) to skip.
func tarDirectory(dir string, w io.Writer, ignorePatterns []string) error {
	tw := tar.NewWriter(w)
	defer func() { _ = tw.Close() }()

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		// Skip the root "." directory entry — extractors handle it implicitly.
		if relPath == "." {
			return nil
		}

		if len(ignorePatterns) > 0 {
			if ignorePath(relPath, info.IsDir(), ignorePatterns) {
				if info.IsDir() {
					return filepath.SkipDir
				}

				return nil
			}
		}

		// Only follow regular files and directories; skip symlinks, devices, etc.
		if !info.Mode().IsRegular() && !info.IsDir() {
			return nil
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("could not build tar header for %q: %w", relPath, err)
		}

		hdr.Name = relPath
		// Produce portable headers: clear OS-specific fields that confuse
		// minimal tar implementations (e.g. busybox).
		hdr.Uid = 0
		hdr.Gid = 0
		hdr.Uname = ""
		hdr.Gname = ""
		hdr.AccessTime = time.Time{}
		hdr.ChangeTime = time.Time{}
		hdr.Xattrs = nil     //nolint:staticcheck // clear macOS xattrs
		hdr.PAXRecords = nil // avoid PAX extensions
		hdr.Format = tar.FormatGNU

		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("could not write tar header for %q: %w", relPath, err)
		}

		if !info.IsDir() {
			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("could not open %q: %w", path, err)
			}
			defer func() { _ = f.Close() }()

			if _, err = io.Copy(tw, f); err != nil {
				return fmt.Errorf("could not write %q to tar: %w", relPath, err)
			}
		}

		return nil
	})
}

// ignorePath returns true if relPath should be excluded based on the given glob patterns.
// For directories, it also probes whether anything inside the directory would match,
// enabling early SkipDir returns for patterns like ".git/**/*".
func ignorePath(relPath string, isDir bool, patterns []string) bool {
	// Normalise to forward slashes for doublestar.
	relPath = filepath.ToSlash(relPath)

	for _, pattern := range patterns {
		if ok, _ := doublestar.Match(pattern, relPath); ok {
			return true
		}

		// For directories, probe with a synthetic child path so that patterns
		// like ".git/**/*" cause the whole directory to be skipped.
		if isDir {
			if ok, _ := doublestar.Match(pattern, relPath+"/x"); ok {
				return true
			}
		}
	}

	return false
}
