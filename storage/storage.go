package storage

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned when a requested key does not exist.
var ErrNotFound = errors.New("not found")

// Pipeline represents a stored pipeline definition.
type Pipeline struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Content       string    `json:"content"`
	DriverDSN     string    `json:"driver_dsn"`
	WebhookSecret string    `json:"webhook_secret,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// RunStatus represents the status of a pipeline run.
type RunStatus string

const (
	RunStatusQueued  RunStatus = "queued"
	RunStatusRunning RunStatus = "running"
	RunStatusSuccess RunStatus = "success"
	RunStatusFailed  RunStatus = "failed"
)

// PipelineRun represents an execution of a pipeline.
type PipelineRun struct {
	ID           string     `json:"id"`
	PipelineID   string     `json:"pipeline_id"`
	Status       RunStatus  `json:"status"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// PaginationResult holds paginated items along with pagination metadata.
type PaginationResult[T any] struct {
	Items      []T  `json:"items"`
	Page       int  `json:"page"`
	PerPage    int  `json:"per_page"`
	TotalItems int  `json:"total_items"`
	TotalPages int  `json:"total_pages"`
	HasNext    bool `json:"has_next"`
}

// ResourceVersion represents a stored resource version.
type ResourceVersion struct {
	ID           int64             `json:"id"`
	ResourceName string            `json:"resource_name"`
	Version      map[string]string `json:"version"`
	FetchedAt    time.Time         `json:"fetched_at"`
	JobName      string            `json:"job_name,omitempty"` // For passed constraints
}

type Driver interface {
	Close() error
	Set(ctx context.Context, prefix string, payload any) error
	Get(ctx context.Context, prefix string) (Payload, error)
	GetAll(ctx context.Context, prefix string, fields []string) (Results, error)

	// Pipeline CRUD operations
	SavePipeline(ctx context.Context, name, content, driverDSN, webhookSecret string) (*Pipeline, error)
	GetPipeline(ctx context.Context, id string) (*Pipeline, error)
	ListPipelines(ctx context.Context, page, perPage int) (*PaginationResult[Pipeline], error)
	DeletePipeline(ctx context.Context, id string) error

	// Pipeline run operations
	SaveRun(ctx context.Context, pipelineID string) (*PipelineRun, error)
	GetRun(ctx context.Context, runID string) (*PipelineRun, error)
	ListRunsByPipeline(ctx context.Context, pipelineID string, page, perPage int) (*PaginationResult[PipelineRun], error)
	SearchRunsByPipeline(ctx context.Context, pipelineID, query string, page, perPage int) (*PaginationResult[PipelineRun], error)
	UpdateRunStatus(ctx context.Context, runID string, status RunStatus, errorMessage string) error

	// Resource version operations
	SaveResourceVersion(ctx context.Context, resourceName string, version map[string]string, jobName string) (*ResourceVersion, error)
	GetLatestResourceVersion(ctx context.Context, resourceName string) (*ResourceVersion, error)
	ListResourceVersions(ctx context.Context, resourceName string, limit int) ([]ResourceVersion, error)
	GetVersionsAfter(ctx context.Context, resourceName string, afterVersion map[string]string) ([]ResourceVersion, error)

	// Full-text search operations
	//
	// SearchPipelines returns pipelines whose name or content match query using
	// FTS5. An empty query returns all pipelines (same as ListPipelines).
	SearchPipelines(ctx context.Context, query string, page, perPage int) (*PaginationResult[Pipeline], error)

	// Search returns records whose indexed text matches query, scoped to paths
	// that begin with prefix. Set automatically indexes content on write so no
	// separate indexing step is required. prefix follows the same convention as
	// Set (no namespace; the implementation adds it internally).
	Search(ctx context.Context, prefix, query string) (Results, error)
}

type Payload map[string]any

func (p *Payload) Value() (driver.Value, error) {
	contents, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("could not marshal payload: %w", err)
	}

	return contents, nil
}

func (p *Payload) Scan(sqlValue any) error {
	switch typedValue := sqlValue.(type) {
	case string:
		err := json.NewDecoder(bytes.NewBufferString(typedValue)).Decode(p)
		if err != nil {
			return fmt.Errorf("could not unmarshal string payload: %w", err)
		}

		return nil
	case []byte:
		err := json.NewDecoder(bytes.NewBuffer(typedValue)).Decode(p)
		if err != nil {
			return fmt.Errorf("could not unmarshal byte payload: %w", err)
		}

		return nil
	case nil:
		return nil
	default:
		return fmt.Errorf("%w: cannot scan type %T: %v", errors.ErrUnsupported, sqlValue, sqlValue)
	}
}

type Result struct {
	ID      int     `db:"id"`
	Path    string  `db:"path"`
	Payload Payload `db:"payload"`
}

type Results []Result

func (results Results) AsTree() *Tree[Payload] {
	tree := NewTree[Payload]()
	for _, result := range results {
		tree.AddNode(result.Path, result.Payload)
	}

	tree.Flatten()

	return tree
}
