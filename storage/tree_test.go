package storage_test

import (
	"testing"

	"github.com/jtarchie/ci/storage"
	. "github.com/onsi/gomega"
)

func TestTree(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	path := storage.NewTree[string]()
	path.AddNode("a/b/c", "1")
	path.AddNode("a/b/d", "2")
	path.AddNode("a/e/f", "3")
	path.AddNode("g/h/i", "4")
	path.AddNode("g/h/j", "5")
	path.AddNode("a/b/d", "6")

	assert.Expect(path).To(Equal(&storage.Tree[string]{
		Name: "",
		Children: []*storage.Tree[string]{
			{
				Name: "a",
				Children: []*storage.Tree[string]{
					{
						Name: "b",
						Children: []*storage.Tree[string]{
							{Name: "c", Children: nil, Value: "1", FullPath: "/a/b/c"},
							{Name: "d", Children: nil, Value: "2", FullPath: "/a/b/d"},
						},
						FullPath: "/a/b",
					},
					{
						Name: "e",
						Children: []*storage.Tree[string]{
							{Name: "f", Children: nil, Value: "3", FullPath: "/a/e/f"},
						},
						FullPath: "/a/e",
					},
				},
				FullPath: "/a",
			},
			{
				Name: "g",
				Children: []*storage.Tree[string]{
					{
						Name: "h",
						Children: []*storage.Tree[string]{
							{Name: "i", Children: nil, Value: "4", FullPath: "/g/h/i"},
							{Name: "j", Children: nil, Value: "5", FullPath: "/g/h/j"},
						},
						FullPath: "/g/h",
					},
				},
				FullPath: "/g",
			},
			{
				Name: "a",
				Children: []*storage.Tree[string]{
					{
						Name: "b",
						Children: []*storage.Tree[string]{
							{Name: "d", Children: nil, Value: "6", FullPath: "/a/b/d"},
						},
						FullPath: "/a/b",
					},
				},
				FullPath: "/a",
			},
		},
	},
	))

	path.Flatten()

	assert.Expect(path).To(Equal(&storage.Tree[string]{
		Name: "",
		Children: []*storage.Tree[string]{
			{
				Name: "a",
				Children: []*storage.Tree[string]{
					{
						Name: "b",
						Children: []*storage.Tree[string]{
							{Name: "c", Children: nil, Value: "1", FullPath: "/a/b/c"},
							{Name: "d", Children: nil, Value: "2", FullPath: "/a/b/d"},
						},
						FullPath: "/a/b",
					},
					{Name: "e/f", Children: nil, Value: "3", FullPath: "/a/e/f"},
				},
				FullPath: "/a",
			},
			{
				Name: "g/h",
				Children: []*storage.Tree[string]{
					{Name: "i", Children: nil, Value: "4", FullPath: "/g/h/i"},
					{Name: "j", Children: nil, Value: "5", FullPath: "/g/h/j"},
				},
				FullPath: "/g/h",
			},
			{Name: "a/b/d", Children: nil, Value: "6", FullPath: "/a/b/d"},
		},
	},
	))
}
