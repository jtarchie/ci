#!/usr/bin/env bash

set -eux

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DEMO_DB="$SCRIPT_DIR/demo.db"
SERVER_URL="http://localhost:8080"
DRIVER="docker"

[ -f "$DEMO_DB" ] && rm "$DEMO_DB"

cd "$PROJECT_ROOT"
rm -f "$DEMO_DB"

go run . server --storage "sqlite://$DEMO_DB" --port 8080 2>&1 &
SERVER_PID=$!

trap 'kill "$SERVER_PID" 2>/dev/null || true; wait "$SERVER_PID" 2>/dev/null || true' EXIT INT TERM

max_attempts=30
attempt=0
while ! curl -s "$SERVER_URL/health" > /dev/null 2>&1; do
    attempt=$((attempt + 1))
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
        echo "server process $SERVER_PID exited before becoming healthy" >&2
        wait "$SERVER_PID" 2>/dev/null || true
        exit 1
    fi
    [ $attempt -ge $max_attempts ] && { echo "server did not become healthy after $max_attempts attempts" >&2; exit 1; }
    sleep 1
done

for yaml_file in "$SCRIPT_DIR"/*.yml; do
    [ -f "$yaml_file" ] || continue
    pipeline_name="$(basename "$yaml_file" .yml)"
    go run . set-pipeline --server-url "$SERVER_URL" --name "$pipeline_name" --driver "$DRIVER" "$yaml_file"
done

wait "$SERVER_PID"
