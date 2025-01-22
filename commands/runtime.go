package commands

import (
	"fmt"
	"io"
	"os"

	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/runtime"
)

type Runtime struct {
	Pipeline     *os.File `arg:"" help:"Path to pipeline javascript file"`
	Orchestrator string   `help:"orchestrator runtime to use" default:"native"`
}

func (c *Runtime) Run() error {
	contents, err := io.ReadAll(c.Pipeline)
	if err != nil {
		return fmt.Errorf("failed to read pipeline file: %w", err)
	}

	orchestrator, found := orchestra.Get(c.Orchestrator)
	if !found {
		return fmt.Errorf("could not get orchestrator: %s", c.Orchestrator)
	}

	client, err := orchestrator("ci")
	if err != nil {
		return fmt.Errorf("could not create docker client: %w", err)
	}

	js := runtime.NewJS()
	err = js.Execute(string(contents), runtime.NewSandbox(client))
	if err != nil {
		return fmt.Errorf("could not execute pipeline: %w", err)
	}

	return nil
}
