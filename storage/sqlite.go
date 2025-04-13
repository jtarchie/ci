package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Sqlite struct {
	client    *sql.DB
	namespace string
}

func NewSqlite(filename string, namespace string) (*Sqlite, error) {
	client, err := sql.Open("sqlite", filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	_, err = client.Exec(`
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

	return &Sqlite{
		client:    client,
		namespace: namespace,
	}, nil
}

func (s *Sqlite) Set(prefix string, payload any) error {
	path := filepath.Clean("/" + s.namespace + "/" + prefix)

	contents, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	_, err = s.client.Exec(`
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

func (s *Sqlite) Close() error {
	err := s.client.Close()
	if err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}

	return nil
}
