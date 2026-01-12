package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/georgysavva/scany/v2/sqlscan"
	"github.com/jtarchie/ci/storage"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/samber/lo"
	_ "modernc.org/sqlite"
)

type Sqlite struct {
	writer    *sql.DB
	reader    *sql.DB
	namespace string
}

func NewSqlite(dsn string, namespace string, _ *slog.Logger) (storage.Driver, error) {
	dsn = strings.TrimPrefix(dsn, "sqlite://")

	writer, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	//nolint: noctx
	_, err = writer.Exec(`
		CREATE TABLE IF NOT EXISTS tasks (
			id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
			path TEXT NOT NULL,
			payload BLOB,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(path)
		) STRICT;
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create tasks table: %w", err)
	}

	//nolint: noctx
	_, err = writer.Exec(`
		CREATE TABLE IF NOT EXISTS pipelines (
			id TEXT NOT NULL PRIMARY KEY,
			name TEXT NOT NULL,
			content TEXT NOT NULL,
			driver_dsn TEXT NOT NULL,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		) STRICT;
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipelines table: %w", err)
	}

	//nolint: noctx
	_, err = writer.Exec(`
		CREATE TABLE IF NOT EXISTS pipeline_runs (
			id TEXT NOT NULL PRIMARY KEY,
			pipeline_id TEXT NOT NULL,
			status TEXT NOT NULL,
			started_at TEXT,
			completed_at TEXT,
			error_message TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (pipeline_id) REFERENCES pipelines(id)
		) STRICT;
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline_runs table: %w", err)
	}

	//nolint: noctx
	_, err = writer.Exec(`
		CREATE TABLE IF NOT EXISTS resource_versions (
			id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
			resource_name TEXT NOT NULL,
			version BLOB NOT NULL,
			job_name TEXT,
			fetched_at TEXT DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(resource_name, version)
		) STRICT;
		CREATE INDEX IF NOT EXISTS idx_resource_versions_name ON resource_versions(resource_name);
		CREATE INDEX IF NOT EXISTS idx_resource_versions_fetched ON resource_versions(resource_name, fetched_at DESC);
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource_versions table: %w", err)
	}

	writer.SetMaxIdleConns(1)
	writer.SetMaxOpenConns(1)

	reader, err := sql.Open("sqlite", dsn+"?mode=ro&immutable=1")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return &Sqlite{
		writer:    writer,
		reader:    reader,
		namespace: namespace,
	}, nil
}

func (s *Sqlite) Set(ctx context.Context, prefix string, payload any) error {
	path := filepath.Clean("/" + s.namespace + "/" + prefix)

	contents, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	_, err = s.writer.ExecContext(ctx, `
		INSERT INTO tasks (path, payload)
		VALUES (?, ?)
		ON CONFLICT(path) DO UPDATE SET
		payload = jsonb_patch(tasks.payload, excluded.payload);
	`, path, contents, s.namespace)
	if err != nil {
		return fmt.Errorf("failed to insert task: %w", err)
	}

	return nil
}

func (s *Sqlite) Get(ctx context.Context, prefix string) (storage.Payload, error) {
	path := filepath.Clean("/" + s.namespace + "/" + prefix)

	var payload storage.Payload
	var payloadBytes []byte

	// Use writer instead of reader to work with in-memory databases
	// where each connection gets its own database.
	// Use json() to convert JSONB back to regular JSON text.
	err := s.writer.QueryRowContext(ctx, `
		SELECT json(payload) FROM tasks WHERE path = ?
	`, path).Scan(&payloadBytes)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, storage.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	err = json.Unmarshal(payloadBytes, &payload)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	return payload, nil
}

func (s *Sqlite) GetAll(ctx context.Context, prefix string, fields []string) (storage.Results, error) {
	if len(fields) == 0 {
		fields = []string{"status"}
	}

	path := filepath.Clean("/" + s.namespace + "/" + prefix)

	var results storage.Results

	jsonSelects := strings.Join(
		lo.Map(fields, func(field string, _ int) string {
			return fmt.Sprintf("'%s', json_extract(payload, '$.%s')", field, field)
		}),
		",",
	)

	query := fmt.Sprintf(`
			SELECT
				id, path, json_object(%s) as payload
			FROM
				tasks
			WHERE path GLOB :path
			ORDER BY
				id ASC
		`, jsonSelects)

	err := sqlscan.Select(
		ctx,
		s.reader,
		&results,
		query,
		sql.Named("path", path+"*"),
	)
	if err != nil {
		return nil, fmt.Errorf("could not select: %w", err)
	}

	return results, nil
}

