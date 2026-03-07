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

## Webhook Providers

The server automatically detects the incoming webhook provider and verifies its
signature. Pipelines receive the detected `provider` and `eventType` on the
`http.request()` object.

### GitHub

Detected when the request includes an `X-GitHub-Event` header.

- **`provider`**: `"github"`
- **`eventType`**: value of `X-GitHub-Event` (e.g. `"push"`, `"pull_request"`)
- **Signature header**: `X-Hub-Signature-256: sha256=<hex>` (HMAC-SHA256)
- If a webhook secret is configured and the header is missing or invalid, the
  request is rejected with `401 Unauthorized`.

```bash
curl -X POST http://localhost:8080/api/webhooks/<pipeline-id> \
  -H "X-GitHub-Event: push" \
  -H "X-Hub-Signature-256: sha256=<hex>" \
  -d '{"ref":"refs/heads/main"}'
```

### Slack

Detected when the request includes an `X-Slack-Signature` header.

- **`provider`**: `"slack"`
- **`eventType`**: top-level `type` field from the JSON body (e.g.
  `"event_callback"`, `"url_verification"`)
- **Signature**: `X-Slack-Signature: v0=<hex>` verified against
  `v0:<X-Slack-Request-Timestamp>:<body>` using HMAC-SHA256
- Both `X-Slack-Signature` and `X-Slack-Request-Timestamp` must be present when
  a secret is configured.

### Generic (fallback)

Used for all other requests that don't match a specific provider.

- **`provider`**: `"generic"`
- **`eventType`**: `""` (empty)
- **Signature header**: `X-Webhook-Signature: <hex-encoded HMAC-SHA256>`
- **Signature query param**: `?signature=<hex-encoded HMAC-SHA256>`

This is the same behaviour as the original webhook implementation and is
compatible with any tool that can send a plain HMAC-SHA256 signature.

## JavaScript/TypeScript API

Pipelines access webhook data through the global `http` object.

### `http.request()`

Returns the incoming HTTP request, or `undefined` if the pipeline was not
triggered via webhook.

```typescript
interface HttpRequest {
  provider: string; // Detected provider: "github", "slack", or "generic"
  eventType: string; // Provider-specific event type (e.g. "push", "event_callback")
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

  // req.provider === "github", req.eventType === "push" | "pull_request" | ...
  http.respond({
    status: 200,
    body: JSON.stringify({ accepted: true, event: req.eventType }),
    headers: { "Content-Type": "application/json" },
  });

  const payload = JSON.parse(req.body);
  if (req.eventType === "push") {
    await runtime.run({
      name: "build",
      image: "golang:1.22",
      command: { path: "go", args: ["build", "./..."] },
    });
  }
};
export { pipeline };
```

#### Slack webhook

```typescript
const pipeline = async () => {
  const req = http.request();
  if (!req) return;

  // Handle Slack's url_verification challenge
  if (req.eventType === "url_verification") {
    const { challenge } = JSON.parse(req.body);
    http.respond({
      status: 200,
      body: JSON.stringify({ challenge }),
      headers: { "Content-Type": "application/json" },
    });
    return;
  }

  http.respond({ status: 200, body: "ok" });

  const payload = JSON.parse(req.body);
  console.log("Slack event:", payload.event?.type);
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
