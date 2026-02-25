package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/georgysavva/scany/v2/sqlscan"
	"github.com/jtarchie/ci/runtime"
	"github.com/jtarchie/ci/storage"
	"github.com/jtarchie/lqs"
	"github.com/samber/lo"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type Sqlite struct {
	writer    *sql.DB
	reader    *sql.DB
	namespace string
}

func NewSqlite(dsn string, namespace string, _ *slog.Logger) (storage.Driver, error) {
	dsn = strings.TrimPrefix(dsn, "sqlite://")

	writer, err := lqs.Open("sqlite", dsn, `
		PRAGMA journal_mode = WAL;
		PRAGMA synchronous = NORMAL;
		PRAGMA foreign_keys = ON;
		PRAGMA busy_timeout = 5000;
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	//nolint: noctx
	_, err = writer.Exec(schemaSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to apply schema: %w", err)
	}

	writer.SetMaxIdleConns(1)
	writer.SetMaxOpenConns(1)

	reader, err := lqs.Open("sqlite", dsn, `
		PRAGMA foreign_keys = ON;
		PRAGMA busy_timeout = 5000;
		PRAGMA query_only = ON;
	`)
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

	// Keep the FTS index in sync: delete any stale entry then insert fresh.
	_, err = s.writer.ExecContext(ctx, `
		DELETE FROM data_fts WHERE rowid IN (
			SELECT rowid FROM data_fts WHERE path = ?
		)
	`, path)
	if err != nil {
		return fmt.Errorf("failed to clear data_fts: %w", err)
	}

	text := stripANSI(extractTextFromJSON(contents))
	_, err = s.writer.ExecContext(ctx, `INSERT INTO data_fts(path, content) VALUES (?, ?)`, path, path+" "+text)
	if err != nil {
		return fmt.Errorf("failed to index data_fts: %w", err)
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
func (s *Sqlite) SavePipeline(ctx context.Context, name, content, driverDSN, webhookSecret string) (*storage.Pipeline, error) {
	id := runtime.PipelineID(name, content)
	now := time.Now().UTC()

	_, err := s.writer.ExecContext(ctx, `
		INSERT INTO pipelines (id, name, content, driver_dsn, webhook_secret, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			driver_dsn=excluded.driver_dsn,
			webhook_secret=excluded.webhook_secret,
			updated_at=excluded.updated_at
	`, id, name, content, driverDSN, webhookSecret, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("failed to save pipeline: %w", err)
	}

	// Keep FTS index in sync: delete any existing entry then re-insert.
	_, err = s.writer.ExecContext(ctx, `
		DELETE FROM pipelines_fts WHERE rowid IN (
			SELECT rowid FROM pipelines_fts WHERE id = ?
		)
	`, id)
	if err != nil {
		return nil, fmt.Errorf("failed to clear pipelines_fts: %w", err)
	}

	_, err = s.writer.ExecContext(ctx,
		`INSERT INTO pipelines_fts(id, name, content) VALUES (?, ?, ?)`,
		id, name, content,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to index pipeline: %w", err)
	}

	return &storage.Pipeline{
		ID:            id,
		Name:          name,
		Content:       content,
		DriverDSN:     driverDSN,
		WebhookSecret: webhookSecret,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

// GetPipeline retrieves a pipeline by its ID.
func (s *Sqlite) GetPipeline(ctx context.Context, id string) (*storage.Pipeline, error) {
	var pipeline storage.Pipeline
	var createdAt, updatedAt string

	err := s.writer.QueryRowContext(ctx, `
		SELECT id, name, content, driver_dsn, webhook_secret, created_at, updated_at
		FROM pipelines WHERE id = ?
	`, id).Scan(&pipeline.ID, &pipeline.Name, &pipeline.Content, &pipeline.DriverDSN, &pipeline.WebhookSecret, &createdAt, &updatedAt)
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

// GetPipelineByName retrieves the most recently updated pipeline with the given name.
func (s *Sqlite) GetPipelineByName(ctx context.Context, name string) (*storage.Pipeline, error) {
	var pipeline storage.Pipeline
	var createdAt, updatedAt string

	err := s.writer.QueryRowContext(ctx, `
		SELECT id, name, content, driver_dsn, webhook_secret, created_at, updated_at
		FROM pipelines WHERE name = ?
		ORDER BY updated_at DESC LIMIT 1
	`, name).Scan(&pipeline.ID, &pipeline.Name, &pipeline.Content, &pipeline.DriverDSN, &pipeline.WebhookSecret, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, storage.ErrNotFound
		}

		return nil, fmt.Errorf("failed to get pipeline by name: %w", err)
	}

	pipeline.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	pipeline.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	return &pipeline, nil
}

// ListPipelines returns a paginated list of pipelines in the database.
func (s *Sqlite) ListPipelines(ctx context.Context, page, perPage int) (*storage.PaginationResult[storage.Pipeline], error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}

	offset := (page - 1) * perPage

	// Get total count
	var totalItems int
	err := s.writer.QueryRowContext(ctx, `SELECT COUNT(*) FROM pipelines`).Scan(&totalItems)
	if err != nil {
		return nil, fmt.Errorf("failed to count pipelines: %w", err)
	}

	// Get paginated results
	rows, err := s.writer.QueryContext(ctx, `
		SELECT id, name, content, driver_dsn, webhook_secret, created_at, updated_at
		FROM pipelines ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, perPage, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list pipelines: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var pipelines []storage.Pipeline

	for rows.Next() {
		var pipeline storage.Pipeline
		var createdAt, updatedAt string

		err := rows.Scan(&pipeline.ID, &pipeline.Name, &pipeline.Content, &pipeline.DriverDSN, &pipeline.WebhookSecret, &createdAt, &updatedAt)
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

	totalPages := (totalItems + perPage - 1) / perPage
	hasNext := page < totalPages

	return &storage.PaginationResult[storage.Pipeline]{
		Items:      pipelines,
		Page:       page,
		PerPage:    perPage,
		TotalItems: totalItems,
		TotalPages: totalPages,
		HasNext:    hasNext,
	}, nil
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
	id := runtime.UniqueID()
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

// GetLatestRunByPipeline returns the most recent run for a pipeline, or ErrNotFound if none exist.
func (s *Sqlite) GetLatestRunByPipeline(ctx context.Context, pipelineID string) (*storage.PipelineRun, error) {
	var run storage.PipelineRun
	var status string
	var createdAt string
	var startedAt, completedAt, errorMessage sql.NullString

	err := s.writer.QueryRowContext(ctx, `
		SELECT id, pipeline_id, status, started_at, completed_at, error_message, created_at
		FROM pipeline_runs WHERE pipeline_id = ?
		ORDER BY created_at DESC LIMIT 1
	`, pipelineID).Scan(&run.ID, &run.PipelineID, &status, &startedAt, &completedAt, &errorMessage, &createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, storage.ErrNotFound
		}

		return nil, fmt.Errorf("failed to get latest run: %w", err)
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

// ListRunsByPipeline returns a paginated list of runs for a specific pipeline.
func (s *Sqlite) ListRunsByPipeline(ctx context.Context, pipelineID string, page, perPage int) (*storage.PaginationResult[storage.PipelineRun], error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}

	offset := (page - 1) * perPage

	// Get total count for this pipeline
	var totalItems int
	err := s.writer.QueryRowContext(ctx, `SELECT COUNT(*) FROM pipeline_runs WHERE pipeline_id = ?`, pipelineID).Scan(&totalItems)
	if err != nil {
		return nil, fmt.Errorf("failed to count runs: %w", err)
	}

	// Get paginated results
	rows, err := s.writer.QueryContext(ctx, `
		SELECT id, pipeline_id, status, started_at, completed_at, error_message, created_at
		FROM pipeline_runs WHERE pipeline_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, pipelineID, perPage, offset)
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

	totalPages := (totalItems + perPage - 1) / perPage
	hasNext := page < totalPages

	return &storage.PaginationResult[storage.PipelineRun]{
		Items:      runs,
		Page:       page,
		PerPage:    perPage,
		TotalItems: totalItems,
		TotalPages: totalPages,
		HasNext:    hasNext,
	}, nil
}

// SearchRunsByPipeline returns a paginated list of runs for a specific pipeline
// filtered by query matching the run ID, status, or error message.
// When query is empty it behaves like ListRunsByPipeline.
func (s *Sqlite) SearchRunsByPipeline(ctx context.Context, pipelineID, query string, page, perPage int) (*storage.PaginationResult[storage.PipelineRun], error) {
	if query == "" {
		return s.ListRunsByPipeline(ctx, pipelineID, page, perPage)
	}

	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}

	offset := (page - 1) * perPage
	like := "%" + query + "%"

	var totalItems int
	err := s.writer.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pipeline_runs
		WHERE pipeline_id = ?
		  AND (id LIKE ? OR status LIKE ? OR error_message LIKE ?)
	`, pipelineID, like, like, like).Scan(&totalItems)
	if err != nil {
		return nil, fmt.Errorf("failed to count run search results: %w", err)
	}

	rows, err := s.writer.QueryContext(ctx, `
		SELECT id, pipeline_id, status, started_at, completed_at, error_message, created_at
		FROM pipeline_runs
		WHERE pipeline_id = ?
		  AND (id LIKE ? OR status LIKE ? OR error_message LIKE ?)
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, pipelineID, like, like, like, perPage, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to search runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var runs []storage.PipelineRun

	for rows.Next() {
		var run storage.PipelineRun
		var status string
		var createdAt string
		var startedAt, completedAt, errorMessage sql.NullString

		if err := rows.Scan(&run.ID, &run.PipelineID, &status, &startedAt, &completedAt, &errorMessage, &createdAt); err != nil {
			return nil, fmt.Errorf("failed to scan run search result: %w", err)
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
		return nil, fmt.Errorf("error iterating run search results: %w", err)
	}

	totalPages := (totalItems + perPage - 1) / perPage

	return &storage.PaginationResult[storage.PipelineRun]{
		Items:      runs,
		Page:       page,
		PerPage:    perPage,
		TotalItems: totalItems,
		TotalPages: totalPages,
		HasNext:    page < totalPages,
	}, nil
}

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
		ORDER BY id DESC
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
		ORDER BY id ASC
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

// SearchPipelines returns pipelines whose name or content contain query using
// the FTS5 index. When query is empty it behaves like ListPipelines.
func (s *Sqlite) SearchPipelines(ctx context.Context, query string, page, perPage int) (*storage.PaginationResult[storage.Pipeline], error) {
	if query == "" {
		return s.ListPipelines(ctx, page, perPage)
	}

	if page < 1 {
		page = 1
	}

	if perPage < 1 {
		perPage = 20
	}

	ftsQuery := sanitizeFTSQuery(query)
	offset := (page - 1) * perPage

	var totalItems int

	err := s.writer.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pipelines
		WHERE id IN (SELECT id FROM pipelines_fts WHERE pipelines_fts MATCH ?)
	`, ftsQuery).Scan(&totalItems)
	if err != nil {
		return nil, fmt.Errorf("failed to count pipeline search results: %w", err)
	}

	rows, err := s.writer.QueryContext(ctx, `
		SELECT p.id, p.name, p.content, p.driver_dsn, p.webhook_secret, p.created_at, p.updated_at
		FROM pipelines p
		WHERE p.id IN (SELECT id FROM pipelines_fts WHERE pipelines_fts MATCH ?)
		ORDER BY p.created_at DESC
		LIMIT ? OFFSET ?
	`, ftsQuery, perPage, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to search pipelines: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var pipelines []storage.Pipeline

	for rows.Next() {
		var pipeline storage.Pipeline
		var createdAt, updatedAt string

		if err := rows.Scan(
			&pipeline.ID, &pipeline.Name, &pipeline.Content,
			&pipeline.DriverDSN, &pipeline.WebhookSecret,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan pipeline search result: %w", err)
		}

		pipeline.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		pipeline.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		pipelines = append(pipelines, pipeline)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating pipeline search results: %w", err)
	}

	totalPages := (totalItems + perPage - 1) / perPage

	return &storage.PaginationResult[storage.Pipeline]{
		Items:      pipelines,
		Page:       page,
		PerPage:    perPage,
		TotalItems: totalItems,
		TotalPages: totalPages,
		HasNext:    page < totalPages,
	}, nil
}

// Search returns records whose indexed text matches query and whose path begins
// with prefix. prefix follows the same convention as Set (no namespace prefix).
func (s *Sqlite) Search(ctx context.Context, prefix, query string) (storage.Results, error) {
	if query == "" {
		return nil, nil
	}

	ftsQuery := sanitizeFTSQuery(query)
	fullPrefix := filepath.Clean("/" + s.namespace + "/" + prefix)

	rows, err := s.writer.QueryContext(ctx, `
		SELECT
			COALESCE(t.id, 0),
			f.path,
			COALESCE(
				json_object(
					'status',     json_extract(t.payload, '$.status'),
					'elapsed',    json_extract(t.payload, '$.elapsed'),
					'started_at', json_extract(t.payload, '$.started_at')
				),
				'{}'
			) AS payload
		FROM data_fts f
		LEFT JOIN tasks t ON t.path = f.path
		WHERE data_fts MATCH ? AND f.path LIKE ? || '/%'
		ORDER BY COALESCE(t.id, f.rowid) ASC
	`, ftsQuery, fullPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results storage.Results

	for rows.Next() {
		var result storage.Result
		var payloadBytes []byte

		if err := rows.Scan(&result.ID, &result.Path, &payloadBytes); err != nil {
			return nil, fmt.Errorf("failed to scan search result: %w", err)
		}

		if err := json.Unmarshal(payloadBytes, &result.Payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal search payload: %w", err)
		}

		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating search results: %w", err)
	}

	return results, nil
}

// sanitizeFTSQuery converts a freeform user query into a safe FTS5 query.
// Each whitespace-separated token is treated as a literal prefix match term,
// preventing accidental use of FTS5 boolean operators (AND, OR, NOT, etc.).
func sanitizeFTSQuery(q string) string {
	words := strings.Fields(q)
	if len(words) == 0 {
		return ""
	}

	terms := make([]string, 0, len(words))

	for _, w := range words {
		// Escape any embedded double-quotes and wrap as a quoted literal with
		// prefix matching (*) so incremental search works naturally.
		safe := strings.ReplaceAll(w, `"`, `""`)
		terms = append(terms, `"`+safe+`"*`)
	}

	return strings.Join(terms, " ")
}

func init() {
	storage.Add("sqlite", NewSqlite)
}