func (s *Sqlite) Close() error {
	err := s.writer.Close()
	if err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}

	err = s.reader.Close()
	if err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}

	return nil
}

// SavePipeline creates or updates a pipeline in the database.
func (s *Sqlite) SavePipeline(ctx context.Context, name, content, driverDSN string) (*storage.Pipeline, error) {
	id := gonanoid.Must()
	now := time.Now().UTC()

	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO pipelines (id, name, content, driver_dsn, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, name, content, driverDSN, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("failed to save pipeline: %w", err)
	}

	return &storage.Pipeline{
		ID:        id,
		Name:      name,
		Content:   content,
		DriverDSN: driverDSN,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// GetPipeline retrieves a pipeline by its ID.
func (s *Sqlite) GetPipeline(ctx context.Context, id string) (*storage.Pipeline, error) {
	var pipeline storage.Pipeline
	var createdAt, updatedAt string

	err := s.writer.QueryRowContext(ctx, `
		SELECT id, name, content, driver_dsn, created_at, updated_at
		FROM pipelines WHERE id = ?
	`, id).Scan(&pipeline.ID, &pipeline.Name, &pipeline.Content, &pipeline.DriverDSN, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, storage.ErrNotFound
		}

		return nil, fmt.Errorf("failed to get pipeline: %w", err)
	}

	pipeline.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	pipeline.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	return &pipeline, nil
}

// ListPipelines returns all pipelines in the database.
func (s *Sqlite) ListPipelines(ctx context.Context) ([]storage.Pipeline, error) {
	rows, err := s.writer.QueryContext(ctx, `
		SELECT id, name, content, driver_dsn, created_at, updated_at
		FROM pipelines ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list pipelines: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var pipelines []storage.Pipeline

	for rows.Next() {
		var pipeline storage.Pipeline
		var createdAt, updatedAt string

		err := rows.Scan(&pipeline.ID, &pipeline.Name, &pipeline.Content, &pipeline.DriverDSN, &createdAt, &updatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan pipeline: %w", err)
		}

		pipeline.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		pipeline.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		pipelines = append(pipelines, pipeline)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating pipelines: %w", err)
	}

	return pipelines, nil
}

// DeletePipeline removes a pipeline by its ID.
func (s *Sqlite) DeletePipeline(ctx context.Context, id string) error {
	result, err := s.writer.ExecContext(ctx, `DELETE FROM pipelines WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete pipeline: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return storage.ErrNotFound
	}

	return nil
}

// SaveRun creates a new pipeline run record.
func (s *Sqlite) SaveRun(ctx context.Context, pipelineID string) (*storage.PipelineRun, error) {
	id := gonanoid.Must()
	now := time.Now().UTC()

	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO pipeline_runs (id, pipeline_id, status, created_at)
		VALUES (?, ?, ?, ?)
	`, id, pipelineID, storage.RunStatusQueued, now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("failed to save run: %w", err)
	}

	return &storage.PipelineRun{
		ID:         id,
		PipelineID: pipelineID,
		Status:     storage.RunStatusQueued,
		CreatedAt:  now,
	}, nil
}

// GetRun retrieves a pipeline run by its ID.
func (s *Sqlite) GetRun(ctx context.Context, runID string) (*storage.PipelineRun, error) {
	var run storage.PipelineRun
	var status string
	var createdAt string
	var startedAt, completedAt, errorMessage sql.NullString

	err := s.writer.QueryRowContext(ctx, `
		SELECT id, pipeline_id, status, started_at, completed_at, error_message, created_at
		FROM pipeline_runs WHERE id = ?
	`, runID).Scan(&run.ID, &run.PipelineID, &status, &startedAt, &completedAt, &errorMessage, &createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, storage.ErrNotFound
		}

		return nil, fmt.Errorf("failed to get run: %w", err)
	}

	run.Status = storage.RunStatus(status)
	run.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

	if startedAt.Valid {
		t, _ := time.Parse(time.RFC3339, startedAt.String)
		run.StartedAt = &t
	}

	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339, completedAt.String)
		run.CompletedAt = &t
	}

	if errorMessage.Valid {
		run.ErrorMessage = errorMessage.String
	}

	return &run, nil
}

// ListRunsByPipeline returns all runs for a specific pipeline.
func (s *Sqlite) ListRunsByPipeline(ctx context.Context, pipelineID string) ([]storage.PipelineRun, error) {
	rows, err := s.writer.QueryContext(ctx, `
		SELECT id, pipeline_id, status, started_at, completed_at, error_message, created_at
		FROM pipeline_runs WHERE pipeline_id = ?
		ORDER BY created_at DESC
	`, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("failed to list runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var runs []storage.PipelineRun

	for rows.Next() {
		var run storage.PipelineRun
		var status string
		var createdAt string
		var startedAt, completedAt, errorMessage sql.NullString

		err := rows.Scan(&run.ID, &run.PipelineID, &status, &startedAt, &completedAt, &errorMessage, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan run: %w", err)
		}

		run.Status = storage.RunStatus(status)
		run.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

		if startedAt.Valid {
			t, _ := time.Parse(time.RFC3339, startedAt.String)
			run.StartedAt = &t
		}

		if completedAt.Valid {
			t, _ := time.Parse(time.RFC3339, completedAt.String)
			run.CompletedAt = &t
		}

		if errorMessage.Valid {
			run.ErrorMessage = errorMessage.String
		}

		runs = append(runs, run)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating runs: %w", err)
	}

	return runs, nil
}

// UpdateRunStatus updates the status of a pipeline run.
func (s *Sqlite) UpdateRunStatus(ctx context.Context, runID string, status storage.RunStatus, errorMessage string) error {
	now := time.Now().UTC()

	var query string
	var args []any

	switch status {
	case storage.RunStatusRunning:
		query = `UPDATE pipeline_runs SET status = ?, started_at = ? WHERE id = ?`
		args = []any{status, now.Format(time.RFC3339), runID}
	case storage.RunStatusSuccess, storage.RunStatusFailed:
		query = `UPDATE pipeline_runs SET status = ?, completed_at = ?, error_message = ? WHERE id = ?`
		args = []any{status, now.Format(time.RFC3339), errorMessage, runID}
	default:
		query = `UPDATE pipeline_runs SET status = ? WHERE id = ?`
		args = []any{status, runID}
	}

	result, err := s.writer.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update run status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return storage.ErrNotFound
	}

	return nil
}

// SaveResourceVersion saves a new resource version to the database.
// If the version already exists for the resource, it updates the job_name and fetched_at.
func (s *Sqlite) SaveResourceVersion(ctx context.Context, resourceName string, version map[string]string, jobName string) (*storage.ResourceVersion, error) {
	versionBytes, err := json.Marshal(version)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal version: %w", err)
	}

	now := time.Now().UTC()

	result, err := s.writer.ExecContext(ctx, `
		INSERT INTO resource_versions (resource_name, version, job_name, fetched_at)
		VALUES (?, jsonb(?), ?, ?)
		ON CONFLICT(resource_name, version) DO UPDATE SET
			job_name = excluded.job_name,
			fetched_at = excluded.fetched_at
	`, resourceName, versionBytes, jobName, now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("failed to save resource version: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		// On conflict update doesn't return last insert id, fetch it
		var fetchedID int64
		err = s.writer.QueryRowContext(ctx, `
			SELECT id FROM resource_versions WHERE resource_name = ? AND version = jsonb(?)
		`, resourceName, versionBytes).Scan(&fetchedID)
		if err != nil {
			return nil, fmt.Errorf("failed to get version id: %w", err)
		}
		id = fetchedID
	}

	return &storage.ResourceVersion{
		ID:           id,
		ResourceName: resourceName,
		Version:      version,
		FetchedAt:    now,
		JobName:      jobName,
	}, nil
}

// GetLatestResourceVersion returns the most recently fetched version for a resource.
func (s *Sqlite) GetLatestResourceVersion(ctx context.Context, resourceName string) (*storage.ResourceVersion, error) {
	var rv storage.ResourceVersion
	var versionBytes []byte
	var fetchedAt string
	var jobName sql.NullString

	err := s.writer.QueryRowContext(ctx, `
		SELECT id, resource_name, json(version), job_name, fetched_at
		FROM resource_versions
		WHERE resource_name = ?
		ORDER BY fetched_at DESC
		LIMIT 1
	`, resourceName).Scan(&rv.ID, &rv.ResourceName, &versionBytes, &jobName, &fetchedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, storage.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get latest version: %w", err)
	}

	if err := json.Unmarshal(versionBytes, &rv.Version); err != nil {
		return nil, fmt.Errorf("failed to unmarshal version: %w", err)
	}

	rv.FetchedAt, _ = time.Parse(time.RFC3339, fetchedAt)
	if jobName.Valid {
		rv.JobName = jobName.String
	}

	return &rv, nil
}

// ListResourceVersions returns the most recent versions for a resource, up to limit.
func (s *Sqlite) ListResourceVersions(ctx context.Context, resourceName string, limit int) ([]storage.ResourceVersion, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.writer.QueryContext(ctx, `
		SELECT id, resource_name, json(version), job_name, fetched_at
		FROM resource_versions
		WHERE resource_name = ?
		ORDER BY fetched_at DESC
		LIMIT ?
	`, resourceName, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list versions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var versions []storage.ResourceVersion

	for rows.Next() {
		var rv storage.ResourceVersion
		var versionBytes []byte
		var fetchedAt string
		var jobName sql.NullString

		err := rows.Scan(&rv.ID, &rv.ResourceName, &versionBytes, &jobName, &fetchedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan version: %w", err)
		}

		if err := json.Unmarshal(versionBytes, &rv.Version); err != nil {
			return nil, fmt.Errorf("failed to unmarshal version: %w", err)
		}

		rv.FetchedAt, _ = time.Parse(time.RFC3339, fetchedAt)
		if jobName.Valid {
			rv.JobName = jobName.String
		}

		versions = append(versions, rv)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating versions: %w", err)
	}

	return versions, nil
}

// GetVersionsAfter returns all versions for a resource that were fetched after the given version.
// If afterVersion is nil, returns all versions.
func (s *Sqlite) GetVersionsAfter(ctx context.Context, resourceName string, afterVersion map[string]string) ([]storage.ResourceVersion, error) {
	if afterVersion == nil {
		return s.ListResourceVersions(ctx, resourceName, 0)
	}

	afterVersionBytes, err := json.Marshal(afterVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal after version: %w", err)
	}

	// First find the fetched_at time of the afterVersion
	var afterFetchedAt string
	err = s.writer.QueryRowContext(ctx, `
		SELECT fetched_at FROM resource_versions
		WHERE resource_name = ? AND version = jsonb(?)
	`, resourceName, afterVersionBytes).Scan(&afterFetchedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			// Version not found, return all versions
			return s.ListResourceVersions(ctx, resourceName, 0)
		}
		return nil, fmt.Errorf("failed to find after version: %w", err)
	}

	rows, err := s.writer.QueryContext(ctx, `
		SELECT id, resource_name, json(version), job_name, fetched_at
		FROM resource_versions
		WHERE resource_name = ? AND fetched_at > ?
		ORDER BY fetched_at ASC
	`, resourceName, afterFetchedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get versions after: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var versions []storage.ResourceVersion

	for rows.Next() {
		var rv storage.ResourceVersion
		var versionBytes []byte
		var fetchedAt string
		var jobName sql.NullString

		err := rows.Scan(&rv.ID, &rv.ResourceName, &versionBytes, &jobName, &fetchedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan version: %w", err)
		}

		if err := json.Unmarshal(versionBytes, &rv.Version); err != nil {
			return nil, fmt.Errorf("failed to unmarshal version: %w", err)
		}

		rv.FetchedAt, _ = time.Parse(time.RFC3339, fetchedAt)
		if jobName.Valid {
			rv.JobName = jobName.String
		}

		versions = append(versions, rv)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating versions: %w", err)
	}

	return versions, nil
}

func init() {
	storage.Add("sqlite", NewSqlite)
}
