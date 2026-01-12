package native

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jtarchie/ci/orchestra"
)

type Volume struct {
	path string
	name string
}

// Name implements orchestra.Volume.
func (n *Volume) Name() string {
	return n.name
}

// Path implements orchestra.Volume.
func (n *Volume) Path() string {
	return n.path
}

// Cleanup implements orchestra.Volume.
func (n *Volume) Cleanup(_ context.Context) error {
	return nil
}

var ErrInvalidPath = errors.New("path is not in the container directory")

func (n *Native) CreateVolume(_ context.Context, name string, _ int) (orchestra.Volume, error) {
	path, err := filepath.Abs(filepath.Join(n.path, name))
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	if !strings.HasPrefix(path, n.path) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidPath, path)
	}

	err = os.MkdirAll(path, os.ModePerm)
	if err != nil {
		return nil, fmt.Errorf("failed to create path: %w", err)
	}

	return &Volume{
		name: name,
		path: path,
	}, nil
}
