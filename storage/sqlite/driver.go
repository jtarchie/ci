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

func init() {
	storage.Add("sqlite", NewSqlite)
}
