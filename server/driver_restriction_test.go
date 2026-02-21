package server_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jtarchie/ci/orchestra"
	_ "github.com/jtarchie/ci/orchestra/native"
	"github.com/jtarchie/ci/server"
	"github.com/jtarchie/ci/storage"
	_ "github.com/jtarchie/ci/storage/sqlite"
	. "github.com/onsi/gomega"
)

func TestDriverRestriction(t *testing.T) {
	t.Parallel()

	storage.Each(func(name string, init storage.InitFunc) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			t.Run("restricts drivers when AllowedDrivers is set", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				// Create router with only native driver allowed
				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{
					AllowedDrivers: "native",
				})
				assert.Expect(err).NotTo(HaveOccurred())

				// Try to create pipeline with docker driver (should fail)
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

				assert.Expect(rec.Code).To(Equal(http.StatusBadRequest))
				assert.Expect(rec.Body.String()).To(ContainSubstring("docker"))
				assert.Expect(rec.Body.String()).To(ContainSubstring("not allowed"))

				// Try to create pipeline with native driver (should succeed)
				body["driver_dsn"] = "native"
				jsonBody, _ = json.Marshal(body)

				req = httptest.NewRequest(http.MethodPost, "/api/pipelines", bytes.NewReader(jsonBody))
				req.Header.Set("Content-Type", "application/json")
				rec = httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusCreated))
			})

			t.Run("wildcard allows all drivers", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				// Create router with wildcard (default)
				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{
					AllowedDrivers: "*",
				})
				assert.Expect(err).NotTo(HaveOccurred())

				// Try to create pipeline with any driver (should succeed if DSN is valid)
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
			})

			t.Run("uses first allowed driver as default when not specified", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				// Create router with native,docker allowed (native should be default)
				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{
					AllowedDrivers: "native,docker",
				})
				assert.Expect(err).NotTo(HaveOccurred())

				// Create pipeline without specifying driver
				body := map[string]string{
					"name":    "test-pipeline",
					"content": "export { pipeline };",
				}
				jsonBody, _ := json.Marshal(body)

				req := httptest.NewRequest(http.MethodPost, "/api/pipelines", bytes.NewReader(jsonBody))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusCreated))

				var resp storage.Pipeline
				err = json.Unmarshal(rec.Body.Bytes(), &resp)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(resp.DriverDSN).To(Equal("native"))
			})

			t.Run("GET /api/drivers returns allowed drivers list", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				// Create router with specific drivers
				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{
					AllowedDrivers: "native,docker,k8s",
				})
				assert.Expect(err).NotTo(HaveOccurred())

				req := httptest.NewRequest(http.MethodGet, "/api/drivers", nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusOK))

				var resp map[string][]string
				err = json.Unmarshal(rec.Body.Bytes(), &resp)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(resp["drivers"]).To(ConsistOf("native", "docker", "k8s"))
			})

			t.Run("GET /api/drivers returns all registered drivers for wildcard", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				// Create router with wildcard
				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{
					AllowedDrivers: "*",
				})
				assert.Expect(err).NotTo(HaveOccurred())

				req := httptest.NewRequest(http.MethodGet, "/api/drivers", nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				assert.Expect(rec.Code).To(Equal(http.StatusOK))

				var resp map[string][]string
				err = json.Unmarshal(rec.Body.Bytes(), &resp)
				assert.Expect(err).NotTo(HaveOccurred())

				// Should return all registered drivers
				registeredDrivers := orchestra.ListDrivers()
				assert.Expect(len(resp["drivers"])).To(BeNumerically(">", 0))
				// Check that all returned drivers are registered
				for _, driver := range resp["drivers"] {
					assert.Expect(registeredDrivers).To(ContainElement(driver))
				}
			})

			t.Run("multiple drivers can be specified", func(t *testing.T) {
				t.Parallel()
				assert := NewGomegaWithT(t)

				buildFile, err := os.CreateTemp(t.TempDir(), "")
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = buildFile.Close() }()

				client, err := init(buildFile.Name(), "namespace", slog.Default())
				assert.Expect(err).NotTo(HaveOccurred())
				defer func() { _ = client.Close() }()

				// Create router with native,docker,k8s allowed
				router, err := server.NewRouter(slog.Default(), client, server.RouterOptions{
					AllowedDrivers: "native,docker,k8s",
				})
				assert.Expect(err).NotTo(HaveOccurred())

				// Test native (should succeed)
				body := map[string]string{
					"name":       "test-pipeline-native",
					"content":    "export { pipeline };",
					"driver_dsn": "native",
				}
				jsonBody, _ := json.Marshal(body)
				req := httptest.NewRequest(http.MethodPost, "/api/pipelines", bytes.NewReader(jsonBody))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				assert.Expect(rec.Code).To(Equal(http.StatusCreated))

				// Test docker (should succeed)
				body["name"] = "test-pipeline-docker"
				body["driver_dsn"] = "docker"
				jsonBody, _ = json.Marshal(body)
				req = httptest.NewRequest(http.MethodPost, "/api/pipelines", bytes.NewReader(jsonBody))
				req.Header.Set("Content-Type", "application/json")
				rec = httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				assert.Expect(rec.Code).To(Equal(http.StatusCreated))

				// Test k8s (should succeed)
				body["name"] = "test-pipeline-k8s"
				body["driver_dsn"] = "k8s://production"
				jsonBody, _ = json.Marshal(body)
				req = httptest.NewRequest(http.MethodPost, "/api/pipelines", bytes.NewReader(jsonBody))
				req.Header.Set("Content-Type", "application/json")
				rec = httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				assert.Expect(rec.Code).To(Equal(http.StatusCreated))

				// Test qemu (should fail - not in allowed list)
				body["name"] = "test-pipeline-qemu"
				body["driver_dsn"] = "qemu"
				jsonBody, _ = json.Marshal(body)
				req = httptest.NewRequest(http.MethodPost, "/api/pipelines", bytes.NewReader(jsonBody))
				req.Header.Set("Content-Type", "application/json")
				rec = httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				assert.Expect(rec.Code).To(Equal(http.StatusBadRequest))
				assert.Expect(rec.Body.String()).To(ContainSubstring("qemu"))
				assert.Expect(rec.Body.String()).To(ContainSubstring("not allowed"))
			})
		})
	})
}
