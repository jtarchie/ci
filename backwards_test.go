package main_test

import (
	"testing"

	"github.com/jtarchie/ci/commands"
	. "github.com/onsi/gomega"
)

func TestBackwardsCompatibility(t *testing.T) {
	t.Parallel()

	t.Run("on_failure", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline:     "examples/fixtures/on_failure.yml",
			Orchestrator: "native",
		}
		err := runner.Run()
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("failing-task failed with code 1"))
	})

	t.Run("on_success", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline:     "examples/fixtures/on_success.yml",
			Orchestrator: "native",
		}
		err := runner.Run()
		assert.Expect(err).ToNot(HaveOccurred())
	})

	t.Run("ensure", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline:     "examples/fixtures/ensure.yml",
			Orchestrator: "native",
		}
		err := runner.Run()
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("ensure-task failed with code 1"))
	})

	t.Run("do", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline:     "examples/fixtures/do.yml",
			Orchestrator: "native",
		}
		err := runner.Run()
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("ensure-task failed with code 11"))
	})

	t.Run("try", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline:     "examples/fixtures/try.yml",
			Orchestrator: "native",
		}
		err := runner.Run()
		assert.Expect(err).ToNot(HaveOccurred())
	})

	t.Run("all", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline:     "examples/fixtures/all.yml",
			Orchestrator: "native",
		}
		err := runner.Run()
		assert.Expect(err).ToNot(HaveOccurred())
	})

	t.Run("on_error", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)
		runner := commands.Runner{
			Pipeline:     "examples/fixtures/on_error.yml",
			Orchestrator: "native",
		}
		err := runner.Run()
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("Task erroring-task errored"))
	})
}
