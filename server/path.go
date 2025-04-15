package server

import (
	"path/filepath"
	"strings"
)

type Path[T any] struct {
	Name     string     `json:"name"`
	Children []*Path[T] `json:"children"`
	Value    T          `json:"value,omitempty"`

	FullPath string `json:"-"`
}

func NewPath[T any]() *Path[T] {
	return &Path[T]{}
}

func (p *Path[T]) AddChild(name string, value T) {
	parts := strings.Split(filepath.Clean(name), string(filepath.Separator))

	current := p

	for index, part := range parts {
		var child *Path[T]

		if len(current.Children) > 0 && current.Children[len(current.Children)-1].Name == part {
			child = current.Children[len(current.Children)-1]
		}

		if child == nil {
			child = &Path[T]{
				Name:     part,
				FullPath: "/" + filepath.Join(parts[:index+1]...),
			}
			current.Children = append(current.Children, child)
		}

		current = child
	}

	current.Value = value
}

func (p *Path[T]) Flatten() {
	for _, child := range p.Children {
		child.Flatten()
	}

	// Then check if this node has a single child
	if p.HasSingleChild() {
		child := p.Children[0]
		p.Name = filepath.Join(p.Name, child.Name)
		p.Value = child.Value
		p.Children = child.Children
		p.FullPath = child.FullPath
		p.Flatten()
	}
}

// IsLeaf determines if this path is a leaf node (has no children).
func (p *Path[T]) IsLeaf() bool {
	return len(p.Children) == 0
}

// IsGroup determines if this path is a group (has children).
func (p *Path[T]) IsGroup() bool {
	return len(p.Children) > 0
}

// HasSingleChild checks if this path has exactly one child.
func (p *Path[T]) HasSingleChild() bool {
	return len(p.Children) == 1
}
