package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/runtime"
)

type Runtime struct {
	Pipeline     *os.File `arg:"" help:"Path to pipeline javascript file"`
	Orchestrator string   `help:"orchestrator runtime to use" default:"native"`
}

func (c *Runtime) Run() error {
	result := api.Build(api.BuildOptions{
		EntryPoints:      []string{c.Pipeline.Name()},
		Bundle:           true,
		Sourcemap:        api.SourceMapInline,
		Platform:         api.PlatformNeutral,
		PreserveSymlinks: true,
		AbsWorkingDir:    filepath.Dir(c.Pipeline.Name()),
	})
	if len(result.Errors) > 0 {
		return fmt.Errorf("could not bundle pipeline: %s", result.Errors[0].Text)
	}

	contents := result.OutputFiles[0].Contents

	orchestrator, found := orchestra.Get(c.Orchestrator)
	if !found {
		return fmt.Errorf("could not get orchestrator: %s", c.Orchestrator)
	}

	client, err := orchestrator("ci")
	if err != nil {
		return fmt.Errorf("could not create docker client: %w", err)
	}

	js := runtime.NewJS()
	err = js.Execute(string(contents), runtime.NewPipelineRunner(client))
	if err != nil {
		return fmt.Errorf("could not execute pipeline: %w", err)
	}

	return nil
}
