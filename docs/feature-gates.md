# Feature Gates

The CI server supports feature gates that let you control which pipeline
capabilities are available. By default, all features are enabled.

## Available Features

| Feature         | What it controls                                              |
| --------------- | ------------------------------------------------------------- |
| `webhooks`      | Webhook trigger routes, `http.request()`/`http.respond()` API |
| `secrets`       | `secret:` env var resolution and secret injection             |
| `notifications` | The `notify` system (Slack, Teams, HTTP)                      |

## Configuration

### CLI Flag

```bash
ci server --allowed-features "webhooks,secrets"
```

### Environment Variable

```bash
export CI_ALLOWED_FEATURES="webhooks,secrets"
ci server
```

### Wildcard (default)

Use `*` to enable all features (this is the default):

```bash
ci server --allowed-features "*"
```

## Behavior

### Webhooks disabled

- `POST /api/pipelines` rejects requests that include a `webhook_secret`
- `ANY /api/webhooks/:id` returns `403 Forbidden`
- Pipeline execution does **not** receive webhook data or response channels

### Secrets disabled

- `secret:` prefixed env vars are **not** resolved during execution
- The secrets manager is not passed to the pipeline runtime

### Notifications disabled

- Calling `notify.send()` in a pipeline returns an error:
  `"notifications feature is not enabled"`

## Discovery

Query the enabled features at runtime:

```bash
curl http://localhost:8080/api/features
# {"features":["webhooks","secrets","notifications"]}
```

## Error on unknown features

If you specify an unknown feature name, the server will refuse to start:

```
could not parse allowed features: unknown feature "bogus"; known features: webhooks, secrets, notifications
```
