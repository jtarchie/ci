package main_test

import (
	"path/filepath"
	"testing"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/jtarchie/ci/commands"
	_ "github.com/jtarchie/ci/orchestra/docker"
	"github.com/jtarchie/ci/orchestra/k8s"
	_ "github.com/jtarchie/ci/orchestra/native"
	_ "github.com/jtarchie/ci/storage/sqlite"
	. "github.com/onsi/gomega"
)

func TestExamplesDocker(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	matches, err := doublestar.FilepathGlob("docker/*.{js,ts,yml,yaml}")
	assert.Expect(err).NotTo(HaveOccurred())

	// Check if k8s is available
	drivers := []string{"docker"}
	if k8s.IsAvailable() {
		drivers = append(drivers, "k8s")
	}

	for _, match := range matches {
		examplePath, err := filepath.Abs(match)
		assert.Expect(err).NotTo(HaveOccurred())

		for _, driver := range drivers {
			t.Run(driver+": "+match, func(t *testing.T) {
				t.Parallel()

				assert := NewGomegaWithT(t)
				runner := commands.Runner{
					Pipeline: examplePath,
					Driver:   driver,
					Storage:  "sqlite://:memory:",
				}
				err := runner.Run(nil)
				assert.Expect(err).NotTo(HaveOccurred())
			})
		}
	}
}

func TestExamplesAll(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	matches, err := doublestar.FilepathGlob("both/*.{js,ts,yml,yaml}")
	assert.Expect(err).NotTo(HaveOccurred())

	drivers := []string{
		"docker",
		"native",
	}

	for _, match := range matches {
		examplePath, err := filepath.Abs(match)
		assert.Expect(err).NotTo(HaveOccurred())

		for _, driver := range drivers {
			t.Run(driver+": "+match, func(t *testing.T) {
				t.Parallel()

				assert := NewGomegaWithT(t)
				runner := commands.Runner{
					Pipeline: examplePath,
					Driver:   driver,
					Storage:  "sqlite://:memory:",
				}
				err := runner.Run(nil)
				assert.Expect(err).NotTo(HaveOccurred())
			})
		}
	}
}
