package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jtarchie/ci/resources"
)

// ResourceRunner provides methods for executing native resources.
type ResourceRunner struct {
	ctx    context.Context //nolint: containedctx
	logger *slog.Logger
}

// NewResourceRunner creates a new ResourceRunner.
func NewResourceRunner(ctx context.Context, logger *slog.Logger) *ResourceRunner {
	return &ResourceRunner{
		ctx:    ctx,
		logger: logger.WithGroup("resource.runner"),
	}
}

// ResourceCheckInput is the input for a Check operation from JS.
type ResourceCheckInput struct {
	Type    string                 `json:"type"`
	Source  map[string]interface{} `json:"source"`
	Version map[string]string      `json:"version,omitempty"`
}

// ResourceCheckResult is the result of a Check operation.
type ResourceCheckResult struct {
	Versions []map[string]string `json:"versions"`
}

// Check discovers new versions of a resource.
func (r *ResourceRunner) Check(input ResourceCheckInput) (*ResourceCheckResult, error) {
	logger := r.logger.With("type", input.Type, "operation", "check")
	logger.Debug("resource.check")

	res, err := resources.Get(input.Type)
	if err != nil {
		return nil, fmt.Errorf("resource type not found: %w", err)
	}

	req := resources.CheckRequest{
		Source:  input.Source,
		Version: input.Version,
	}

	resp, err := res.Check(r.ctx, req)
	if err != nil {
		logger.Error("resource.check", "err", err)

		return nil, fmt.Errorf("check failed: %w", err)
	}

	versions := make([]map[string]string, len(resp))
	for i, v := range resp {
		versions[i] = v
	}

	return &ResourceCheckResult{Versions: versions}, nil
}

// ResourceInInput is the input for an In operation from JS.
type ResourceInInput struct {
	Type    string                 `json:"type"`
	Source  map[string]interface{} `json:"source"`
	Version map[string]string      `json:"version"`
	Params  map[string]interface{} `json:"params,omitempty"`
	DestDir string                 `json:"destDir"`
}

// ResourceInResult is the result of an In operation.
type ResourceInResult struct {
	Version  map[string]string `json:"version"`
	Metadata []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"metadata"`
}

// In fetches a specific version of a resource.
func (r *ResourceRunner) In(input ResourceInInput) (*ResourceInResult, error) {
	logger := r.logger.With("type", input.Type, "operation", "in", "destDir", input.DestDir)
	logger.Debug("resource.in")

	res, err := resources.Get(input.Type)
	if err != nil {
		return nil, fmt.Errorf("resource type not found: %w", err)
	}

	req := resources.InRequest{
		Source:  input.Source,
		Version: input.Version,
		Params:  input.Params,
	}

	resp, err := res.In(r.ctx, input.DestDir, req)
	if err != nil {
		logger.Error("resource.in", "err", err)

		return nil, fmt.Errorf("in failed: %w", err)
	}

	result := &ResourceInResult{
		Version: resp.Version,
	}

	for _, m := range resp.Metadata {
		result.Metadata = append(result.Metadata, struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}{Name: m.Name, Value: m.Value})
	}

	return result, nil
}

// ResourceOutInput is the input for an Out operation from JS.
type ResourceOutInput struct {
	Type   string                 `json:"type"`
	Source map[string]interface{} `json:"source"`
	Params map[string]interface{} `json:"params,omitempty"`
	SrcDir string                 `json:"srcDir"`
}

// ResourceOutResult is the result of an Out operation.
type ResourceOutResult struct {
	Version  map[string]string `json:"version"`
	Metadata []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"metadata"`
}

// Out pushes a new version of a resource.
func (r *ResourceRunner) Out(input ResourceOutInput) (*ResourceOutResult, error) {
	logger := r.logger.With("type", input.Type, "operation", "out", "srcDir", input.SrcDir)
	logger.Debug("resource.out")

	res, err := resources.Get(input.Type)
	if err != nil {
		return nil, fmt.Errorf("resource type not found: %w", err)
	}

	req := resources.OutRequest{
		Source: input.Source,
		Params: input.Params,
	}

	resp, err := res.Out(r.ctx, input.SrcDir, req)
	if err != nil {
		logger.Error("resource.out", "err", err)

		return nil, fmt.Errorf("out failed: %w", err)
	}

	result := &ResourceOutResult{
		Version: resp.Version,
	}

	for _, m := range resp.Metadata {
		result.Metadata = append(result.Metadata, struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}{Name: m.Name, Value: m.Value})
	}

	return result, nil
}

// IsNative returns true if the given resource type is a native resource.
func (r *ResourceRunner) IsNative(resourceType string) bool {
	return resources.IsNative(resourceType)
}

// ListNativeResources returns a list of all registered native resource types.
func (r *ResourceRunner) ListNativeResources() []string {
	return resources.List()
}

// NativeResourceInfo holds information about resource execution for JSON serialization.
type NativeResourceInfo struct {
	Request  json.RawMessage `json:"request"`
	Response json.RawMessage `json:"response"`
}
