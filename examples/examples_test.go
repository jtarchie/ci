package main_test

import (
	"path/filepath"
	"testing"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/jtarchie/ci/commands"
	_ "github.com/jtarchie/ci/orchestra/docker"
	_ "github.com/jtarchie/ci/orchestra/native"
	. "github.com/onsi/gomega"
)

func TestExamplesDocker(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	matches, err := doublestar.FilepathGlob("docker/*.{js,ts,yml,yaml}")
	assert.Expect(err).NotTo(HaveOccurred())

	drivers := []string{"docker"}

	for _, match := range matches {
		examplePath, err := filepath.Abs(match)
		assert.Expect(err).NotTo(HaveOccurred())

		for _, driver := range drivers {
			t.Run(driver+": "+match, func(t *testing.T) {
				t.Parallel()

				assert := NewGomegaWithT(t)
				runner := commands.Runner{
					Pipeline:     examplePath,
					Orchestrator: driver,
				}
				err := runner.Run()
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
					Pipeline:     examplePath,
					Orchestrator: driver,
				}
				err := runner.Run()
				assert.Expect(err).NotTo(HaveOccurred())
			})
		}
	}
}
