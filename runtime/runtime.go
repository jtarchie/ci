package runtime

import (
	"fmt"
	"sync"

	"github.com/dop251/goja"
	gonanoid "github.com/matoous/go-nanoid/v2"
)

type Runtime struct {
	jsVM     *goja.Runtime
	promises *sync.WaitGroup
	sandbox  *PipelineRunner
	tasks    chan func() error
}

func NewRuntime(
	jsVM *goja.Runtime,
	sandbox *PipelineRunner,
) *Runtime {
	promises := &sync.WaitGroup{}
	tasks := make(chan func() error, 1)

	return &Runtime{
		jsVM:     jsVM,
		promises: promises,
		sandbox:  sandbox,
		tasks:    tasks,
	}
}

func (r *Runtime) Run(input RunInput) *goja.Promise {
	promise, resolve, reject := r.jsVM.NewPromise()

	r.promises.Add(1)

	go func() {
		result, err := r.sandbox.Run(input)

		r.tasks <- func() error {
			defer r.promises.Done()

			if err != nil {
				err = reject(err)
				if err != nil {
					return fmt.Errorf("could not reject run: %w", err)
				}

				return nil
			}

			err := resolve(result)
			if err != nil {
				return fmt.Errorf("could not resolve run: %w", err)
			}

			return nil
		}
	}()

	return promise
}

func (r *Runtime) CreateVolume(input VolumeInput) *goja.Promise {
	if input.Name == "" {
		input.Name = gonanoid.Must()
	}

	promise, resolve, reject := r.jsVM.NewPromise()

	r.promises.Add(1)

	go func() {
		result, err := r.sandbox.CreateVolume(input)

		r.tasks <- func() error {
			defer r.promises.Done()

			if err != nil {
				err = reject(err)
				if err != nil {
					return fmt.Errorf("could not reject run: %w", err)
				}

				return nil
			}

			err := resolve(result)
			if err != nil {
				return fmt.Errorf("could not resolve create volume: %w", err)
			}

			return nil
		}
	}()

	return promise
}

func (r *Runtime) Wait() error {
	go func() {
		r.promises.Wait()
		close(r.tasks)
	}()

	for task := range r.tasks {
		err := task()
		if err != nil {
			return fmt.Errorf("could not wait: %w", err)
		}
	}

	return nil
}
