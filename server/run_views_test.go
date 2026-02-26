package server_test

import (
	"context"
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

func TestRunViews(t *testing.T) {
	t.Parallel()

	storage.Each(func(name string, init storage.InitFunc) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			t.Run("GET /runs/:id/tasks returns HTML with task tree", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				// Create a pipeline and run
				pipeline, err := client.SavePipeline(context.Background(), "test-pipeline", "export const pipeline = async () => {};", "docker://", "")
				assert.Expect(err).NotTo(HaveOccurred())

				run, err := client.SaveRun(context.Background(), pipeline.ID)
				assert.Expect(err).NotTo(HaveOccurred())

				// Store some task data at the expected path
				err = client.Set(context.Background(), "/pipeline/"+run.ID+"/jobs/test-job", map[string]any{"status": "success"})
				assert.Expect(err).NotTo(HaveOccurred())

				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{})
				assert.Expect(err).NotTo(HaveOccurred())

				req := httptest.NewRequest(http.MethodGet, "/runs/"+run.ID+"/tasks", nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusOK))
				assert.Expect(rec.Header().Get("Content-Type")).To(ContainSubstring("text/html"))
				assert.Expect(rec.Body.String()).To(ContainSubstring(">Tasks</h1>"))
				assert.Expect(rec.Body.String()).To(ContainSubstring("test-job"))
			})

			t.Run("GET /runs/:id/graph returns HTML with graph view", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				// Create a pipeline and run
				pipeline, err := client.SavePipeline(context.Background(), "test-pipeline", "export const pipeline = async () => {};", "docker://", "")
				assert.Expect(err).NotTo(HaveOccurred())

				run, err := client.SaveRun(context.Background(), pipeline.ID)
				assert.Expect(err).NotTo(HaveOccurred())

				// Store some task data at the expected path
				err = client.Set(context.Background(), "/pipeline/"+run.ID+"/jobs/test-job", map[string]any{"status": "success", "dependsOn": []string{}})
				assert.Expect(err).NotTo(HaveOccurred())

				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{})
				assert.Expect(err).NotTo(HaveOccurred())

				req := httptest.NewRequest(http.MethodGet, "/runs/"+run.ID+"/graph", nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusOK))
				assert.Expect(rec.Header().Get("Content-Type")).To(ContainSubstring("text/html"))
				assert.Expect(rec.Body.String()).To(ContainSubstring("Task Graph"))
				assert.Expect(rec.Body.String()).To(ContainSubstring("test-job"))
			})

			t.Run("GET /runs/:id/tasks returns empty tree for non-existent run", func(t *testing.T) {
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

				req := httptest.NewRequest(http.MethodGet, "/runs/non-existent-run/tasks", nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				// Should still return 200 with empty tree
				assert.Expect(rec.Code).To(Equal(http.StatusOK))
				assert.Expect(rec.Header().Get("Content-Type")).To(ContainSubstring("text/html"))
			})

			t.Run("GET /runs/:id/tasks includes RunID in template data", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				// Create a pipeline and run
				pipeline, err := client.SavePipeline(context.Background(), "test-pipeline", "export const pipeline = async () => {};", "docker://", "")
				assert.Expect(err).NotTo(HaveOccurred())

				run, err := client.SaveRun(context.Background(), pipeline.ID)
				assert.Expect(err).NotTo(HaveOccurred())

				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{})
				assert.Expect(err).NotTo(HaveOccurred())

				req := httptest.NewRequest(http.MethodGet, "/runs/"+run.ID+"/tasks", nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusOK))
				// The template should show "Run <runID>" in breadcrumb
				assert.Expect(rec.Body.String()).To(ContainSubstring("Run " + run.ID))
			})

			t.Run("GET /runs/:id/graph includes correct link to tasks view", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				// Create a pipeline and run
				pipeline, err := client.SavePipeline(context.Background(), "test-pipeline", "export const pipeline = async () => {};", "docker://", "")
				assert.Expect(err).NotTo(HaveOccurred())

				run, err := client.SaveRun(context.Background(), pipeline.ID)
				assert.Expect(err).NotTo(HaveOccurred())

				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{})
				assert.Expect(err).NotTo(HaveOccurred())

				req := httptest.NewRequest(http.MethodGet, "/runs/"+run.ID+"/graph", nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusOK))
				// The template should have a link to /runs/:id/tasks
				assert.Expect(rec.Body.String()).To(ContainSubstring("/runs/" + run.ID + "/tasks"))
			})

			t.Run("GET /runs/:id/tasks shows execution number for single task", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				pipeline, err := client.SavePipeline(context.Background(), "k6-pipeline", "export const pipeline = async () => {};", "docker://", "")
				assert.Expect(err).NotTo(HaveOccurred())

				run, err := client.SaveRun(context.Background(), pipeline.ID)
				assert.Expect(err).NotTo(HaveOccurred())

				// Single task - mirrors the k6 pipeline structure
				err = client.Set(context.Background(), "/pipeline/"+run.ID+"/tasks/0-k6", map[string]any{"status": "success"})
				assert.Expect(err).NotTo(HaveOccurred())

				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{})
				assert.Expect(err).NotTo(HaveOccurred())

				req := httptest.NewRequest(http.MethodGet, "/runs/"+run.ID+"/tasks", nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusOK))
				// The execution number badge must not be empty — <no value> must not appear
				assert.Expect(rec.Body.String()).NotTo(ContainSubstring("no value"))
				// The number "1" must appear inside the badge span
				assert.Expect(rec.Body.String()).To(MatchRegexp(`w-6 h-6[^>]*>\s*1\s*<`))
			})

			t.Run("GET /runs/:id/tasks shows correct execution numbers for multiple tasks", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				pipeline, err := client.SavePipeline(context.Background(), "multi-pipeline", "export const pipeline = async () => {};", "docker://", "")
				assert.Expect(err).NotTo(HaveOccurred())

				run, err := client.SaveRun(context.Background(), pipeline.ID)
				assert.Expect(err).NotTo(HaveOccurred())

				err = client.Set(context.Background(), "/pipeline/"+run.ID+"/tasks/0-task-a", map[string]any{"status": "success"})
				assert.Expect(err).NotTo(HaveOccurred())
				err = client.Set(context.Background(), "/pipeline/"+run.ID+"/tasks/1-task-b", map[string]any{"status": "success"})
				assert.Expect(err).NotTo(HaveOccurred())

				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{})
				assert.Expect(err).NotTo(HaveOccurred())

				req := httptest.NewRequest(http.MethodGet, "/runs/"+run.ID+"/tasks", nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusOK))
				assert.Expect(rec.Body.String()).NotTo(ContainSubstring("no value"))
			})

			t.Run("GET /runs/:id/tasks includes correct link to graph view", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				// Create a pipeline and run
				pipeline, err := client.SavePipeline(context.Background(), "test-pipeline", "export const pipeline = async () => {};", "docker://", "")
				assert.Expect(err).NotTo(HaveOccurred())

				run, err := client.SaveRun(context.Background(), pipeline.ID)
				assert.Expect(err).NotTo(HaveOccurred())

				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{})
				assert.Expect(err).NotTo(HaveOccurred())

				req := httptest.NewRequest(http.MethodGet, "/runs/"+run.ID+"/tasks", nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusOK))
				// The template should have a link to /runs/:id/graph
				assert.Expect(rec.Body.String()).To(ContainSubstring("/runs/" + run.ID + "/graph"))
			})
		})
	})
}
