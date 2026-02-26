package commands_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jtarchie/ci/commands"
	"github.com/jtarchie/ci/server"
	"github.com/jtarchie/ci/storage"
	_ "github.com/jtarchie/ci/storage/sqlite"
	. "github.com/onsi/gomega"
)

// newTestServer spins up a real server.Router backed by a temporary SQLite
// database and returns the storage client and a started httptest.Server.
// The caller does not need to close either — t.Cleanup handles it.
func newTestServer(t *testing.T, opts server.RouterOptions) (storage.Driver, *httptest.Server) {
	t.Helper()
	assert := NewGomegaWithT(t)

	buildFile, err := os.CreateTemp(t.TempDir(), "*.db")
	assert.Expect(err).NotTo(HaveOccurred())
	_ = buildFile.Close()

	var (
		client   storage.Driver
		initFunc storage.InitFunc
	)

	storage.Each(func(_ string, f storage.InitFunc) {
		if initFunc == nil {
			initFunc = f
		}
	})

	client, err = initFunc(buildFile.Name(), "test", slog.Default())
	assert.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = client.Close() })

	router, err := server.NewRouter(slog.Default(), client, opts)
	assert.Expect(err).NotTo(HaveOccurred())

	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)

	return client, ts
}

// minimalJS is a valid pipeline used across tests.
const minimalJS = `
const pipeline = async () => {};
export { pipeline };
`

func writePipeline(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write pipeline file: %v", err)
	}
	return path
}

