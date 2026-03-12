package s3_test

import (
	"context"
	"log/slog"
	"os/exec"
	"testing"

	"github.com/jtarchie/pocketci/storage"
	s3storage "github.com/jtarchie/pocketci/storage/s3"
	"github.com/jtarchie/pocketci/testhelpers"
	. "github.com/onsi/gomega"
)

func setupS3(t *testing.T) storage.Driver {
	t.Helper()

	if _, err := exec.LookPath("minio"); err != nil {
		t.Skip("minio not installed, skipping S3 storage test")
	}

	server := testhelpers.StartMinIO(t)
	t.Cleanup(server.Stop)

	dsn := server.CacheURL()

	client, err := s3storage.NewS3(dsn, "namespace", slog.Default())

	assert := NewGomegaWithT(t)
	assert.Expect(err).NotTo(HaveOccurred())

	t.Cleanup(func() { _ = client.Close() })

	return client
}

func TestS3Driver_SetAndGet(t *testing.T) {
	client := setupS3(t)
	assert := NewGomegaWithT(t)
	ctx := context.Background()

	err := client.Set(ctx, "/foo", map[string]string{
		"field":   "123",
		"another": "456",
	})
	assert.Expect(err).NotTo(HaveOccurred())

	payload, err := client.Get(ctx, "/foo")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(payload["field"]).To(Equal("123"))
	assert.Expect(payload["another"]).To(Equal("456"))
}

func TestS3Driver_SetMerge(t *testing.T) {
	client := setupS3(t)
	assert := NewGomegaWithT(t)
	ctx := context.Background()

	err := client.Set(ctx, "/merge-test", map[string]string{
		"a": "1",
		"b": "2",
	})
	assert.Expect(err).NotTo(HaveOccurred())

	err = client.Set(ctx, "/merge-test", map[string]string{
		"b": "updated",
		"c": "3",
	})
	assert.Expect(err).NotTo(HaveOccurred())

	payload, err := client.Get(ctx, "/merge-test")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(payload["a"]).To(Equal("1"))
	assert.Expect(payload["b"]).To(Equal("updated"))
	assert.Expect(payload["c"]).To(Equal("3"))
}

func TestS3Driver_GetNotFound(t *testing.T) {
	client := setupS3(t)
	assert := NewGomegaWithT(t)
	ctx := context.Background()

	_, err := client.Get(ctx, "/nonexistent")
	assert.Expect(err).To(MatchError(storage.ErrNotFound))
}

func TestS3Driver_GetAll(t *testing.T) {
	client := setupS3(t)
	assert := NewGomegaWithT(t)
	ctx := context.Background()

	err := client.Set(ctx, "/items/a", map[string]any{
		"field":   "123",
		"another": "456",
	})
	assert.Expect(err).NotTo(HaveOccurred())

	results, err := client.GetAll(ctx, "/items", []string{"field"})
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(results).To(HaveLen(1))
	assert.Expect(results[0].Payload["field"]).To(Equal("123"))
	assert.Expect(results[0].Payload).NotTo(HaveKey("another"))

	results, err = client.GetAll(ctx, "/items", []string{"*"})
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(results).To(HaveLen(1))
	assert.Expect(results[0].Payload["field"]).To(Equal("123"))
	assert.Expect(results[0].Payload["another"]).To(Equal("456"))
}

func TestS3Driver_UpdateStatusForPrefix(t *testing.T) {
	client := setupS3(t)
	assert := NewGomegaWithT(t)
	ctx := context.Background()

	err := client.Set(ctx, "/tasks/1", map[string]string{"status": "running", "name": "task1"})
	assert.Expect(err).NotTo(HaveOccurred())

	err = client.Set(ctx, "/tasks/2", map[string]string{"status": "pending", "name": "task2"})
	assert.Expect(err).NotTo(HaveOccurred())

	err = client.UpdateStatusForPrefix(ctx, "/tasks", []string{"running"}, "cancelled")
	assert.Expect(err).NotTo(HaveOccurred())

	p1, err := client.Get(ctx, "/tasks/1")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(p1["status"]).To(Equal("cancelled"))
	assert.Expect(p1["name"]).To(Equal("task1"))

	p2, err := client.Get(ctx, "/tasks/2")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(p2["status"]).To(Equal("pending"))
}

