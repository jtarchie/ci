package resources

import "context"

// Version represents a resource version as arbitrary key-value pairs.
// For git, this would be {"ref": "abc123"}, for time {"time": "2024-01-01T00:00:00Z"}.
type Version map[string]string

// MetadataField represents a single piece of metadata about a resource.
type MetadataField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Metadata is a list of key-value pairs providing additional information
// about a resource version. This is displayed in the UI.
type Metadata []MetadataField

// CheckRequest is the input to a Check operation.
// Source contains the resource configuration, Version is the last known version (may be nil).
type CheckRequest struct {
	Source  map[string]any `json:"source"`
	Version Version        `json:"version,omitempty"`
}

// CheckResponse is the output of a Check operation.
// Returns an ordered list of new versions (oldest first), including the requested version if still valid.
type CheckResponse []Version

// InRequest is the input to an In (get) operation.
type InRequest struct {
	Source  map[string]any `json:"source"`
	Version Version        `json:"version"`
	Params  map[string]any `json:"params,omitempty"`
}

// InResponse is the output of an In operation.
type InResponse struct {
	Version  Version  `json:"version"`
	Metadata Metadata `json:"metadata,omitempty"`
}

// OutRequest is the input to an Out (put) operation.
type OutRequest struct {
	Source map[string]any `json:"source"`
	Params map[string]any `json:"params,omitempty"`
}

// OutResponse is the output of an Out operation.
type OutResponse struct {
	Version  Version  `json:"version"`
	Metadata Metadata `json:"metadata,omitempty"`
}

// Resource is the interface that all native resources must implement.
// It follows the Concourse resource protocol: check discovers versions,
// in fetches a version, and out publishes a new version.
type Resource interface {
	// Name returns the resource type name (e.g., "git", "s3", "time").
	Name() string

	// Check discovers new versions of the resource.
	// If Version is nil, return only the latest version.
	// Otherwise, return all versions newer than the given version (including it if still valid).
	Check(ctx context.Context, req CheckRequest) (CheckResponse, error)

	// In fetches a specific version of the resource to the destination directory.
	// destDir is an absolute path to an empty directory where the resource should be placed.
	In(ctx context.Context, destDir string, req InRequest) (InResponse, error)

	// Out pushes a new version of the resource.
	// srcDir is an absolute path to the directory containing the pipeline's sources.
	Out(ctx context.Context, srcDir string, req OutRequest) (OutResponse, error)
}
