package commands_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jtarchie/ci/commands"
	_ "github.com/jtarchie/ci/orchestra/docker"
	_ "github.com/jtarchie/ci/orchestra/native"
	_ "github.com/jtarchie/ci/resources/mock"
	"github.com/jtarchie/ci/storage"
	_ "github.com/jtarchie/ci/storage/sqlite"
	. "github.com/onsi/gomega"
)

// youtubeIDStyle generates an 11-character ID from a hash of the input
// This matches the implementation in runner.go
func youtubeIDStyle(input string) string {
	hash := sha256.Sum256([]byte(input))
	encoded := base64.RawURLEncoding.EncodeToString(hash[:])

	const maxLength = 11
	if len(encoded) > maxLength {
		return encoded[:maxLength]
	}

	return encoded
}

func TestRunner(t *testing.T) {
	t.Parallel()

	t.Run("version persistence across runs", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		tempDir := t.TempDir()
		dbPath := filepath.Join(tempDir, "test.db")
		storageURL := fmt.Sprintf("sqlite://%s", dbPath)

		// Pipeline that uses version: every with a known version
		pipelineContent := `
resource_types:
  - name: mock
    type: registry-image
    source:
      repository: concourse/mock-resource

resources:
  - name: my-resource
    type: mock
    source:
      force_version: "v1.0.0"

jobs:
  - name: process-version
    plan:
      - get: my-resource
        version: every
      - task: show-version
        config:
          platform: linux
          image_resource:
            type: registry-image
            source:
              repository: busybox
          inputs:
            - name: my-resource
          run:
            path: cat
            args: ["my-resource/version"]
        assert:
          code: 0
          stdout: "v1.0.0"
`
		pipelineFile := filepath.Join(tempDir, "versions.yml")
		err := os.WriteFile(pipelineFile, []byte(pipelineContent), 0o600)
		assert.Expect(err).NotTo(HaveOccurred())

		// Run 1: Should fetch and store version "v1.0.0"
		runner1 := commands.Runner{
			Pipeline: pipelineFile,
			Driver:   "native",
			Storage:  storageURL,
		}
		err = runner1.Run(nil)
		assert.Expect(err).NotTo(HaveOccurred())

		// Open storage directly to verify the version was persisted
		pipelinePath, err := filepath.Abs(pipelineFile)
		assert.Expect(err).NotTo(HaveOccurred())
		runtimeID := youtubeIDStyle(pipelinePath)

		initStorage, found := storage.GetFromDSN(storageURL)
		assert.Expect(found).To(BeTrue())

		store, err := initStorage(storageURL, runtimeID, nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = store.Close() }()

		// The resource name is scoped by pipelineID: "{pipelineID}/{resourceName}"
		scopedResourceName := fmt.Sprintf("%s/%s", runtimeID, "my-resource")

		// Verify the version was saved
		ctx := context.Background()
		savedVersion, err := store.GetLatestResourceVersion(ctx, scopedResourceName)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(savedVersion).NotTo(BeNil())
		assert.Expect(savedVersion.Version).To(HaveKeyWithValue("version", "v1.0.0"))
		assert.Expect(savedVersion.ResourceName).To(Equal(scopedResourceName))

		// Run 2: Should recognize the version was already processed
		runner2 := commands.Runner{
			Pipeline: pipelineFile,
			Driver:   "native",
			Storage:  storageURL,
		}
		err = runner2.Run(nil)
		assert.Expect(err).NotTo(HaveOccurred())

		// Verify still only one version stored (no duplicates)
		versions, err := store.ListResourceVersions(ctx, scopedResourceName, 100)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(versions).To(HaveLen(1))
		assert.Expect(versions[0].Version).To(HaveKeyWithValue("version", "v1.0.0"))
	})

	t.Run("version isolation between pipelines", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		tempDir := t.TempDir()
		dbPath := filepath.Join(tempDir, "shared.db")
		storageURL := fmt.Sprintf("sqlite://%s", dbPath)

		// Two pipelines with same resource name but different versions
		pipeline1Content := `
resource_types:
  - name: mock
    type: registry-image
    source:
      repository: concourse/mock-resource

resources:
  - name: shared-resource
    type: mock
    source:
      force_version: "p1-v1"

jobs:
  - name: pipeline1-job
    plan:
      - get: shared-resource
      - task: show-version
        config:
          platform: linux
          image_resource:
            type: registry-image
            source:
              repository: busybox
          inputs:
            - name: shared-resource
          run:
            path: cat
            args: ["shared-resource/version"]
        assert:
          code: 0
          stdout: "p1-v1"
`
		pipeline2Content := `
resource_types:
  - name: mock
    type: registry-image
    source:
      repository: concourse/mock-resource

resources:
  - name: shared-resource
    type: mock
    source:
      force_version: "p2-v1"

jobs:
  - name: pipeline2-job
    plan:
      - get: shared-resource
      - task: show-version
        config:
          platform: linux
          image_resource:
            type: registry-image
            source:
              repository: busybox
          inputs:
            - name: shared-resource
          run:
            path: cat
            args: ["shared-resource/version"]
        assert:
          code: 0
          stdout: "p2-v1"
`

		pipeline1File := filepath.Join(tempDir, "pipeline1.yml")
		err := os.WriteFile(pipeline1File, []byte(pipeline1Content), 0o600)
		assert.Expect(err).NotTo(HaveOccurred())

		pipeline2File := filepath.Join(tempDir, "pipeline2.yml")
		err = os.WriteFile(pipeline2File, []byte(pipeline2Content), 0o600)
		assert.Expect(err).NotTo(HaveOccurred())

		// Run pipeline 1
		runner1 := commands.Runner{
			Pipeline: pipeline1File,
			Driver:   "native",
			Storage:  storageURL,
		}
		err = runner1.Run(nil)
		assert.Expect(err).NotTo(HaveOccurred())

		// Run pipeline 2
		runner2 := commands.Runner{
			Pipeline: pipeline2File,
			Driver:   "native",
			Storage:  storageURL,
		}
		err = runner2.Run(nil)
		assert.Expect(err).NotTo(HaveOccurred())

		// Open storage to verify isolation
		pipeline1Path, err := filepath.Abs(pipeline1File)
		assert.Expect(err).NotTo(HaveOccurred())
		pipeline1ID := youtubeIDStyle(pipeline1Path)

		pipeline2Path, err := filepath.Abs(pipeline2File)
		assert.Expect(err).NotTo(HaveOccurred())
		pipeline2ID := youtubeIDStyle(pipeline2Path)

		// Use pipeline1's ID to open storage (they share the same db)
		initStorage, found := storage.GetFromDSN(storageURL)
		assert.Expect(found).To(BeTrue())

		store, err := initStorage(storageURL, pipeline1ID, nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = store.Close() }()

		ctx := context.Background()

		// Verify pipeline 1's version
		p1ResourceName := fmt.Sprintf("%s/%s", pipeline1ID, "shared-resource")
		p1Version, err := store.GetLatestResourceVersion(ctx, p1ResourceName)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(p1Version).NotTo(BeNil())
		assert.Expect(p1Version.Version).To(HaveKeyWithValue("version", "p1-v1"))

		// Verify pipeline 2's version is stored separately
		p2ResourceName := fmt.Sprintf("%s/%s", pipeline2ID, "shared-resource")
		p2Version, err := store.GetLatestResourceVersion(ctx, p2ResourceName)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(p2Version).NotTo(BeNil())
		assert.Expect(p2Version.Version).To(HaveKeyWithValue("version", "p2-v1"))

		// Verify the resource names are different (isolation)
		assert.Expect(p1ResourceName).NotTo(Equal(p2ResourceName))
	})
}