func TestS3Driver_PipelineCRUD(t *testing.T) {
	client := setupS3(t)
	assert := NewGomegaWithT(t)
	ctx := context.Background()

	pipeline, err := client.SavePipeline(ctx, "test-pipeline", "console.log('hello');", "docker://", "js")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(pipeline.ID).NotTo(BeEmpty())
	assert.Expect(pipeline.Name).To(Equal("test-pipeline"))
	assert.Expect(pipeline.Content).To(Equal("console.log('hello');"))
	assert.Expect(pipeline.DriverDSN).To(Equal("docker://"))
	assert.Expect(pipeline.ContentType).To(Equal("js"))

	retrieved, err := client.GetPipeline(ctx, pipeline.ID)
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(retrieved.Name).To(Equal("test-pipeline"))

	byName, err := client.GetPipelineByName(ctx, "test-pipeline")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(byName.ID).To(Equal(pipeline.ID))

	updated, err := client.SavePipeline(ctx, "test-pipeline", "console.log('updated');", "docker://", "js")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(updated.ID).To(Equal(pipeline.ID))

	_, err = client.GetPipeline(ctx, "nonexistent-id")
	assert.Expect(err).To(MatchError(storage.ErrNotFound))

	err = client.DeletePipeline(ctx, pipeline.ID)
	assert.Expect(err).NotTo(HaveOccurred())

	_, err = client.GetPipeline(ctx, pipeline.ID)
	assert.Expect(err).To(MatchError(storage.ErrNotFound))
}

func TestS3Driver_RunLifecycle(t *testing.T) {
	client := setupS3(t)
	assert := NewGomegaWithT(t)
	ctx := context.Background()

	pipeline, err := client.SavePipeline(ctx, "run-pipeline", "code", "docker://", "js")
	assert.Expect(err).NotTo(HaveOccurred())

	run, err := client.SaveRun(ctx, pipeline.ID)
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(run.Status).To(Equal(storage.RunStatusQueued))
	assert.Expect(run.StartedAt).To(BeNil())

	retrieved, err := client.GetRun(ctx, run.ID)
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(retrieved.PipelineID).To(Equal(pipeline.ID))

	err = client.UpdateRunStatus(ctx, run.ID, storage.RunStatusRunning, "")
	assert.Expect(err).NotTo(HaveOccurred())

	retrieved, err = client.GetRun(ctx, run.ID)
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(retrieved.Status).To(Equal(storage.RunStatusRunning))
	assert.Expect(retrieved.StartedAt).NotTo(BeNil())

	err = client.UpdateRunStatus(ctx, run.ID, storage.RunStatusSuccess, "")
	assert.Expect(err).NotTo(HaveOccurred())

	retrieved, err = client.GetRun(ctx, run.ID)
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(retrieved.Status).To(Equal(storage.RunStatusSuccess))
	assert.Expect(retrieved.CompletedAt).NotTo(BeNil())

	_, err = client.GetRun(ctx, "nonexistent-run")
	assert.Expect(err).To(MatchError(storage.ErrNotFound))
}

func TestS3Driver_SearchRunsByPipeline(t *testing.T) {
	client := setupS3(t)
	assert := NewGomegaWithT(t)
	ctx := context.Background()

	pipeline, err := client.SavePipeline(ctx, "search-run-pipeline", "code", "docker://", "js")
	assert.Expect(err).NotTo(HaveOccurred())

	_, err = client.SaveRun(ctx, pipeline.ID)
	assert.Expect(err).NotTo(HaveOccurred())

	run2, err := client.SaveRun(ctx, pipeline.ID)
	assert.Expect(err).NotTo(HaveOccurred())

	err = client.UpdateRunStatus(ctx, run2.ID, storage.RunStatusFailed, "something broke")
	assert.Expect(err).NotTo(HaveOccurred())

	result, err := client.SearchRunsByPipeline(ctx, pipeline.ID, "", 1, 10)
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result.TotalItems).To(Equal(2))

	result, err = client.SearchRunsByPipeline(ctx, pipeline.ID, "failed", 1, 10)
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result.TotalItems).To(Equal(1))

	result, err = client.SearchRunsByPipeline(ctx, pipeline.ID, "broke", 1, 10)
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result.TotalItems).To(Equal(1))
}

