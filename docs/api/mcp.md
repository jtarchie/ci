# MCP (Model Context Protocol)

The server exposes a Model Context Protocol endpoint for AI assistants and LLM
integrations.

`POST /mcp` or WebSocket `/mcp`

Inspect and search pipeline runs programmatically using MCP tools:

- `get_run` — fetch run status and metadata
- `list_run_tasks` — list tasks in a run with outputs
- `get_run_task` — fetch a single task in a run with full payload/output
- `search_tasks` — full-text search task outputs
- `search_pipelines` — search stored pipelines by name/content

## Client Setup

See [MCP](../guides/mcp.md) for VS Code extension setup and detailed usage.

## Example (Direct HTTP)

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "get_run",
      "arguments": { "run_id": "run-123" }
    }
  }'
```

See [MCP](../guides/mcp.md) for full tool reference and client implementations.

## Example (Single Task Full Payload)

```bash
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
      "name": "get_run_task",
      "arguments": {
        "run_id": "run-123",
        "path": "jobs/review-pr/1/agent/code-quality-reviewer"
      }
    }
  }'
```