func TestRunnerWithDocker(t *testing.T) {
	t.Parallel()

	// Skip if docker is not available
	if os.Getenv("CI_TEST_DOCKER") == "" {
		t.Skip("Skipping docker-based tests. Set CI_TEST_DOCKER=1 to enable.")
	}

	t.Run("version every with time-resource", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		tempDir := t.TempDir()
		dbPath := filepath.Join(tempDir, "test.db")
		storageURL := fmt.Sprintf("sqlite://%s", dbPath)

		// Use time-resource with a 1-second interval
		// Each check will return new versions as time passes
		pipelineContent := `
resource_types:
  - name: time
    type: registry-image
    source:
      repository: concourse/time-resource

resources:
  - name: timer
    type: time
    source:
      interval: 1s

jobs:
  - name: process-time
    plan:
      - get: timer
        version: every
      - task: show-time
        config:
          platform: linux
          image_resource:
            type: registry-image
            source:
              repository: busybox
          inputs:
            - name: timer
          run:
            path: sh
            args: ["-c", "cat timer/input && echo"]
        assert:
          code: 0
`
		pipelineFile := filepath.Join(tempDir, "time-versions.yml")
		err := os.WriteFile(pipelineFile, []byte(pipelineContent), 0o600)
		assert.Expect(err).NotTo(HaveOccurred())

		// Run 1: Should fetch the first time version
		runner1 := commands.Runner{
			Pipeline: pipelineFile,
			Driver:   "docker",
			Storage:  storageURL,
		}
		err = runner1.Run(nil)
		assert.Expect(err).NotTo(HaveOccurred())

		// Open storage to check version count
		pipelinePath, err := filepath.Abs(pipelineFile)
		assert.Expect(err).NotTo(HaveOccurred())
		runtimeID := youtubeIDStyle(pipelinePath)

		initStorage, found := storage.GetFromDSN(storageURL)
		assert.Expect(found).To(BeTrue())

		store, err := initStorage(storageURL, runtimeID, nil)
		assert.Expect(err).NotTo(HaveOccurred())
		defer func() { _ = store.Close() }()

		ctx := context.Background()
		scopedResourceName := fmt.Sprintf("%s/%s", runtimeID, "timer")

		// Verify a version was saved after run 1
		versions1, err := store.ListResourceVersions(ctx, scopedResourceName, 100)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(versions1).To(HaveLen(1))
		firstVersion := versions1[0].Version

		// Wait for the interval to pass so new versions are available
		time.Sleep(3 * time.Second)

		// Run 2: Should fetch a NEW time version (not the same one)
		runner2 := commands.Runner{
			Pipeline: pipelineFile,
			Driver:   "docker",
			Storage:  storageURL,
		}
		err = runner2.Run(nil)
		assert.Expect(err).NotTo(HaveOccurred())

		// Verify we now have 2 distinct versions
		versions2, err := store.ListResourceVersions(ctx, scopedResourceName, 100)
		assert.Expect(err).NotTo(HaveOccurred())
		t.Logf("After run 2, versions: %+v", versions2)
		assert.Expect(versions2).To(HaveLen(2))

		// Verify the versions are different
		secondVersion := versions2[0].Version // Most recent first
		assert.Expect(secondVersion).NotTo(Equal(firstVersion))

		// Run 3: Wait and get another version
		time.Sleep(3 * time.Second)

		runner3 := commands.Runner{
			Pipeline: pipelineFile,
			Driver:   "docker",
			Storage:  storageURL,
		}
		err = runner3.Run(nil)
		assert.Expect(err).NotTo(HaveOccurred())

		// Verify we now have 3 distinct versions
		versions3, err := store.ListResourceVersions(ctx, scopedResourceName, 100)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(versions3).To(HaveLen(3))
	})
}
