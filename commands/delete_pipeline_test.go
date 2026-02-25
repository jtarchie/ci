package commands_test

import (
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/jtarchie/ci/commands"
	"github.com/jtarchie/ci/storage"
	. "github.com/onsi/gomega"
)

func TestDeletePipeline(t *testing.T) {
	t.Parallel()

	t.Run("deletes a pipeline by name", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		ps := newPipelineServer(
			storage.Pipeline{ID: "abc-123", Name: "my-pipeline"},
		)
		server := httptest.NewServer(ps)
		defer server.Close()

		cmd := commands.DeletePipeline{
			Name:      "my-pipeline",
			ServerURL: server.URL,
		}

		err := cmd.Run(slog.Default())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ps.pipelines).To(BeEmpty())
	})

	t.Run("deletes a pipeline by ID", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		ps := newPipelineServer(
			storage.Pipeline{ID: "abc-123", Name: "my-pipeline"},
		)
		server := httptest.NewServer(ps)
		defer server.Close()

		cmd := commands.DeletePipeline{
			Name:      "abc-123",
			ServerURL: server.URL,
		}

		err := cmd.Run(slog.Default())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ps.pipelines).To(BeEmpty())
	})

	t.Run("returns error when pipeline not found", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		ps := newPipelineServer()
		server := httptest.NewServer(ps)
		defer server.Close()

		cmd := commands.DeletePipeline{
			Name:      "non-existent",
			ServerURL: server.URL,
		}

		err := cmd.Run(slog.Default())
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("no pipeline found"))
	})

	t.Run("deletes all pipelines matching a name", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		ps := newPipelineServer(
			storage.Pipeline{ID: "id-1", Name: "duplicate"},
			storage.Pipeline{ID: "id-2", Name: "duplicate"},
			storage.Pipeline{ID: "id-3", Name: "keep-me"},
		)
		server := httptest.NewServer(ps)
		defer server.Close()

		cmd := commands.DeletePipeline{
			Name:      "duplicate",
			ServerURL: server.URL,
		}

		err := cmd.Run(slog.Default())
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(ps.pipelines).To(HaveLen(1))
		assert.Expect(ps.pipelines[0].Name).To(Equal("keep-me"))
	})
}
