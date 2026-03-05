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

// Run executes a container task. Accepts an object with optional onOutput callback.
func (r *Runtime) Run(call goja.FunctionCall) goja.Value {
	promise, resolve, reject := r.jsVM.NewPromise()

	// Extract input from the first argument
	if len(call.Arguments) == 0 {
		_ = reject(r.jsVM.NewGoError(fmt.Errorf("run requires an input object")))
		return r.jsVM.ToValue(promise)
	}

	inputObj := call.Arguments[0].ToObject(r.jsVM)

	// Parse the input struct (goja will map fields by json tags)
	var input RunInput
	if err := r.jsVM.ExportTo(inputObj, &input); err != nil {
		_ = reject(r.jsVM.NewGoError(fmt.Errorf("invalid input: %w", err)))
		return r.jsVM.ToValue(promise)
	}

	// Check for onOutput callback
	onOutputVal := inputObj.Get("onOutput")
	var onOutputFunc goja.Callable
	if onOutputVal != nil && !goja.IsUndefined(onOutputVal) && !goja.IsNull(onOutputVal) {
		var ok bool
		onOutputFunc, ok = goja.AssertFunction(onOutputVal)
		if !ok {
			_ = reject(r.jsVM.NewGoError(fmt.Errorf("onOutput must be a function")))
			return r.jsVM.ToValue(promise)
		}
	}

	// If callback provided, wrap it to safely invoke through the tasks channel
	if onOutputFunc != nil {
		input.OnOutput = func(stream string, data string) {
			// Queue the callback invocation on the main JS thread via tasks channel
			r.tasks <- func() error {
				_, err := onOutputFunc(goja.Undefined(), r.jsVM.ToValue(stream), r.jsVM.ToValue(data))
				if err != nil {
					// Log but don't fail - callbacks shouldn't break the task
					return nil
				}
				return nil
			}
		}
	}

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

	return r.jsVM.ToValue(promise)
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

// StartSandbox starts a long-lived sandbox container and resolves with a JS object
// exposing exec(config) and close() methods.
func (r *Runtime) StartSandbox(call goja.FunctionCall) goja.Value {
	promise, resolve, reject := r.jsVM.NewPromise()

	if len(call.Arguments) == 0 {
		_ = reject(r.jsVM.NewGoError(fmt.Errorf("startSandbox requires an input object")))
		return r.jsVM.ToValue(promise)
	}

	inputObj := call.Arguments[0].ToObject(r.jsVM)

	var input SandboxInput
	if err := r.jsVM.ExportTo(inputObj, &input); err != nil {
		_ = reject(r.jsVM.NewGoError(fmt.Errorf("invalid startSandbox input: %w", err)))
		return r.jsVM.ToValue(promise)
	}

	r.promises.Add(1)

	go func() {
		handle, err := r.runner.StartSandbox(input)

		r.tasks <- func() error {
			defer r.promises.Done()

			if err != nil {
				err = reject(err)
				if err != nil {
					return fmt.Errorf("could not reject startSandbox: %w", err)
				}

				return nil
			}

			// Build the sandbox JS object with exec and close methods.
			sandboxObj := r.jsVM.NewObject()
			_ = sandboxObj.Set("id", handle.ID())

			_ = sandboxObj.Set("exec", func(call goja.FunctionCall) goja.Value {
				execPromise, execResolve, execReject := r.jsVM.NewPromise()

				if len(call.Arguments) == 0 {
					_ = execReject(r.jsVM.NewGoError(fmt.Errorf("exec requires an input object")))
					return r.jsVM.ToValue(execPromise)
				}

				execInputObj := call.Arguments[0].ToObject(r.jsVM)

				var execInput ExecInput
				if err := r.jsVM.ExportTo(execInputObj, &execInput); err != nil {
					_ = execReject(r.jsVM.NewGoError(fmt.Errorf("invalid exec input: %w", err)))
					return r.jsVM.ToValue(execPromise)
				}

				// Check for onOutput callback.
				onOutputVal := execInputObj.Get("onOutput")
				if onOutputVal != nil && !goja.IsUndefined(onOutputVal) && !goja.IsNull(onOutputVal) {
					if onOutputFunc, ok := goja.AssertFunction(onOutputVal); ok {
						execInput.OnOutput = func(stream, data string) {
							r.tasks <- func() error {
								_, err := onOutputFunc(goja.Undefined(), r.jsVM.ToValue(stream), r.jsVM.ToValue(data))
								if err != nil {
									return nil
								}

								return nil
							}
						}
					}
				}

				r.promises.Add(1)

				go func() {
					result, err := handle.Exec(execInput)

					r.tasks <- func() error {
						defer r.promises.Done()

						if err != nil {
							err = execReject(err)
							if err != nil {
								return fmt.Errorf("could not reject exec: %w", err)
							}

							return nil
						}

						err = execResolve(result)
						if err != nil {
							return fmt.Errorf("could not resolve exec: %w", err)
						}

						return nil
					}
				}()

				return r.jsVM.ToValue(execPromise)
			})

			_ = sandboxObj.Set("close", func(call goja.FunctionCall) goja.Value {
				closePromise, closeResolve, closeReject := r.jsVM.NewPromise()

				r.promises.Add(1)

				go func() {
					err := handle.Close()

					r.tasks <- func() error {
						defer r.promises.Done()

						if err != nil {
							err = closeReject(err)
							if err != nil {
								return fmt.Errorf("could not reject close: %w", err)
							}

							return nil
						}

						err = closeResolve(goja.Undefined())
						if err != nil {
							return fmt.Errorf("could not resolve close: %w", err)
						}

						return nil
					}
				}()

				return r.jsVM.ToValue(closePromise)
			})

			err = resolve(sandboxObj)
			if err != nil {
				return fmt.Errorf("could not resolve startSandbox: %w", err)
			}

			return nil
		}
	}()

	return r.jsVM.ToValue(promise)
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
