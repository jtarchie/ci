package commands

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/google/uuid"
	"github.com/jtarchie/ci/backwards"
	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/runtime"
)

type Runner struct {
	Pipeline     string `arg:""           help:"Path to pipeline javascript file" type:"existingfile"`
	Orchestrator string `default:"native" help:"orchestrator runtime to use"`
}

func (c *Runner) Run() error {
	ctx := context.Background()

	var pipeline string

	extension := filepath.Ext(c.Pipeline)
	if extension == ".yml" || extension == ".yaml" {
		var err error

		pipeline, err = backwards.NewPipeline(c.Pipeline)
		if err != nil {
			return fmt.Errorf("could not create pipeline from YAML: %w", err)
		}
	} else {
		result := api.Build(api.BuildOptions{
			EntryPoints:      []string{c.Pipeline},
			Bundle:           true,
			Sourcemap:        api.SourceMapInline,
			Platform:         api.PlatformNeutral,
			PreserveSymlinks: true,
			AbsWorkingDir:    filepath.Dir(c.Pipeline),
		})
		if len(result.Errors) > 0 {
			return fmt.Errorf("%w: %s", ErrCouldNotBundle, result.Errors[0].Text)
		}

		pipeline = string(result.OutputFiles[0].Contents)
	}

	orchestrator, found := orchestra.Get(c.Orchestrator)
	if !found {
		return fmt.Errorf("could not get orchestrator (%q): %w", c.Orchestrator, ErrOrchestratorNotFound)
	}

	client, err := orchestrator("ci-" + uuid.New().String())
	if err != nil {
		return fmt.Errorf("could not create docker client: %w", err)
	}
	defer client.Close()

	js := runtime.NewJS(ctx)

	err = js.Execute(pipeline, runtime.NewPipelineRunner(client, ctx))
	if err != nil {
		return fmt.Errorf("could not execute pipeline: %w", err)
	}

	return nil
}

var (
	ErrCouldNotBundle       = errors.New("could not bundle pipeline")
	ErrOrchestratorNotFound = errors.New("orchestrator not found")
)
