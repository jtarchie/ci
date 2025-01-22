package runtime

import (
	"fmt"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/require"
	"github.com/evanw/esbuild/pkg/api"
)

type JS struct{}

func NewJS() *JS {
	return &JS{}
}

func (j *JS) Execute(source string, sandbox *Sandbox) error {
	// this is setup to build the pipeline in a goja vm
	vm := goja.New()
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))

	new(require.Registry).Enable(vm)
	console.Enable(vm)

	err := vm.Set("run", sandbox.Run)
	if err != nil {
		return fmt.Errorf("could not set run: %w", err)
	}

	result := api.Transform(source, api.TransformOptions{
		Loader:    api.LoaderTS,
		Format:    api.FormatCommonJS,
		Target:    api.ES2015,
		Sourcemap: api.SourceMapNone,
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

	pipeline, err := vm.RunProgram(program)
	if err != nil {
		defer vm.ClearInterrupt()

		return fmt.Errorf("could not run program: %w", err)
	}

	// let's run the pipeline
	pipelineFunc, ok := goja.AssertFunction(pipeline)
	if !ok {
		return fmt.Errorf("pipeline is not a function")
	}

	_, err = pipelineFunc(goja.Undefined())
	if err != nil {
		return fmt.Errorf("could not run pipeline: %w", err)
	}

	return nil
}
