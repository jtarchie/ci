package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jtarchie/ci/server"
	"github.com/jtarchie/ci/storage"
	_ "github.com/jtarchie/ci/storage/sqlite"
	. "github.com/onsi/gomega"
)

func TestPipelineAPI(t *testing.T) {
	t.Parallel()

	storage.Each(func(name string, init storage.InitFunc) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			t.Run("POST /api/pipelines creates a pipeline", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{})
				assert.Expect(err).NotTo(HaveOccurred())

				body := map[string]string{
					"name":       "test-pipeline",
					"content":    "export { pipeline };",
					"driver_dsn": "docker://",
				}
				jsonBody, _ := json.Marshal(body)

				req := httptest.NewRequest(http.MethodPost, "/api/pipelines", bytes.NewReader(jsonBody))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusCreated))

				var resp map[string]any
				err = json.Unmarshal(rec.Body.Bytes(), &resp)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(resp["id"]).NotTo(BeNil())
				assert.Expect(resp["name"]).To(Equal("test-pipeline"))
				assert.Expect(resp["content"]).To(Equal("export { pipeline };"))
				_, hasDriver := resp["driver_dsn"]
				assert.Expect(hasDriver).To(BeFalse())
			})

			t.Run("POST /api/pipelines returns 400 for missing name", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{})
				assert.Expect(err).NotTo(HaveOccurred())

				body := map[string]string{
					"content": "export { pipeline };",
				}
				jsonBody, _ := json.Marshal(body)

				req := httptest.NewRequest(http.MethodPost, "/api/pipelines", bytes.NewReader(jsonBody))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusBadRequest))
			})

			t.Run("GET /api/pipelines lists all pipelines", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				_, err = client.SavePipeline(context.Background(), "pipeline-1", "content1", "docker://", "")
				assert.Expect(err).NotTo(HaveOccurred())

				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{})
				assert.Expect(err).NotTo(HaveOccurred())

				req := httptest.NewRequest(http.MethodGet, "/api/pipelines", nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusOK))

				var result map[string]any
				err = json.Unmarshal(rec.Body.Bytes(), &result)
				assert.Expect(err).NotTo(HaveOccurred())
				items, ok := result["items"].([]any)
				assert.Expect(ok).To(BeTrue())
				assert.Expect(items).To(HaveLen(1))
				item, ok := items[0].(map[string]any)
				assert.Expect(ok).To(BeTrue())
				_, hasDriver := item["driver_dsn"]
				assert.Expect(hasDriver).To(BeFalse())
			})

			t.Run("GET /api/pipelines/:id retrieves a pipeline", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				saved, err := client.SavePipeline(context.Background(), "my-pipeline", "content", "docker://", "")
				assert.Expect(err).NotTo(HaveOccurred())

				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{})
				assert.Expect(err).NotTo(HaveOccurred())

				req := httptest.NewRequest(http.MethodGet, "/api/pipelines/"+saved.ID, nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusOK))

				var resp map[string]any
				err = json.Unmarshal(rec.Body.Bytes(), &resp)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(resp["id"]).To(Equal(saved.ID))
				assert.Expect(resp["name"]).To(Equal("my-pipeline"))
				_, hasDriver := resp["driver_dsn"]
				assert.Expect(hasDriver).To(BeFalse())
			})

			t.Run("GET /api/pipelines/:id returns 404 for non-existent", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{})
				assert.Expect(err).NotTo(HaveOccurred())

				req := httptest.NewRequest(http.MethodGet, "/api/pipelines/non-existent", nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusNotFound))
			})

			t.Run("DELETE /api/pipelines/:id deletes a pipeline", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				saved, err := client.SavePipeline(context.Background(), "to-delete", "content", "docker://", "")
				assert.Expect(err).NotTo(HaveOccurred())

				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{})
				assert.Expect(err).NotTo(HaveOccurred())

				req := httptest.NewRequest(http.MethodDelete, "/api/pipelines/"+saved.ID, nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusNoContent))

				// Verify it's deleted
				_, err = client.GetPipeline(context.Background(), saved.ID)
				assert.Expect(err).To(Equal(storage.ErrNotFound))
			})

			t.Run("DELETE /api/pipelines/:id returns 404 for non-existent", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{})
				assert.Expect(err).NotTo(HaveOccurred())

				req := httptest.NewRequest(http.MethodDelete, "/api/pipelines/non-existent", nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusNotFound))
			})
		})
	})
}
