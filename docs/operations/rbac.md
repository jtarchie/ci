# Authorization (RBAC)

PocketCI uses [expr-lang](https://expr-lang.org) expressions for role-based
access control at two levels: **server-wide** and **per-pipeline**.

If no RBAC expression is configured, all authenticated users have full access.

## User Fields

RBAC expressions evaluate against the authenticated user object:

| Field           | Type       | Description                                            |
| --------------- | ---------- | ------------------------------------------------------ |
| `Email`         | `string`   | User's email address                                   |
| `Name`          | `string`   | Display name                                           |
| `NickName`      | `string`   | Provider username                                      |
| `Provider`      | `string`   | OAuth provider (`github`, `gitlab`, `microsoftonline`) |
| `UserID`        | `string`   | Unique ID from the provider                            |
| `Organizations` | `[]string` | GitHub organizations (GitHub provider only)            |
| `Groups`        | `[]string` | Groups from the provider (GitLab, Microsoft)           |

## Server-Level RBAC

Restrict who can access the entire server using `--server-rbac`:

```bash
pocketci server \
  --oauth-github-client-id ... \
  --oauth-github-client-secret ... \
  --oauth-session-secret ... \
  --oauth-callback-url https://ci.example.com \
  --server-rbac '"myorg" in Organizations'
```

Users who authenticate but don't match the expression receive a `403 Forbidden`
response.

### Examples

```bash
# Only GitHub users in the "engineering" org
--server-rbac '"engineering" in Organizations'

# Only users from a specific email domain
--server-rbac 'Email endsWith "@company.com"'

# Multiple conditions
--server-rbac '"myorg" in Organizations && Provider == "github"'
```

## Pipeline-Level RBAC

Each pipeline can have its own access control expression, set via the `--rbac`
flag on `pocketci set-pipeline`:

```bash
pocketci set-pipeline my-pipeline.ts \
  --server http://localhost:8080 \
  --rbac '"deploy-team" in Organizations'
```

Or via the API:

```bash
curl -X PUT http://localhost:8080/api/pipelines/my-pipeline \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{
    "content": "...",
    "content_type": "ts",
    "rbac_expression": "\"deploy-team\" in Organizations"
  }'
```

Pipeline RBAC controls:

- Viewing the pipeline and its runs
- Triggering the pipeline
- Deleting the pipeline
- Executing `pocketci run` against the pipeline

Pipelines without an RBAC expression are accessible to all authenticated users.

### Expression Examples

```bash
# Only specific users
--rbac 'Email == "alice@example.com" || Email == "bob@example.com"'

# Organization membership
--rbac '"platform-team" in Organizations'

# Provider restriction
--rbac 'Provider == "github"'

# Combined conditions
--rbac 'Provider == "github" && "myorg" in Organizations && "admin" in Groups'
```

## Expression Syntax

PocketCI uses [expr-lang](https://expr-lang.org/docs/language-definition)
expressions. Common operators:

| Operator     | Example                                          |
| ------------ | ------------------------------------------------ |
| `==`         | `Email == "alice@example.com"`                   |
| `!=`         | `Provider != "gitlab"`                           |
| `in`         | `"myorg" in Organizations`                       |
| `&&`         | `Provider == "github" && "org" in Organizations` |
| `\|\|`       | `Email == "a@b.com" \|\| Email == "c@d.com"`     |
| `!`          | `!("banned" in Groups)`                          |
| `endsWith`   | `Email endsWith "@company.com"`                  |
| `startsWith` | `NickName startsWith "admin"`                    |
| `contains`   | `Name contains "Smith"`                          |

Expressions are validated at configuration time — invalid syntax causes an
immediate error rather than a runtime failure.
