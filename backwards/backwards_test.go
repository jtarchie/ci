package backwards_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/jtarchie/ci/backwards"
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

func createLogger() (*strings.Builder, *slog.Logger) {
	logs := &strings.Builder{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	return logs, logger
}

func TestBackwardsCompatibility(t *testing.T) {
	t.Parallel()

	t.Run("on_failure", func(t *testing.T) {
		t.Parallel()

		logs, logger := createLogger()

		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline: "steps/on_failure.yml",
			Driver:   "native",
			Storage:  "sqlite://:memory:",
		}
		err := runner.Run(logger)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(logs.String()).To(ContainSubstring("failing-task failed with code 1"))
	})

	t.Run("on_success", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline: "steps/on_success.yml",
			Driver:   "native",
			Storage:  "sqlite://:memory:",
		}
		err := runner.Run(nil)
		assert.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("ensure", func(t *testing.T) {
		t.Parallel()

		logs, logger := createLogger()
		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline: "steps/ensure.yml",
			Driver:   "native",
			Storage:  "sqlite://:memory:",
		}
		err := runner.Run(logger)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(logs.String()).To(ContainSubstring("ensure-task failed with code 1"))
	})

	t.Run("do", func(t *testing.T) {
		t.Parallel()

		logs, logger := createLogger()
		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline: "steps/do.yml",
			Driver:   "native",
			Storage:  "sqlite://:memory:",
		}
		err := runner.Run(logger)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(logs.String()).To(ContainSubstring("ensure-task failed with code 11"))
	})

	t.Run("try", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline: "steps/try.yml",
			Driver:   "native",
			Storage:  "sqlite://:memory:",
		}
		err := runner.Run(nil)
		assert.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("all", func(t *testing.T) {
		t.Parallel()

		logs, logger := createLogger()
		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline: "steps/all.yml",
			Driver:   "native",
			Storage:  "sqlite://:memory:",
		}
		err := runner.Run(logger)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(logs.String()).To(ContainSubstring(`assert`))
		assert.Expect(strings.Count(logs.String(), `assert`)).To(Equal(21))
	})

	t.Run("on_error", func(t *testing.T) {
		t.Parallel()

		logs, logger := createLogger()
		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline: "steps/on_error.yml",
			Driver:   "native",
			Storage:  "sqlite://:memory:",
		}
		err := runner.Run(logger)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(logs.String()).To(ContainSubstring("Task erroring-task errored"))
		assert.Expect(logs.String()).To(ContainSubstring(`assert`))
		assert.Expect(strings.Count(logs.String(), `assert`)).To(Equal(12))
	})

	t.Run("on_abort", func(t *testing.T) {
		t.Parallel()

		logs, logger := createLogger()
		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline: "steps/on_abort.yml",
			Driver:   "native",
			Storage:  "sqlite://:memory:",
		}
		err := runner.Run(logger)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(logs.String()).To(ContainSubstring("Task abort-task aborted"))
	})

	t.Run("task/file", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline: "steps/task_file.yml",
			Driver:   "native",
			Storage:  "sqlite://:memory:",
		}
		err := runner.Run(nil)
		assert.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("mutate job asserts", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		matches, err := filepath.Glob("steps/*.yml")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(matches).NotTo(BeEmpty())

		for _, match := range matches {
			// Capture the variable for the closure
			t.Run(filepath.Base(match), func(t *testing.T) {
				t.Parallel()

				assert := NewGomegaWithT(t)
				contents, err := os.ReadFile(match)
				assert.Expect(err).NotTo(HaveOccurred())

				var config backwards.Config

				err = yaml.UnmarshalWithOptions(contents, &config, yaml.Strict())
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(config.Assert.Execution).NotTo(BeEmpty())

				config.Assert.Execution[0] = "unknown-job"

				file, err := os.CreateTemp(t.TempDir(), "*.yml")
				assert.Expect(err).NotTo(HaveOccurred())

				defer func() { _ = os.Remove(file.Name()) }()

				contents, err = yaml.MarshalWithOptions(config)
				assert.Expect(err).NotTo(HaveOccurred())
				_, err = file.Write(contents)
				assert.Expect(err).NotTo(HaveOccurred())
				assert.Expect(file.Close()).NotTo(HaveOccurred())

				runner := commands.Runner{
					Pipeline: file.Name(),
					Driver:   "native",
					Storage:  "sqlite://:memory:",
				}
				err = runner.Run(nil)

				assert.Expect(err).To(HaveOccurred())
				assert.Expect(err.Error()).To(ContainSubstring("assertion failed"))
			})
		}
	})

	t.Run("mutate step asserts", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		matches, err := filepath.Glob("steps/*.yml")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(matches).NotTo(BeEmpty())

		for _, match := range matches {
			t.Run(filepath.Base(match), func(t *testing.T) {
				t.Parallel()

				assert := NewGomegaWithT(t)
				contents, err := os.ReadFile(match)
				assert.Expect(err).NotTo(HaveOccurred())

				var config backwards.Config

				err = yaml.UnmarshalWithOptions(contents, &config, yaml.Strict())
				assert.Expect(err).NotTo(HaveOccurred())

				// Collect all steps with assertions
				type stepLocation struct {
					jobIdx  int
					stepIdx int
					name    string
				}
				var stepsWithAssertions []stepLocation

				for i := range config.Jobs {
					for j := range config.Jobs[i].Plan {
						step := &config.Jobs[i].Plan[j]
						if step.Assert != nil {
							taskName := step.Task
							if taskName == "" {
								taskName = fmt.Sprintf("step-%d", j)
							}
							stepsWithAssertions = append(stepsWithAssertions, stepLocation{
								jobIdx:  i,
								stepIdx: j,
								name:    taskName,
							})
						}
					}
				}

				// Skip files without step-level assertions
				if len(stepsWithAssertions) == 0 {
					t.Skip("No step-level assertions found")
					return
				}

				// Test each step's assertion independently
				for _, loc := range stepsWithAssertions {
					t.Run(loc.name, func(t *testing.T) {
						assert := NewGomegaWithT(t)

						// Make a deep copy of config for this test
						var testConfig backwards.Config
						configBytes, err := yaml.MarshalWithOptions(config)
						assert.Expect(err).NotTo(HaveOccurred())
						err = yaml.UnmarshalWithOptions(configBytes, &testConfig, yaml.Strict())
						assert.Expect(err).NotTo(HaveOccurred())

						// Mutate only this specific step's assertion
						step := &testConfig.Jobs[loc.jobIdx].Plan[loc.stepIdx]
						if step.Assert.Code != nil {
							// Change expected exit code
							wrongCode := *step.Assert.Code + 1
							step.Assert.Code = &wrongCode
						} else if step.Assert.Stdout != "" {
							// Change expected stdout
							step.Assert.Stdout = "THIS-WILL-NOT-MATCH-" + step.Assert.Stdout
						} else if step.Assert.Stderr != "" {
							// Change expected stderr
							step.Assert.Stderr = "THIS-WILL-NOT-MATCH-" + step.Assert.Stderr
						}

						file, err := os.CreateTemp(t.TempDir(), "*.yml")
						assert.Expect(err).NotTo(HaveOccurred())

						defer func() { _ = os.Remove(file.Name()) }()

						contents, err := yaml.MarshalWithOptions(testConfig)
						assert.Expect(err).NotTo(HaveOccurred())
						_, err = file.Write(contents)
						assert.Expect(err).NotTo(HaveOccurred())
						assert.Expect(file.Close()).NotTo(HaveOccurred())

						runner := commands.Runner{
							Pipeline: file.Name(),
							Driver:   "native",
							Storage:  "sqlite://:memory:",
						}
						err = runner.Run(nil)

						assert.Expect(err).To(HaveOccurred())
						assert.Expect(err.Error()).To(ContainSubstring("assertion failed"))
					})
				}
			})
		}
	})

	t.Run("resource version modes", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline: "versions/modes.yml",
			Driver:   "native",
			Storage:  "sqlite://:memory:",
		}
		err := runner.Run(nil)
		assert.Expect(err).NotTo(HaveOccurred())
	})
}

func TestVersionsWithDocker(t *testing.T) {
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

		pipelineFile := "versions/time-resource.yml"

		// Run 1: Should fetch the first time version
		runner1 := commands.Runner{
			Pipeline: pipelineFile,
			Driver:   "docker",
			Storage:  storageURL,
		}
		err := runner1.Run(nil)
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
