# MCP (Model Context Protocol)

The server exposes a Model Context Protocol endpoint for AI assistants and LLM integrations.

`POST /mcp` or WebSocket `/mcp`

Inspect and search pipeline runs programmatically using MCP tools:

- `get_run` — fetch run status and metadata
- `list_run_tasks` — list tasks in a run with outputs
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
