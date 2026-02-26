package commands_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jtarchie/ci/commands"
	"github.com/jtarchie/ci/storage"
	. "github.com/onsi/gomega"
)

// pipelineServer is a test helper that simulates the server's pipeline API,
// tracking the sequence of requests and maintaining an in-memory list of
// pipelines. It handles GET /api/pipelines, POST /api/pipelines, and
// DELETE /api/pipelines/{id}.
type pipelineServer struct {
	mu        sync.Mutex
	pipelines []storage.Pipeline
	// requests records every (method, path) pair received, in order.
	requests []string
}

func newPipelineServer(initial ...storage.Pipeline) *pipelineServer {
	return &pipelineServer{pipelines: initial}
}

func (ps *pipelineServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ps.mu.Lock()
	ps.requests = append(ps.requests, r.Method+" "+r.URL.Path)
	ps.mu.Unlock()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/pipelines":
		ps.mu.Lock()
		list := ps.pipelines
		ps.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)

	case r.Method == http.MethodPost && r.URL.Path == "/api/pipelines":
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		p := storage.Pipeline{ID: "new-id-123", Name: req["name"]}
		ps.mu.Lock()
		ps.pipelines = append(ps.pipelines, p)
		ps.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(p)

	case r.Method == http.MethodDelete && len(r.URL.Path) > len("/api/pipelines/"):
		id := r.URL.Path[len("/api/pipelines/"):]
		ps.mu.Lock()
		kept := ps.pipelines[:0]
		for _, p := range ps.pipelines {
			if p.ID != id {
				kept = append(kept, p)
			}
		}
		ps.pipelines = kept
		ps.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}
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

	t.Run("uploads a valid JavaScript pipeline", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		ps := newPipelineServer()
		server := httptest.NewServer(ps)
		defer server.Close()

		pipelineFile := writePipeline(t, t.TempDir(), "my-pipeline.js", `
const pipeline = async () => {
	console.log("hello");
};
export { pipeline };
`)

		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: server.URL,
			Driver:    "docker://",
		}

		err := cmd.Run(slog.Default())
		assert.Expect(err).NotTo(HaveOccurred())

		assert.Expect(ps.pipelines).To(HaveLen(1))
		assert.Expect(ps.pipelines[0].Name).To(Equal("my-pipeline"))
		// Sequence: list → create (no existing pipeline to delete).
		assert.Expect(ps.requests).To(Equal([]string{
			"GET /api/pipelines",
			"POST /api/pipelines",
		}))
	})

	t.Run("uploads a valid TypeScript pipeline", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		ps := newPipelineServer()
		server := httptest.NewServer(ps)
		defer server.Close()

		pipelineFile := writePipeline(t, t.TempDir(), "typed-pipeline.ts", `
const pipeline = async (): Promise<void> => {
	const x: string = "hello";
	console.log(x);
};
export { pipeline };
`)

		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: server.URL,
		}

		err := cmd.Run(slog.Default())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ps.pipelines[0].Name).To(Equal("typed-pipeline"))
	})

	t.Run("uses custom name when provided", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		ps := newPipelineServer()
		server := httptest.NewServer(ps)
		defer server.Close()

		pipelineFile := writePipeline(t, t.TempDir(), "file.js", minimalJS)

		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			Name:      "custom-name",
			ServerURL: server.URL,
		}

		err := cmd.Run(slog.Default())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ps.pipelines[0].Name).To(Equal("custom-name"))
	})

	t.Run("fails on invalid syntax", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("server should not be called for invalid syntax")
		}))
		defer server.Close()

		pipelineFile := writePipeline(t, t.TempDir(), "bad.js", `
const pipeline = async ( => {
	console.log("hello");
};
`)

		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: server.URL,
		}

		err := cmd.Run(slog.Default())
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("validation failed"))
	})

	t.Run("handles server error gracefully", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				// Return empty list so the list step succeeds.
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode([]storage.Pipeline{})
			default:
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "database error"})
			}
		}))
		defer server.Close()

		pipelineFile := writePipeline(t, t.TempDir(), "pipeline.js", minimalJS)

		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: server.URL,
		}

		err := cmd.Run(slog.Default())
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("database error"))
	})

	t.Run("rejects unsupported file extensions", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		pipelineFile := writePipeline(t, t.TempDir(), "pipeline.txt", "some content")

		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: "http://localhost:8080",
		}

		err := cmd.Run(slog.Default())
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("unsupported file extension"))
	})

	t.Run("idempotent: deletes existing pipeline with same name before creating", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		// Seed the server with an existing pipeline that has the same name.
		existing := storage.Pipeline{ID: "old-id-abc", Name: "my-pipeline"}
		ps := newPipelineServer(existing)
		server := httptest.NewServer(ps)
		defer server.Close()

		pipelineFile := writePipeline(t, t.TempDir(), "my-pipeline.js", `
const pipeline = async () => {
	console.log("hello");
};
export { pipeline };
`)

		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: server.URL,
		}

		err := cmd.Run(slog.Default())
		assert.Expect(err).NotTo(HaveOccurred())

		// Old pipeline must be gone, new one present.
		assert.Expect(ps.pipelines).To(HaveLen(1))
		assert.Expect(ps.pipelines[0].ID).NotTo(Equal("old-id-abc"))
		assert.Expect(ps.pipelines[0].Name).To(Equal("my-pipeline"))

		// Must have listed, deleted the old one, then created the new one.
		assert.Expect(ps.requests).To(Equal([]string{
			"GET /api/pipelines",
			"DELETE /api/pipelines/old-id-abc",
			"POST /api/pipelines",
		}))
	})

	t.Run("idempotent: no delete when no pipeline with same name exists", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		// Seed a pipeline with a different name — should not be touched.
		other := storage.Pipeline{ID: "other-id", Name: "other-pipeline"}
		ps := newPipelineServer(other)
		server := httptest.NewServer(ps)
		defer server.Close()

		pipelineFile := writePipeline(t, t.TempDir(), "new-pipeline.js", minimalJS)

		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: server.URL,
		}

		err := cmd.Run(slog.Default())
		assert.Expect(err).NotTo(HaveOccurred())

		// Other pipeline untouched, new one added.
		assert.Expect(ps.pipelines).To(HaveLen(2))

		// Sequence: list → create only (no delete).
		assert.Expect(ps.requests).To(Equal([]string{
			"GET /api/pipelines",
			"POST /api/pipelines",
		}))
	})

	t.Run("idempotent: deletes all duplicates when multiple exist with same name", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		// Two stale entries with the same name (shouldn't happen in practice but
		// the client should handle it gracefully).
		ps := newPipelineServer(
			storage.Pipeline{ID: "dup-1", Name: "my-pipeline"},
			storage.Pipeline{ID: "dup-2", Name: "my-pipeline"},
		)
		server := httptest.NewServer(ps)
		defer server.Close()

		pipelineFile := writePipeline(t, t.TempDir(), "my-pipeline.js", `
const pipeline = async () => { console.log("v2"); };
export { pipeline };
`)

		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: server.URL,
		}

		err := cmd.Run(slog.Default())
		assert.Expect(err).NotTo(HaveOccurred())

		// Both duplicates deleted, new one created.
		assert.Expect(ps.pipelines).To(HaveLen(1))
		assert.Expect(ps.pipelines[0].ID).To(Equal("new-id-123"))

		assert.Expect(ps.requests).To(Equal([]string{
			"GET /api/pipelines",
			"DELETE /api/pipelines/dup-1",
			"DELETE /api/pipelines/dup-2",
			"POST /api/pipelines",
		}))
	})

	t.Run("basic auth credentials in server URL are forwarded on all requests", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		var authHeaders []string
		var mu sync.Mutex

		ps := newPipelineServer(storage.Pipeline{ID: "old-id", Name: "auth-pipeline"})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			authHeaders = append(authHeaders, r.Header.Get("Authorization"))
			mu.Unlock()
			ps.ServeHTTP(w, r)
		}))
		defer server.Close()

		// Embed credentials in the URL.
		serverURLWithAuth := "http://admin:secret@" + server.Listener.Addr().String()

		pipelineFile := writePipeline(t, t.TempDir(), "auth-pipeline.js", minimalJS)

		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: serverURLWithAuth,
		}

		err := cmd.Run(slog.Default())
		assert.Expect(err).NotTo(HaveOccurred())

		// GET, DELETE, POST — all three must carry the Authorization header.
		assert.Expect(authHeaders).To(HaveLen(3))
		for _, h := range authHeaders {
			assert.Expect(h).To(HavePrefix("Basic "))
		}
	})

	t.Run("credentials are redacted from the server URL in output", func(t *testing.T) {
		// Not parallel — captures os.Stdout, which is not goroutine-safe.
		assert := NewGomegaWithT(t)

		ps := newPipelineServer()
		server := httptest.NewServer(ps)
		defer server.Close()

		serverURLWithAuth := "http://admin:supersecret@" + server.Listener.Addr().String()

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
		assert.Expect(output).To(ContainSubstring(server.Listener.Addr().String()))
	})
}
