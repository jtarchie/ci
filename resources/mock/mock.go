package mock

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/jtarchie/ci/resources"
)

// Mock implements a simple mock resource for testing.
// It maintains a version counter and creates files in the destination directory.
type Mock struct {
	versionCounter atomic.Int64
}

func (m *Mock) Name() string {
	return "mock"
}

// Check returns the current version based on force_version from source.
func (m *Mock) Check(_ context.Context, req resources.CheckRequest) (resources.CheckResponse, error) {
	forceVersion := ""
	if fv, ok := req.Source["force_version"].(string); ok {
		forceVersion = fv
	}

	if forceVersion == "" {
		forceVersion = fmt.Sprintf("%d", m.versionCounter.Add(1))
	}

	version := resources.Version{
		"version": forceVersion,
	}

	// If a version was provided, include it and the current version
	if req.Version != nil && req.Version["version"] != "" {
		return resources.CheckResponse{req.Version, version}, nil
	}

	return resources.CheckResponse{version}, nil
}

// In creates a version file in the destination directory.
func (m *Mock) In(_ context.Context, destDir string, req resources.InRequest) (resources.InResponse, error) {
	version := req.Version["version"]
	if version == "" {
		return resources.InResponse{}, fmt.Errorf("version is required")
	}

	// Create version file
	versionFile := filepath.Join(destDir, "version")

	err := os.WriteFile(versionFile, []byte(version), 0o600)
	if err != nil {
		return resources.InResponse{}, fmt.Errorf("failed to write version file: %w", err)
	}

	// Create privileged file if requested
	if _, ok := req.Params["privileged"]; ok {
		privilegedFile := filepath.Join(destDir, "privileged")

		err = os.WriteFile(privilegedFile, []byte("true"), 0o600)
		if err != nil {
			return resources.InResponse{}, fmt.Errorf("failed to write privileged file: %w", err)
		}
	}

	return resources.InResponse{
		Version: resources.Version{
			"version": version,
		},
		Metadata: resources.Metadata{
			{Name: "version", Value: version},
		},
	}, nil
}

// Out increments the version and returns it.
func (m *Mock) Out(_ context.Context, _ string, req resources.OutRequest) (resources.OutResponse, error) {
	version := ""
	if v, ok := req.Params["version"].(string); ok {
		version = v
	} else if v, ok := req.Params["version"].(float64); ok {
		version = fmt.Sprintf("%.0f", v)
	}

	if version == "" {
		version = fmt.Sprintf("%d", m.versionCounter.Add(1))
	}

	return resources.OutResponse{
		Version: resources.Version{
			"version": version,
		},
		Metadata: resources.Metadata{
			{Name: "version", Value: version},
		},
	}, nil
}

func init() {
	resources.Register("mock", func() resources.Resource {
		return &Mock{}
	})
}

var _ resources.Resource = &Mock{}