func TestSetPipeline(t *testing.T) {
	t.Parallel()

	storage.Each(func(driverName string, _ storage.InitFunc) {
		t.Run(driverName, func(t *testing.T) {
			t.Parallel()

			t.Run("uploads a valid JavaScript pipeline", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				client, ts := newTestServer(t, server.RouterOptions{})

				pipelineFile := writePipeline(t, t.TempDir(), "my-pipeline.js", `
const pipeline = async () => {
	console.log("hello");
};
export { pipeline };
`)
				cmd := commands.SetPipeline{
					Pipeline:  pipelineFile,
					ServerURL: ts.URL,
					Driver:    "docker://",
				}

				err := cmd.Run(slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())

				result, err := client.ListPipelines(context.Background(), 1, 100)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(result.Items).To(HaveLen(1))
				assert.Expect(result.Items[0].Name).To(Equal("my-pipeline"))
				assert.Expect(result.Items[0].DriverDSN).To(Equal("docker://"))
			})

			t.Run("uploads a valid TypeScript pipeline", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				client, ts := newTestServer(t, server.RouterOptions{})

				pipelineFile := writePipeline(t, t.TempDir(), "typed-pipeline.ts", `
const pipeline = async (): Promise<void> => {
	const x: string = "hello";
	console.log(x);
};
export { pipeline };
`)
				cmd := commands.SetPipeline{
					Pipeline:  pipelineFile,
					ServerURL: ts.URL,
				}

				err := cmd.Run(slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())

				result, err := client.ListPipelines(context.Background(), 1, 100)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(result.Items).To(HaveLen(1))
				assert.Expect(result.Items[0].Name).To(Equal("typed-pipeline"))
			})

			t.Run("uses custom name when provided", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				client, ts := newTestServer(t, server.RouterOptions{})

				pipelineFile := writePipeline(t, t.TempDir(), "file.js", minimalJS)
				cmd := commands.SetPipeline{
					Pipeline:  pipelineFile,
					Name:      "custom-name",
					ServerURL: ts.URL,
				}

				err := cmd.Run(slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())

				result, err := client.ListPipelines(context.Background(), 1, 100)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(result.Items).To(HaveLen(1))
				assert.Expect(result.Items[0].Name).To(Equal("custom-name"))
			})

			t.Run("handles server error gracefully", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				_, realTS := newTestServer(t, server.RouterOptions{})

				// Wrap the real router so that POST /api/pipelines always returns 500.
				wrapped := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPost {
						w.WriteHeader(http.StatusInternalServerError)
						_ = json.NewEncoder(w).Encode(map[string]string{"error": "database error"})
						return
					}
					realTS.Config.Handler.ServeHTTP(w, r)
				}))
				t.Cleanup(wrapped.Close)

				pipelineFile := writePipeline(t, t.TempDir(), "pipeline.js", minimalJS)
				cmd := commands.SetPipeline{
					Pipeline:  pipelineFile,
					ServerURL: wrapped.URL,
				}

				err := cmd.Run(slog.Default())
				assert.Expect(err).To(HaveOccurred())
				assert.Expect(err.Error()).To(ContainSubstring("database error"))
			})

			t.Run("idempotent: replaces existing pipeline with same name", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				client, ts := newTestServer(t, server.RouterOptions{})

				// Seed with an existing pipeline of the same name.
				existing, err := client.SavePipeline(context.Background(), "my-pipeline", "old content", "native://", "")
				assert.Expect(err).NotTo(HaveOccurred())

				pipelineFile := writePipeline(t, t.TempDir(), "my-pipeline.js", `
const pipeline = async () => { console.log("v2"); };
export { pipeline };
`)
				cmd := commands.SetPipeline{
					Pipeline:  pipelineFile,
					ServerURL: ts.URL,
				}

				err = cmd.Run(slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())

				result, err := client.ListPipelines(context.Background(), 1, 100)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(result.Items).To(HaveLen(1))
				assert.Expect(result.Items[0].Name).To(Equal("my-pipeline"))
				// The old record was deleted by the command; the new one has a different ID.
				assert.Expect(result.Items[0].ID).NotTo(Equal(existing.ID))
			})

			t.Run("idempotent: no delete when no pipeline with same name exists", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				client, ts := newTestServer(t, server.RouterOptions{})

				// Seed a pipeline with a different name — it must remain untouched.
				_, err := client.SavePipeline(context.Background(), "other-pipeline", "content", "docker://", "")
				assert.Expect(err).NotTo(HaveOccurred())

				pipelineFile := writePipeline(t, t.TempDir(), "new-pipeline.js", minimalJS)
				cmd := commands.SetPipeline{
					Pipeline:  pipelineFile,
					ServerURL: ts.URL,
				}

				err = cmd.Run(slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())

				result, err := client.ListPipelines(context.Background(), 1, 100)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(result.Items).To(HaveLen(2))
			})

			t.Run("basic auth credentials in server URL are forwarded on all requests", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				_, ts := newTestServer(t, server.RouterOptions{
					BasicAuthUsername: "admin",
					BasicAuthPassword: "secret",
				})

				serverURLWithAuth := "http://admin:secret@" + ts.Listener.Addr().String()

				pipelineFile := writePipeline(t, t.TempDir(), "auth-pipeline.js", minimalJS)
				cmd := commands.SetPipeline{
					Pipeline:  pipelineFile,
					ServerURL: serverURLWithAuth,
				}

				// If auth is not forwarded on any request the server rejects with 401
				// and the command returns an error.
				err := cmd.Run(slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	// These tests do not require a storage-backed server.

	t.Run("fails on invalid syntax", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		pipelineFile := writePipeline(t, t.TempDir(), "bad.js", `
const pipeline = async ( => {
	console.log("hello");
};
`)
		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: "http://localhost:0", // never reached — validation is client-side
		}

		err := cmd.Run(slog.Default())
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("validation failed"))
	})

	t.Run("rejects unsupported file extensions", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		pipelineFile := writePipeline(t, t.TempDir(), "pipeline.txt", "some content")
		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: "http://localhost:0",
		}

		err := cmd.Run(slog.Default())
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("unsupported file extension"))
	})

	t.Run("credentials are redacted from the server URL in output", func(t *testing.T) {
		// Not parallel — captures os.Stdout, which is not goroutine-safe.
		assert := NewGomegaWithT(t)

		_, ts := newTestServer(t, server.RouterOptions{})

		serverURLWithAuth := "http://admin:supersecret@" + ts.Listener.Addr().String()

		pipelineFile := writePipeline(t, t.TempDir(), "my-pipeline.js", minimalJS)
		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: serverURLWithAuth,
		}

		// Capture stdout.
		origStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		err := cmd.Run(slog.Default())

		_ = w.Close()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		os.Stdout = origStdout

		assert.Expect(err).NotTo(HaveOccurred())
		output := buf.String()
		assert.Expect(output).NotTo(ContainSubstring("supersecret"))
		assert.Expect(output).To(ContainSubstring(ts.Listener.Addr().String()))
	})
}
