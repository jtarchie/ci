package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
)

func TestExamples(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	path, err := gexec.Build("github.com/jtarchie/ci")
	assert.Expect(err).ToNot(HaveOccurred())

	matches, err := filepath.Glob("examples/*.[jt]s")
	assert.Expect(err).ToNot(HaveOccurred())

	drivers := []string{"docker", "native"}

	for _, match := range matches {
		examplePath, err := filepath.Abs(match)
		assert.Expect(err).ToNot(HaveOccurred())

		for _, driver := range drivers {
			t.Run(driver+": "+match, func(t *testing.T) {
				t.Parallel()

				assert := NewGomegaWithT(t)

				session, err := gexec.Start(
					exec.Command(
						path, "runtime",
						"--orchestrator", driver,
						examplePath,
					), os.Stderr, os.Stderr)
				assert.Expect(err).ToNot(HaveOccurred())
				assert.Eventually(session, "5s").Should(gexec.Exit(0))
			})
		}
	}
}
