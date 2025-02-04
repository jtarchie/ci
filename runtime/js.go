package runtime

import (
	"errors"
	"fmt"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/require"
	"github.com/evanw/esbuild/pkg/api"
	"golang.org/x/sync/errgroup"
)

type JS struct{}

func NewJS() *JS {
	return &JS{}
}

func (j *JS) Execute(source string, sandbox *PipelineRunner) error {
	result := api.Transform(source, api.TransformOptions{
		Loader:    api.LoaderTS,
		Format:    api.FormatCommonJS,
		Target:    api.ES2015,
		Sourcemap: api.SourceMapInline,
		Platform:  api.PlatformNeutral,
	})

	if len(result.Errors) > 0 {
		return &goja.CompilerSyntaxError{
			CompilerError: goja.CompilerError{
				Message: result.Errors[0].Text,
			},
		}
	}

	program, err := goja.Compile(
		"main.js",
		"{(function() { const module = {}; "+string(result.Code)+"; return module.exports.pipeline;}).apply(undefined)}",
		true,
	)
	if err != nil {
		return fmt.Errorf("could not compile: %w", err)
	}

	// this is setup to build the pipeline in a goja jsVM
	jsVM := goja.New()
	jsVM.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))

	new(require.Registry).Enable(jsVM)
	console.Enable(jsVM)

	err = jsVM.Set("assert", NewAssert(jsVM))
	if err != nil {
		return fmt.Errorf("could not set assert: %w", err)
	}

	promises := errgroup.Group{}
	promises.SetLimit(1)

	err = jsVM.Set("run", func(input RunInput) *goja.Promise {
		promise, resolve, reject := jsVM.NewPromise()

		promises.Go(func() error {
			result := sandbox.Run(input)
			if err != nil {
				err = reject(result)
				if err != nil {
					return fmt.Errorf("could not reject: %w", err)
				}
			} else {
				err = resolve(result)
				if err != nil {
					return fmt.Errorf("could not resolve: %w", err)
				}
			}

			return nil
		})

		return promise
	})
	if err != nil {
		return fmt.Errorf("could not set run: %w", err)
	}

	pipeline, err := jsVM.RunProgram(program)
	if err != nil {
		defer jsVM.ClearInterrupt()

		return fmt.Errorf("could not run program: %w", err)
	}

	// let's run the pipeline
	pipelineFunc, ok := goja.AssertFunction(pipeline)
	if !ok {
		return ErrPipelineNotFunction
	}

	_, err = pipelineFunc(goja.Undefined())
	if err != nil {
		return fmt.Errorf("could not run pipeline: %w", err)
	}

	err = promises.Wait()
	if err != nil {
		return fmt.Errorf("could not wait for promises: %w", err)
	}

	return nil
}

var ErrPipelineNotFunction = errors.New("pipeline is not a function")
