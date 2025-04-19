package commands

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/jtarchie/ci/backwards"
)

type Transpile struct {
	Pipeline *os.File `arg:"" help:"Path to pipeline YAML file"`
}

// Run transpiles a pipeline YAML file into a pipeline.
// This is helpful for debugging and understanding the pipeline.
func (t *Transpile) Run(_ *slog.Logger) error {
	var err error

	source, err := backwards.NewPipeline(t.Pipeline.Name())
	if err != nil {
		return fmt.Errorf("could not create pipeline from YAML: %w", err)
	}

	result := api.Transform(source, api.TransformOptions{
		Loader:     api.LoaderTS,
		Format:     api.FormatCommonJS,
		Target:     api.ES2017,
		Sourcemap:  api.SourceMapInline,
		Platform:   api.PlatformNeutral,
		Sourcefile: "main.js",
	})

	if len(result.Errors) > 0 {
		return fmt.Errorf("%s: %w", result.Errors[0].Text, ErrTranspile)
	}

	if len(result.Warnings) > 0 {
		return fmt.Errorf("%s: %w", result.Warnings[0].Text, ErrTranspile)
	}

	fmt.Fprintln(os.Stdout, string(result.Code))

	return nil
}

var ErrTranspile = errors.New("could not transpile pipeline")
