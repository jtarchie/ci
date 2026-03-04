# Webhooks

Trigger pipeline execution via HTTP webhooks. Pipelines can read the incoming
request and optionally send an HTTP response while continuing to execute in the
background.

## Setup

### 1. Create a pipeline with a webhook secret

```bash
ci set-pipeline my-pipeline.ts \
  --server http://localhost:8080 \
  --webhook-secret "my-secret-key"
```

The webhook secret is optional. If omitted, the webhook endpoint accepts all
requests without signature validation.

### 2. Configure the server

```bash
ci server \
  --port 8080 \
  --storage sqlite://ci.db \
  --webhook-timeout 5s   # How long to wait for http.respond() (default: 5s)
```

## Triggering Pipelines

Send any HTTP method to `/api/webhooks/:pipeline_id`:

```bash
curl -X POST http://localhost:8080/api/webhooks/<pipeline-id> \
  -H "Content-Type: application/json" \
  -H "X-Webhook-Signature: <hmac-sha256-hex>" \
  -d '{"event": "push", "ref": "refs/heads/main"}'
```

### Signature Validation

When a pipeline has a webhook secret configured, requests must include an
HMAC-SHA256 signature of the raw request body.

**Via header** (preferred):

```
X-Webhook-Signature: <hex-encoded HMAC-SHA256>
```

**Via query parameter** (for providers that don't support custom headers):

```
/api/webhooks/<id>?signature=<hex-encoded HMAC-SHA256>
```

Computing the signature:

```bash
# Bash
echo -n '{"event": "push"}' | openssl dgst -sha256 -hmac "my-secret-key" | cut -d' ' -f2
```

```python
# Python
import hmac, hashlib
hmac.new(b"my-secret-key", b'{"event": "push"}', hashlib.sha256).hexdigest()
```

```javascript
// Node.js
const crypto = require("crypto");
crypto
  .createHmac("sha256", "my-secret-key")
  .update('{"event": "push"}')
  .digest("hex");
```

## JavaScript/TypeScript API

Pipelines access webhook data through the global `http` object.

### `http.request()`

Returns the incoming HTTP request, or `undefined` if the pipeline was not
triggered via webhook.

```typescript
interface HttpRequest {
  method: string; // "GET", "POST", etc.
  url: string; // Request URL path with query string
  headers: Record<string, string>; // Request headers
  body: string; // Raw request body
  query: Record<string, string>; // Parsed query parameters
}
```

### `http.respond(response)`

Sends an HTTP response back to the webhook caller. The pipeline continues
executing after the response is sent.

```typescript
interface HttpResponse {
  status: number; // HTTP status code (default: 200)
  body?: string; // Response body
  headers?: Record<string, string>; // Response headers
}
```

**Key behaviors:**

- One-shot: only the first call takes effect; subsequent calls are ignored
- Non-blocking: the pipeline continues running after responding
- No-op when not triggered via webhook
- If `http.respond()` is not called within the server's `--webhook-timeout`, the
  server returns `202 Accepted` with a JSON body containing the `run_id`

### Examples

#### Minimal webhook

```typescript
const pipeline = async () => {
  const req = http.request();
  if (req) {
    http.respond({ status: 200, body: "ok" });
  }
  // Continue processing...
};
export { pipeline };
```

#### Read body and respond with data

```typescript
const pipeline = async () => {
  const req = http.request();
  if (req) {
    const data = JSON.parse(req.body);
    http.respond({
      status: 200,
      body: JSON.stringify({ received: true, keys: Object.keys(data) }),
      headers: { "Content-Type": "application/json" },
    });
  }
  // Run containers in the background after responding
  await runtime.run({
    name: "process",
    image: "alpine",
    command: { path: "echo", args: ["processing webhook"] },
  });
};
export { pipeline };
```

#### GitHub webhook

```typescript
const pipeline = async () => {
  const req = http.request();
  if (!req) return;

  const event = req.headers["X-Github-Event"];
  http.respond({
    status: 200,
    body: JSON.stringify({ accepted: true, event }),
    headers: { "Content-Type": "application/json" },
  });

  const payload = JSON.parse(req.body);
  if (event === "push") {
    await runtime.run({
      name: "build",
      image: "golang:1.22",
      command: { path: "go", args: ["build", "./..."] },
    });
  }
};
export { pipeline };
```

#### Pipeline that works both ways

```typescript
const pipeline = async () => {
  const req = http.request();

  if (req) {
    // Triggered via webhook
    http.respond({ status: 200, body: "acknowledged" });
    console.log(`Webhook: ${req.method} from ${req.headers["User-Agent"]}`);
  } else {
    // Triggered manually via /api/pipelines/:id/trigger
    console.log("Manual trigger");
  }

  // Same logic regardless of trigger method
  await runtime.run({
    name: "test",
    image: "golang:1.22",
    command: { path: "go", args: ["test", "./..."] },
  });
};
export { pipeline };
```

## Response Behavior

| Scenario                                       | HTTP Response                               |
| ---------------------------------------------- | ------------------------------------------- |
| Pipeline calls `http.respond()` before timeout | Pipeline's response (status, body, headers) |
| Pipeline doesn't call `http.respond()` in time | `202 Accepted` with `{"run_id": "..."}`     |
| Pipeline errors before responding              | `202 Accepted` (pipeline still ran)         |
| No webhook secret, no signature sent           | Request accepted (no validation)            |
| Webhook secret set, no signature               | `401 Unauthorized`                          |
| Webhook secret set, invalid signature          | `401 Unauthorized`                          |

## Pipeline Context

When triggered via webhook, `pipelineContext.triggeredBy` is set to `"webhook"`.
For manual triggers, it is `"manual"`.

```typescript
if (pipelineContext.triggeredBy === "webhook") {
  // Handle webhook-specific logic
}
```
