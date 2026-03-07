# Webhooks API

Trigger pipelines via HTTP webhooks.

`POST /api/webhooks/:pipeline-id`

Execute a pipeline in response to an HTTP request. The pipeline can read the
incoming request and optionally send an HTTP response before continuing
execution in the background.

## Signature Validation (Optional)

The server automatically detects the webhook provider from the request headers
and applies provider-specific signature verification.

| Provider  | Detection header    | Signature header                                            |
| --------- | ------------------- | ----------------------------------------------------------- |
| `github`  | `X-GitHub-Event`    | `X-Hub-Signature-256: sha256=<hex>`                         |
| `slack`   | `X-Slack-Signature` | `X-Slack-Signature: v0=<hex>` + `X-Slack-Request-Timestamp` |
| `generic` | _(fallback)_        | `X-Webhook-Signature: <hex>` or `?signature=<hex>`          |

If the pipeline has a `webhook_secret` configured, requests must pass the
provider's signature check. Requests that fail validation receive
`401 Unauthorized`.

## Example

```bash
curl -X POST http://localhost:8080/api/webhooks/my-pipeline \
  -H "Content-Type: application/json" \
  -H "X-Webhook-Signature: ..." \
  -d '{"event": "push", "branch": "main"}'
```

## Pipeline API

Inside the pipeline, access the incoming request and respond:

```typescript
const pipeline = async () => {
  const req = http.request();
  if (req) {
    // req.provider    — "github" | "slack" | "generic"
    // req.eventType   — e.g. "push", "pull_request", "event_callback"
    http.respond({
      status: 200,
      body: JSON.stringify({ acknowledged: true, provider: req.provider }),
      headers: { "Content-Type": "application/json" }
    });
  }

  // Pipeline continues running in the background
  await runtime.run({ ... });
};
export { pipeline };
```

See [Webhooks](../guides/webhooks.md) for detailed examples (GitHub, custom
signatures, etc.).
