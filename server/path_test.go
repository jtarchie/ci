package server_test

import (
	"testing"

	"github.com/jtarchie/ci/server"
	. "github.com/onsi/gomega"
)

func TestPath(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	path := server.NewPath[string]()
	path.AddChild("a/b/c", "1")
	path.AddChild("a/b/d", "2")
	path.AddChild("a/e/f", "3")
	path.AddChild("g/h/i", "4")
	path.AddChild("g/h/j", "5")
	path.AddChild("a/b/d", "6")

	assert.Expect(path).To(Equal(&server.Path[string]{
		Name: "",
		Children: []*server.Path[string]{
			{
				Name: "a",
				Children: []*server.Path[string]{
					{
						Name: "b",
						Children: []*server.Path[string]{
							{Name: "c", Children: nil, Value: "1"},
							{Name: "d", Children: nil, Value: "2"},
						},
					},
					{
						Name: "e",
						Children: []*server.Path[string]{
							{Name: "f", Children: nil, Value: "3"},
						},
					},
				},
			},
			{
				Name: "g",
				Children: []*server.Path[string]{
					{
						Name: "h",
						Children: []*server.Path[string]{
							{Name: "i", Children: nil, Value: "4"},
							{Name: "j", Children: nil, Value: "5"},
						},
					},
				},
			},
			{
				Name: "a",
				Children: []*server.Path[string]{
					{
						Name: "b",
						Children: []*server.Path[string]{
							{Name: "d", Children: nil, Value: "6"},
						},
					},
				},
			},
		},
	},
	))
}
