package runtime

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/require"
	"github.com/evanw/esbuild/pkg/api"
)

type JS struct{}

func NewJS() *JS {
	return &JS{}
}

func (j *JS) Execute(source string, sandbox *PipelineRunner) error {
	result := api.Transform(source, api.TransformOptions{
		Loader:     api.LoaderTS,
		Format:     api.FormatCommonJS,
		Target:     api.ES2015,
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

	new(require.Registry).Enable(jsVM)
	console.Enable(jsVM)

	err = jsVM.Set("assert", NewAssert(jsVM))
	if err != nil {
		return fmt.Errorf("could not set assert: %w", err)
	}

	runtime := NewRuntime(jsVM, sandbox)

	err = jsVM.Set("runtime", runtime)
	if err != nil {
		return fmt.Errorf("could not set runtime: %w", err)
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

	value, err := pipelineFunc(goja.Undefined())
	if err != nil {
		return fmt.Errorf("could not run pipeline: %w", err)
	}

	promise, ok := value.Export().(*goja.Promise)
	if !ok {
		return fmt.Errorf("pipeline did not return a promise: %w", ErrPipelineNotFunction)
	}

	err = runtime.Wait()
	if err != nil {
		return fmt.Errorf("could not wait for promises: %w", err)
	}

	if promise.State() == goja.PromiseStateRejected {
		res := promise.Result()
		if resObj, ok := res.(*goja.Object); ok {
			if stack := resObj.Get("stack"); stack != nil {
				//nolint: err113
				return fmt.Errorf("pipeline promise rejected: %v\n%v", res, stack)
			}
		}

		//nolint: err113
		return fmt.Errorf("pipeline promise rejected: %v", res)
	}

	return nil
}

var ErrPipelineNotFunction = errors.New("pipeline is not a function")