func TestS3Driver_SearchPipelines(t *testing.T) {
	client := setupS3(t)
	assert := NewGomegaWithT(t)
	ctx := context.Background()

	_, err := client.SavePipeline(ctx, "alpha-pipeline", "first content", "docker://", "js")
	assert.Expect(err).NotTo(HaveOccurred())

	_, err = client.SavePipeline(ctx, "beta-pipeline", "second content", "docker://", "js")
	assert.Expect(err).NotTo(HaveOccurred())

	result, err := client.SearchPipelines(ctx, "", 1, 10)
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result.TotalItems).To(Equal(2))

	result, err = client.SearchPipelines(ctx, "alpha", 1, 10)
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result.TotalItems).To(Equal(1))
	assert.Expect(result.Items[0].Name).To(Equal("alpha-pipeline"))

	result, err = client.SearchPipelines(ctx, "second", 1, 10)
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result.TotalItems).To(Equal(1))
	assert.Expect(result.Items[0].Name).To(Equal("beta-pipeline"))
}

func TestS3Driver_Search(t *testing.T) {
	client := setupS3(t)
	assert := NewGomegaWithT(t)
	ctx := context.Background()

	err := client.Set(ctx, "/run/1/step/build", map[string]any{
		"status": "success",
		"stdout": "building project...",
	})
	assert.Expect(err).NotTo(HaveOccurred())

	err = client.Set(ctx, "/run/1/step/test", map[string]any{
		"status": "failed",
		"stdout": "test failed: assert error",
	})
	assert.Expect(err).NotTo(HaveOccurred())

	results, err := client.Search(ctx, "/run/1", "assert")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(results).To(HaveLen(1))

	results, err = client.Search(ctx, "/run/1", "")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(results).To(BeNil())
}

// TestS3Driver_EncryptWithSseS3 verifies that a driver can be constructed with
// encrypt=sse-s3 in the DSN. Actual server-side encryption requires a KMS-enabled
// S3-compatible service; correct parsing is also exercised in s3config tests.
func TestS3Driver_EncryptWithSseS3(t *testing.T) {
	assert := NewGomegaWithT(t)

	// Construction succeeds — no real S3 calls needed to verify config parsing.
	client, err := s3storage.NewS3("s3://s3.amazonaws.com/bucket?region=us-east-1&encrypt=sse-s3", "sse-ns", slog.Default())
	assert.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = client.Close() })
}

// TestS3Driver_DSNParams verifies that non-default DSN parameters (force_path_style,
// sse) are accepted without error during driver construction.
func TestS3Driver_DSNParams(t *testing.T) {
	assert := NewGomegaWithT(t)

	// force_path_style=false is a valid param; construction must not return an error.
	client, err := s3storage.NewS3("s3://s3.amazonaws.com/bucket?region=us-east-1&force_path_style=false", "params-ns", slog.Default())
	assert.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = client.Close() })
}

// TestS3Driver_InvalidEncrypt verifies that an unsupported encrypt param value is rejected
// at construction time, before any requests are made.
func TestS3Driver_InvalidEncrypt(t *testing.T) {
	assert := NewGomegaWithT(t)

	_, err := s3storage.NewS3("s3://s3.amazonaws.com/bucket?region=us-east-1&encrypt=bogus", "ns", slog.Default())
	assert.Expect(err).To(HaveOccurred())
	assert.Expect(err.Error()).To(ContainSubstring("unsupported encrypt value"))
}
