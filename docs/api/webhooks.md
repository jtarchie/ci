# Webhooks API

Trigger pipelines via HTTP webhooks.

`POST /api/webhooks/:pipeline-id`

Execute a pipeline in response to an HTTP request. The pipeline can read the incoming request and optionally send an HTTP response before continuing execution in the background.

## Signature Validation (Optional)

If the pipeline has a `webhook_secret` configured, requests must include an HMAC-SHA256 signature of the request body.

**Via header**:
```
X-Webhook-Signature: <hex-encoded HMAC-SHA256>
```

**Via query parameter**:
```
/api/webhooks/:pipeline-id?signature=<hex-encoded HMAC-SHA256>
```

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
    // Pipeline was triggered via webhook
    http.respond({
      status: 200,
      body: JSON.stringify({ acknowledged: true }),
      headers: { "Content-Type": "application/json" }
    });
  }

  // Pipeline continues running in the background
  await runtime.run({ ... });
};
export { pipeline };
```

See [Webhooks](../guides/webhooks.md) for detailed examples (GitHub, custom signatures, etc.).
