package server

import (
	"testing"

	"github.com/jtarchie/pocketci/storage"
	. "github.com/onsi/gomega"
)

func TestCountTaskStatsCountsErrorAsFailure(t *testing.T) {
	t.Parallel()

	tree := &storage.Tree[storage.Payload]{
		Children: []*storage.Tree[storage.Payload]{
			{Name: "task-success", Value: storage.Payload{"status": "success"}},
			{Name: "task-failure", Value: storage.Payload{"status": "failure"}},
			{Name: "task-error", Value: storage.Payload{"status": "error"}},
			{Name: "task-pending", Value: storage.Payload{"status": "pending"}},
			{Name: "task-unknown", Value: storage.Payload{}},
		},
	}

	stats := countTaskStats(tree)
	assert := NewWithT(t)
	assert.Expect(stats.Success).To(Equal(1))
	assert.Expect(stats.Failure).To(Equal(2))
	assert.Expect(stats.Pending).To(Equal(2))
}
