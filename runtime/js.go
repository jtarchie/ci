package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/require"
	"github.com/evanw/esbuild/pkg/api"
	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/storage"
)

// ExecuteOptions configures pipeline execution.
type ExecuteOptions struct {
	// Resume enables resume mode for the pipeline.
	Resume bool
	// RunID is the unique identifier for this pipeline run.
	// If resuming, this should match the previous run's ID.
	RunID string
}

type JS struct {
	logger *slog.Logger
}

func NewJS(logger *slog.Logger) *JS {
	return &JS{
		logger: logger.WithGroup("js"),
	}
}

// Execute runs a pipeline with default options (no resume).
func (j *JS) Execute(ctx context.Context, source string, driver orchestra.Driver, storage storage.Driver) error {
	return j.ExecuteWithOptions(ctx, source, driver, storage, ExecuteOptions{})
}

// ExecuteWithOptions runs a pipeline with the given options.
func (j *JS) ExecuteWithOptions(ctx context.Context, source string, driver orchestra.Driver, storage storage.Driver, opts ExecuteOptions) error {
	var runner Runner

	if opts.Resume {
		resumableRunner, err := NewResumableRunner(ctx, driver, storage, j.logger, ResumeOptions{
			RunID:  opts.RunID,
			Resume: opts.Resume,
		})
		if err != nil {
			return fmt.Errorf("could not create resumable runner: %w", err)
		}
		runner = resumableRunner
	} else {
		runner = NewPipelineRunner(ctx, driver, j.logger)
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
		return &goja.CompilerSyntaxError{
			CompilerError: goja.CompilerError{
				Message: result.Errors[0].Text,
			},
		}
	}

	// split lines
	lines := strings.Split(strings.TrimSpace(string(result.Code)), "\n")

	if len(lines) == 0 {
		return fmt.Errorf("could not find source map: %w", errors.ErrUnsupported)
	}

	var sourceMap string

	sourceMap, lines = lines[len(lines)-1], lines[:len(lines)-1]
	finalSource := "{(function() { const module = {}; " + strings.Join(lines, "\n") +
		"; return module.exports.pipeline;}).apply(undefined)}\n" +
		sourceMap

	program, err := goja.Compile(
		"main.js",
		finalSource,
		true,
	)
	if err != nil {
		return fmt.Errorf("could not compile: %w", err)
	}

	// this is setup to build the pipeline in a goja jsVM
	jsVM := goja.New()
	jsVM.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))

	if timeout, ok := ctx.Deadline(); ok {
		// https://github.com/dop251/goja?tab=readme-ov-file#interrupting
		time.AfterFunc(time.Until(timeout), func() {
			jsVM.Interrupt("context deadline exceeded")
		})
	}

	registry := require.NewRegistry()
	registry.Enable(jsVM)
	registry.RegisterNativeModule("console", console.RequireWithPrinter(&printer{
		logger: j.logger.WithGroup("console"),
	}))

	_ = jsVM.Set("console", require.Require(jsVM, "console"))

	err = jsVM.Set("assert", NewAssert(jsVM, j.logger))
	if err != nil {
		return fmt.Errorf("could not set assert: %w", err)
	}

	err = jsVM.Set("YAML", NewYAML(jsVM, j.logger))
	if err != nil {
		return fmt.Errorf("could not set YAML: %w", err)
	}

	runtime := NewRuntime(jsVM, runner)

	err = jsVM.Set("runtime", runtime)
	if err != nil {
		return fmt.Errorf("could not set runtime: %w", err)
	}

	// Set up native resource runner
	resourceRunner := NewResourceRunner(ctx, j.logger)

	err = jsVM.Set("nativeResources", resourceRunner)
	if err != nil {
		return fmt.Errorf("could not set nativeResources: %w", err)
	}

	err = jsVM.Set("storage", storage)
	if err != nil {
		return fmt.Errorf("could not set storage: %w", err)
	}

	pipeline, err := jsVM.RunProgram(program)
	if err != nil {
		defer jsVM.ClearInterrupt()

		return fmt.Errorf("could not run program: %w", err)
	}

	// let's run the pipeline
	pipelineFunc, found := goja.AssertFunction(pipeline)
	if !found {
		return ErrPipelineNotFunction
	}

	value, err := pipelineFunc(goja.Undefined())
	if err != nil {
		return fmt.Errorf("could not run pipeline: %w", err)
	}

	if value == nil {
		return fmt.Errorf("pipeline returned nil: %w", ErrPipelineReturnedNonPromise)
	}

	promise, found := value.Export().(*goja.Promise)
	if !found {
		return fmt.Errorf("pipeline did not return a promise: %w", ErrPipelineNotFunction)
	}

	err = runtime.Wait()
	if err != nil {
		return fmt.Errorf("pipeline did not successfully execute: %w", err)
	}

	if promise.State() == goja.PromiseStateRejected {
		res := promise.Result()
		if resObj, ok := res.(*goja.Object); ok {
			if stack := resObj.Get("stack"); stack != nil {
				return fmt.Errorf("%w: %v\n%v", ErrPromiseRejected, res, stack)
			}
		}

		return fmt.Errorf("%w: %v", ErrPromiseRejected, res)
	}

	return nil
}

var (
	ErrPipelineNotFunction        = errors.New("pipeline is not a function")
	ErrPipelineReturnedNonPromise = errors.New("pipeline did not return a promise")
	ErrPromiseRejected            = errors.New("promise rejected")
)
