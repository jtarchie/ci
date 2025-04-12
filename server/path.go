package server

import "strings"

type Path[T any] struct {
	Name     string     `json:"name"`
	Children []*Path[T] `json:"children"`
	Value    T          `json:"value,omitempty"`
}

func NewPath[T any]() *Path[T] {
	return &Path[T]{}
}

func (p *Path[T]) AddChild(name string, value T) {
	parts := strings.Split(name, "/")
	current := p

	for _, part := range parts {
		var child *Path[T]

		for _, c := range current.Children {
			if c.Name == part {
				child = c

				break
			}
		}

		if child == nil {
			child = &Path[T]{Name: part}
			current.Children = append(current.Children, child)
		}

		current = child
	}

	current.Value = value
}
