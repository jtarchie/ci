package commands

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/jtarchie/ci/backwards"
)

type Transpile struct {
	Pipeline *os.File `arg:"" help:"Path to pipeline YAML file"`
}

// Run transpiles a pipeline YAML file into a pipeline.
// This is helpful for debugging and understanding the pipeline.
func (t *Transpile) Run(_ *slog.Logger) error {
	var err error

	pipeline, err := backwards.NewPipeline(t.Pipeline.Name())
	if err != nil {
		return fmt.Errorf("could not create pipeline from YAML: %w", err)
	}

	fmt.Fprintln(os.Stdout, pipeline)

	return nil
}
