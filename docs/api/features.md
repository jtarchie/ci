# Features API

List and query enabled feature gates.

## List Features

`GET /api/features`

Returns available feature gates enabled on the server.

```bash
curl http://localhost:8080/api/features
```

Response:

```json
{
  "features": [
    {
      "name": "my_feature",
      "enabled": true
    }
  ]
}
```

The response reflects the `--allowed-features` configured on the server.

See [Feature Gates](../operations/feature-gates.md) for details on specific
features.
