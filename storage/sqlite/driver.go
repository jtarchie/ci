package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/georgysavva/scany/v2/sqlscan"
	"github.com/jtarchie/ci/storage"
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

func (s *Sqlite) Set(prefix string, payload any) error {
	path := filepath.Clean("/" + s.namespace + "/" + prefix)

	contents, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	//nolint: noctx
	_, err = s.writer.Exec(`
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

func (s *Sqlite) Get(prefix string) (storage.Payload, error) {
	path := filepath.Clean("/" + s.namespace + "/" + prefix)

	var payload storage.Payload
	var payloadBytes []byte

	// Use writer instead of reader to work with in-memory databases
	// where each connection gets its own database.
	// Use json() to convert JSONB back to regular JSON text.
	//nolint: noctx
	err := s.writer.QueryRow(`
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

func (s *Sqlite) GetAll(prefix string, fields []string) (storage.Results, error) {
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
		context.Background(),
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

func init() {
	storage.Add("sqlite", NewSqlite)
}
