# Runs API

Query pipeline execution history and logs.

## List Runs

`GET /api/runs`

Get paginated execution history.

```bash
curl http://localhost:8080/api/runs
```

## Get Run

`GET /api/runs/:id`

Retrieve a specific run's metadata and status.

```bash
curl http://localhost:8080/api/runs/run-id-123
```

Response includes:

- `status` — `running`, `success`, `failure`, `error`, etc.
- `code` — exit code (0 = success)
- `created_at`, `updated_at` — timestamps

## Get Run Tasks

`GET /api/runs/:id/tasks`

List all tasks (containers) executed in a run.

```bash
curl http://localhost:8080/api/runs/run-id-123/tasks
```

Each task includes:

- `name` — task name
- `status` — execution status
- `code` — exit code
- `stdout`, `stderr` — logs (redacted if secrets were used)
- `started_at`, `ended_at` — timing

See [MCP](./mcp.md) for advanced task search and filtering.
