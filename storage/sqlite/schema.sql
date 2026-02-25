CREATE TABLE IF NOT EXISTS tasks (
  id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
  path TEXT NOT NULL,
  payload BLOB,
  created_at TEXT DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(path)
) STRICT;

CREATE TABLE IF NOT EXISTS pipelines (
  id TEXT NOT NULL PRIMARY KEY,
  name TEXT NOT NULL,
  content TEXT NOT NULL,
  driver_dsn TEXT NOT NULL,
  webhook_secret TEXT NOT NULL DEFAULT '',
  created_at TEXT DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT DEFAULT CURRENT_TIMESTAMP
) STRICT;

CREATE TABLE IF NOT EXISTS pipeline_runs (
  id TEXT NOT NULL PRIMARY KEY,
  pipeline_id TEXT NOT NULL,
  status TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  error_message TEXT,
  created_at TEXT DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (pipeline_id) REFERENCES pipelines(id) ON DELETE CASCADE
) STRICT;

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

-- FTS5 virtual table for pipeline full-text search (name + content).
CREATE VIRTUAL TABLE IF NOT EXISTS pipelines_fts USING fts5(
  id UNINDEXED,
  name,
  content,
  tokenize = 'unicode61'
);

-- FTS5 virtual table for general full-text search over any stored record.
-- content holds ANSI-stripped text extracted from the JSON payload.
CREATE VIRTUAL TABLE IF NOT EXISTS data_fts USING fts5(
  path UNINDEXED,
  content,
  tokenize = 'unicode61'
);

-- Remove FTS entries when a pipeline is deleted.
CREATE TRIGGER IF NOT EXISTS pipelines_fts_delete
AFTER
  DELETE ON pipelines BEGIN
DELETE FROM
  pipelines_fts
WHERE
  id = OLD.id;

END;

-- Remove FTS entries when a task is deleted.
CREATE TRIGGER IF NOT EXISTS data_fts_delete
AFTER
  DELETE ON tasks BEGIN
DELETE FROM
  data_fts
WHERE
  path = OLD.path;

END;