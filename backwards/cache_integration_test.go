package backwards_test

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"testing"

	gonanoid "github.com/matoous/go-nanoid/v2"

	"github.com/jtarchie/ci/commands"
	_ "github.com/jtarchie/ci/orchestra/cache/s3"
	_ "github.com/jtarchie/ci/orchestra/docker"
	_ "github.com/jtarchie/ci/orchestra/native"
	_ "github.com/jtarchie/ci/storage/sqlite"
	"github.com/jtarchie/ci/testhelpers"
	. "github.com/onsi/gomega"
)

// TestCacheS3Persistence tests that caches are persisted to S3 and restored
// across completely separate pipeline runs.
func TestCacheS3Persistence(t *testing.T) {
	// Skip if minio is not available
	if _, err := exec.LookPath("minio"); err != nil {
		t.Skip("minio not installed, skipping S3 cache integration test")
	}

	// Start MinIO server
	minio := testhelpers.StartMinIO(t)
	defer minio.Stop()

	cacheURL := minio.CacheURL()

	// Create a unique cache value so we know it came from S3
	cacheValue := gonanoid.Must()

	t.Run("docker", func(t *testing.T) {
		testCachePersistence(t, "docker://", cacheURL, cacheValue)
	})

	t.Run("native", func(t *testing.T) {
		testCachePersistence(t, "native://", cacheURL, cacheValue+"-native")
	})
}

func testCachePersistence(t *testing.T, driverDSN, cacheURL, cacheValue string) {
	assert := NewGomegaWithT(t)

	// Create temp directory for pipeline files
	tmpDir := t.TempDir()

	// Pipeline 1: Write to cache
	writePipeline := fmt.Sprintf(`---
jobs:
  - name: write-job
    plan:
      - task: write-cache
        config:
          platform: linux
          image_resource:
            type: registry-image
            source: { repository: busybox }
          caches:
            - path: mycache
          run:
            path: sh
            args:
              - -c
              - |
                  echo "Writing cache value: %s"
                  echo "%s" > ./mycache/value.txt
                  cat ./mycache/value.txt
        assert:
          stdout: "%s"
          code: 0
`, cacheValue, cacheValue, cacheValue)

	writePipelinePath := tmpDir + "/write-pipeline.yml"
	err := os.WriteFile(writePipelinePath, []byte(writePipeline), 0644)
	assert.Expect(err).NotTo(HaveOccurred())

	// Pipeline 2: Read from cache (should be restored from S3)
	readPipeline := fmt.Sprintf(`---
jobs:
  - name: read-job
    plan:
      - task: read-cache
        config:
          platform: linux
          image_resource:
            type: registry-image
            source: { repository: busybox }
          caches:
            - path: mycache
          run:
            path: sh
            args:
              - -c
              - |
                  echo "Reading cache value:"
                  cat ./mycache/value.txt
        assert:
          stdout: "%s"
          code: 0
`, cacheValue)

	readPipelinePath := tmpDir + "/read-pipeline.yml"
	err = os.WriteFile(readPipelinePath, []byte(readPipeline), 0644)
	assert.Expect(err).NotTo(HaveOccurred())

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// URL-encode the cache URL since it contains & and ? which would conflict with DSN parsing
	encodedCacheURL := url.QueryEscape(cacheURL)

	// Run pipeline 1: Write to cache
	t.Log("Running write pipeline...")
	runner1 := commands.Runner{
		Pipeline: writePipelinePath,
		Driver:   driverDSN + "?cache=" + encodedCacheURL + "&cache_compression=zstd&cache_prefix=test",
		Storage:  "sqlite://:memory:",
	}
	err = runner1.Run(logger)
	assert.Expect(err).NotTo(HaveOccurred(), "Write pipeline should succeed")

	// Run pipeline 2: Read from cache (completely new runner instance)
	// This tests that the cache was persisted to S3 and restored
	t.Log("Running read pipeline (should restore from S3)...")
	runner2 := commands.Runner{
		Pipeline: readPipelinePath,
		Driver:   driverDSN + "?cache=" + encodedCacheURL + "&cache_compression=zstd&cache_prefix=test",
		Storage:  "sqlite://:memory:",
	}
	err = runner2.Run(logger)
	assert.Expect(err).NotTo(HaveOccurred(), "Read pipeline should succeed - cache should be restored from S3")
}
