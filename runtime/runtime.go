package runtime

import (
	"fmt"
	"sync"

	"github.com/dop251/goja"
)

type Runtime struct {
	jsVM        *goja.Runtime
	promises    *sync.WaitGroup
	runner      Runner
	tasks       chan func() error
	namespace   string
	runID       string
	mu          sync.Mutex // Protects volumeIndex
	volumeIndex int        // Counter for unnamed volumes
}

func NewRuntime(
	jsVM *goja.Runtime,
	runner Runner,
	namespace string,
	runID string,
) *Runtime {
	promises := &sync.WaitGroup{}
	tasks := make(chan func() error, 1)

	return &Runtime{
		jsVM:      jsVM,
		promises:  promises,
		runner:    runner,
		tasks:     tasks,
		namespace: namespace,
		runID:     runID,
	}
}

func (r *Runtime) Run(input RunInput) *goja.Promise {
	promise, resolve, reject := r.jsVM.NewPromise()

	r.promises.Add(1)

	go func() {
		result, err := r.runner.Run(input)

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
		// Generate deterministic volume name using counter
		r.mu.Lock()
		volumeID := fmt.Sprintf("vol-%d", r.volumeIndex)
		r.volumeIndex++
		r.mu.Unlock()
		input.Name = DeterministicVolumeID(r.namespace, fmt.Sprintf("%s-%s", r.runID, volumeID))
	}

	promise, resolve, reject := r.jsVM.NewPromise()

	r.promises.Add(1)

	go func() {
		result, err := r.runner.CreateVolume(input)

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
