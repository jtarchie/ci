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

type NativeVolume struct {
	path string
	name string
}

// Name implements orchestra.Volume.
func (n *NativeVolume) Name() string {
	return n.name
}

// Cleanup implements orchestra.Volume.
func (n *NativeVolume) Cleanup(ctx context.Context) error {
	return nil
}

var ErrInvalidPath = errors.New("path is not in the container directory")

func (n *Native) CreateVolume(ctx context.Context, name string, size int) (orchestra.Volume, error) {
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

	return &NativeVolume{
		name: name,
		path: path,
	}, nil
}
