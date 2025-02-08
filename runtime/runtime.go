package runtime

import (
	"fmt"

	"github.com/dop251/goja"
	"github.com/google/uuid"
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
	promises := &errgroup.Group{}
	// promises.SetLimit(1)

	return &Runtime{
		promises: promises,
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

func (r *Runtime) CreateVolume(input VolumeInput) *goja.Promise {
	if input.Name == "" {
		input.Name = uuid.New().String()
	}

	promise, resolve, _ := r.jsVM.NewPromise()

	r.promises.Go(func() error {
		result := r.sandbox.CreateVolume(input)

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
