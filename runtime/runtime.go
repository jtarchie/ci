package runtime

import (
	"fmt"

	"github.com/dop251/goja"
	"golang.org/x/sync/errgroup"
)

type Runtime struct {
	promises *errgroup.Group
	sandbox  *PipelineRunner
	jsVM     *goja.Runtime
}

func NewRuntime(
	jsVM *goja.Runtime,
	sandbox *PipelineRunner,
) *Runtime {
	return &Runtime{
		promises: &errgroup.Group{},
		sandbox:  sandbox,
		jsVM:     jsVM,
	}
}

func (r *Runtime) Run(input RunInput) *goja.Promise {
	promise, resolve, _ := r.jsVM.NewPromise()

	r.promises.Go(func() error {
		result := r.sandbox.Run(input)

		err := resolve(result)
		if err != nil {
			return fmt.Errorf("could not resolve: %w", err)
		}

		return nil
	})

	return promise
}

func (r *Runtime) Wait() error {
	err := r.promises.Wait()
	if err != nil {
		return fmt.Errorf("could not wait: %w", err)
	}

	return nil
}
