package native

import (
	"fmt"
	"os"

	"github.com/jtarchie/ci/orchestra"
)

type Native struct {
	namespace string
	path      string
}

func NewNative(namespace string) (orchestra.Driver, error) {
	path, err := os.MkdirTemp("", namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	return &Native{
		namespace: namespace,
		path:      path,
	}, nil
}

func (n *Native) Name() string {
	return "native"
}

func init() {
	orchestra.Add("native", NewNative)
}

var (
	_ orchestra.Driver          = &Native{}
	_ orchestra.Container       = &NativeContainer{}
	_ orchestra.ContainerStatus = &NativeStatus{}
	_ orchestra.Volume          = &NativeVolume{}
)
