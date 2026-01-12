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
	// PipelineID is the unique identifier for this pipeline.
	// Used to scope resource versions to a specific pipeline.
	PipelineID string
}

type JS struct {
	logger *slog.Logger
}

func NewJS(logger *slog.Logger) *JS {
	return &JS{
		logger: logger.WithGroup("js"),
	}
}

// TranspileAndValidate transpiles TypeScript/JavaScript source code to executable JavaScript.
// It performs esbuild transpilation, wraps the code for module exports, and validates
// the result can be compiled by goja. Returns the ready-to-execute code or an error.
func TranspileAndValidate(source string) (string, error) {
	result := api.Transform(source, api.TransformOptions{
		Loader:     api.LoaderTS,
		Format:     api.FormatCommonJS,
		Target:     api.ES2017,
		Sourcemap:  api.SourceMapInline,
		Platform:   api.PlatformNeutral,
		Sourcefile: "main.js",
	})

	if len(result.Errors) > 0 {
		return "", fmt.Errorf("syntax error: %s", result.Errors[0].Text)
	}

	lines := strings.Split(strings.TrimSpace(string(result.Code)), "\n")
	if len(lines) == 0 {
		return "", fmt.Errorf("empty pipeline after transpilation: %w", errors.ErrUnsupported)
	}

	var sourceMap string
	sourceMap, lines = lines[len(lines)-1], lines[:len(lines)-1]
	finalSource := "{(function() { const module = {}; " + strings.Join(lines, "\n") +
		"; return module.exports.pipeline;}).apply(undefined)}\n" +
		sourceMap

	_, err := goja.Compile("main.js", finalSource, true)
	if err != nil {
		return "", fmt.Errorf("compilation error: %w", err)
	}

	return finalSource, nil
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

	finalSource, err := TranspileAndValidate(source)
	if err != nil {
		return err
	}

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

	// Set up notification runtime
	notifier := NewNotifier(j.logger)
	notifyRuntime := NewNotifyRuntime(ctx, jsVM, notifier, runtime.promises, runtime.tasks)

	err = jsVM.Set("notify", notifyRuntime)
	if err != nil {
		return fmt.Errorf("could not set notify: %w", err)
	}

	// Set up native resource runner
	resourceRunner := NewResourceRunner(ctx, j.logger)

	err = jsVM.Set("nativeResources", resourceRunner)
	if err != nil {
		return fmt.Errorf("could not set nativeResources: %w", err)
	}

	// Wrap storage to inject context automatically for JavaScript calls
	storageWrapper := &storageContextWrapper{
		driver: storage,
		ctx:    ctx,
	}
	err = jsVM.Set("storage", storageWrapper)
	if err != nil {
		return fmt.Errorf("could not set storage: %w", err)
	}

	// Expose pipeline context to JavaScript (runID, pipelineID, etc.)
	pipelineContext := map[string]interface{}{
		"runID":      opts.RunID,
		"pipelineID": opts.PipelineID,
	}
	err = jsVM.Set("pipelineContext", pipelineContext)
	if err != nil {
		return fmt.Errorf("could not set pipelineContext: %w", err)
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

// storageContextWrapper wraps a storage.Driver to automatically inject context
// for JavaScript calls that don't pass context explicitly.
type storageContextWrapper struct {
	driver storage.Driver
	ctx    context.Context
}

// Set wraps the storage Set method, injecting context automatically.
func (w *storageContextWrapper) Set(prefix string, payload any) error {
	return w.driver.Set(w.ctx, prefix, payload)
}

// Get wraps the storage Get method, injecting context automatically.
func (w *storageContextWrapper) Get(prefix string) (storage.Payload, error) {
	return w.driver.Get(w.ctx, prefix)
}

// GetAll wraps the storage GetAll method, injecting context automatically.
func (w *storageContextWrapper) GetAll(prefix string, fields []string) (storage.Results, error) {
	return w.driver.GetAll(w.ctx, prefix, fields)
}

// SavePipeline wraps the storage SavePipeline method, injecting context automatically.
func (w *storageContextWrapper) SavePipeline(name, content, driverDSN string) (*storage.Pipeline, error) {
	return w.driver.SavePipeline(w.ctx, name, content, driverDSN)
}

// GetPipeline wraps the storage GetPipeline method, injecting context automatically.
func (w *storageContextWrapper) GetPipeline(id string) (*storage.Pipeline, error) {
	return w.driver.GetPipeline(w.ctx, id)
}

// ListPipelines wraps the storage ListPipelines method, injecting context automatically.
func (w *storageContextWrapper) ListPipelines() ([]storage.Pipeline, error) {
	return w.driver.ListPipelines(w.ctx)
}

// DeletePipeline wraps the storage DeletePipeline method, injecting context automatically.
func (w *storageContextWrapper) DeletePipeline(id string) error {
	return w.driver.DeletePipeline(w.ctx, id)
}

// Close wraps the storage Close method (no context needed).
func (w *storageContextWrapper) Close() error {
	return w.driver.Close()
}

// SaveResourceVersion wraps the storage SaveResourceVersion method.
func (w *storageContextWrapper) SaveResourceVersion(resourceName string, version map[string]string, jobName string) (*storage.ResourceVersion, error) {
	return w.driver.SaveResourceVersion(w.ctx, resourceName, version, jobName)
}

// GetLatestResourceVersion wraps the storage GetLatestResourceVersion method.
func (w *storageContextWrapper) GetLatestResourceVersion(resourceName string) (*storage.ResourceVersion, error) {
	return w.driver.GetLatestResourceVersion(w.ctx, resourceName)
}

// ListResourceVersions wraps the storage ListResourceVersions method.
func (w *storageContextWrapper) ListResourceVersions(resourceName string, limit int) ([]storage.ResourceVersion, error) {
	return w.driver.ListResourceVersions(w.ctx, resourceName, limit)
}

// GetVersionsAfter wraps the storage GetVersionsAfter method.
func (w *storageContextWrapper) GetVersionsAfter(resourceName string, afterVersion map[string]string) ([]storage.ResourceVersion, error) {
	return w.driver.GetVersionsAfter(w.ctx, resourceName, afterVersion)
}
