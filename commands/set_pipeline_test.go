package commands_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jtarchie/ci/commands"
	"github.com/jtarchie/ci/storage"
	. "github.com/onsi/gomega"
)

func TestSetPipeline(t *testing.T) {
	t.Parallel()

	t.Run("uploads a valid JavaScript pipeline", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		var receivedReq map[string]string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Expect(r.Method).To(Equal(http.MethodPost))
			assert.Expect(r.URL.Path).To(Equal("/api/pipelines"))

			err := json.NewDecoder(r.Body).Decode(&receivedReq)
			assert.Expect(err).NotTo(HaveOccurred())

			resp := storage.Pipeline{
				ID:   "test-id-123",
				Name: receivedReq["name"],
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		pipelineFile := filepath.Join(tmpDir, "my-pipeline.js")
		err := os.WriteFile(pipelineFile, []byte(`
const pipeline = async () => {
	console.log("hello");
};
export { pipeline };
`), 0644)
		assert.Expect(err).NotTo(HaveOccurred())

		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: server.URL,
			Driver:    "docker://",
		}

		err = cmd.Run(slog.Default())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(receivedReq["name"]).To(Equal("my-pipeline"))
		assert.Expect(receivedReq["driver_dsn"]).To(Equal("docker://"))
		assert.Expect(receivedReq["content"]).To(ContainSubstring("pipeline"))
	})

	t.Run("uploads a valid TypeScript pipeline", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		var receivedReq map[string]string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&receivedReq)
			resp := storage.Pipeline{ID: "test-id", Name: receivedReq["name"]}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		pipelineFile := filepath.Join(tmpDir, "typed-pipeline.ts")
		err := os.WriteFile(pipelineFile, []byte(`
const pipeline = async (): Promise<void> => {
	const x: string = "hello";
	console.log(x);
};
export { pipeline };
`), 0644)
		assert.Expect(err).NotTo(HaveOccurred())

		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: server.URL,
		}

		err = cmd.Run(slog.Default())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(receivedReq["name"]).To(Equal("typed-pipeline"))
	})

	t.Run("uses custom name when provided", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		var receivedReq map[string]string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&receivedReq)
			resp := storage.Pipeline{ID: "test-id", Name: receivedReq["name"]}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		pipelineFile := filepath.Join(tmpDir, "file.js")
		err := os.WriteFile(pipelineFile, []byte(`
const pipeline = async () => {};
export { pipeline };
`), 0644)
		assert.Expect(err).NotTo(HaveOccurred())

		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			Name:      "custom-name",
			ServerURL: server.URL,
		}

		err = cmd.Run(slog.Default())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(receivedReq["name"]).To(Equal("custom-name"))
	})

	t.Run("fails on invalid syntax", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("server should not be called for invalid syntax")
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		pipelineFile := filepath.Join(tmpDir, "bad.js")
		err := os.WriteFile(pipelineFile, []byte(`
const pipeline = async ( => {
	console.log("hello");
};
`), 0644)
		assert.Expect(err).NotTo(HaveOccurred())

		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: server.URL,
		}

		err = cmd.Run(slog.Default())
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("validation failed"))
	})

	t.Run("handles server error gracefully", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "database error"})
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		pipelineFile := filepath.Join(tmpDir, "pipeline.js")
		err := os.WriteFile(pipelineFile, []byte(`
const pipeline = async () => {};
export { pipeline };
`), 0644)
		assert.Expect(err).NotTo(HaveOccurred())

		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: server.URL,
		}

		err = cmd.Run(slog.Default())
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("database error"))
	})

	t.Run("rejects unsupported file extensions", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		tmpDir := t.TempDir()
		pipelineFile := filepath.Join(tmpDir, "pipeline.txt")
		err := os.WriteFile(pipelineFile, []byte(`some content`), 0644)
		assert.Expect(err).NotTo(HaveOccurred())

		cmd := commands.SetPipeline{
			Pipeline:  pipelineFile,
			ServerURL: "http://localhost:8080",
		}

		err = cmd.Run(slog.Default())
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("unsupported file extension"))
	})
}
