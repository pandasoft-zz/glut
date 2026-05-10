# Mock API

GLUT starts a mock GitLab API server for each test. Jobs receive
`CI_API_V4_URL` that points to this server. The server records each request so
`assert.api` can check it later.

## Auth

Most endpoints need a token header.

```bash
PRIVATE-TOKEN: test-token
```

Bearer auth is also accepted.

```bash
Authorization: Bearer test-token
```

Token behavior is configured in `setup.api.token`.

```yaml
setup:
  api:
    token:
      valid: true
      expires_at: "2030-01-01T00:00:00Z"
      scopes:
        - "api"
```

If `valid` is false, the server returns `401 Unauthorized`. Write requests need
the `api` scope when scopes are set.

## Built-in Endpoints

`GET /api/v4/version` returns a mock GitLab version. It does not need auth.

`GET /api/v4/personal_access_tokens/self` returns token state.

`GET /api/v4/projects/:id` returns the test project.

The accepted project ids are:

- `1`
- the URL-escaped project path from `setup.api.project.path`

The default project path is `test-group/test-project`.

```yaml
setup:
  api:
    project:
      default_branch: "main"
      path: "group/project"
```

## Tier 2 CRUD Resources

These resources support list, create, get, update, and delete under
`/api/v4/projects/:id`.

| Resource | Path | Identifier |
| --- | --- | --- |
| Releases | `/releases` | `tag_name` |
| Merge requests | `/merge_requests` | `iid` |
| Tags | `/repository/tags` | `name` |
| Branches | `/repository/branches` | `name` |
| Labels | `/labels` | `id` |
| Milestones | `/milestones` | `id` |
| Issues | `/issues` | `iid` |
| Hooks | `/hooks` | `id` |
| Variables | `/variables` | `key` |
| Deployments | `/deployments` | `id` |
| Environments | `/environments` | `id` |
| Pipelines | `/pipelines` | `id` |

Example:

```bash
curl \
  -H "PRIVATE-TOKEN: test-token" \
  -H "Content-Type: application/json" \
  -d '{"tag_name":"v1.2.0","name":"v1.2.0"}' \
  "$CI_API_V4_URL/projects/1/releases"
```

The matching assert can check the recorded call.

```yaml
assert:
  api:
    "POST /api/v4/projects/*/releases":
      called: true
      body:
        tag_name: "v1.2.0"
```

## Tier 1 Special Endpoints

Some endpoints have special behavior.

### Create Commit

`POST /api/v4/projects/:id/repository/commits` returns a mock commit response.
The response includes `id`, `short_id`, `title`, `message`, and
`committed_date`.

### Merge Request Note

`POST /api/v4/projects/:id/merge_requests/:iid/notes` returns a note response
with `id` and `body`.

### Merge Request Approve

`POST /api/v4/projects/:id/merge_requests/:iid/approve` returns:

```json
{
  "approved": true
}
```

## Seed Data

Use `setup.api.seed` to prepare data before the job starts.

```yaml
setup:
  api:
    seed:
      releases:
        - tag_name: "v1.0.0"
          name: "Old release"
      merge_requests:
        - iid: 7
          title: "Update image"
      labels:
        - id: 1
          name: "ready"
```

Seed data is stored in the same in-memory store as data created during the
test.

## Recorded Calls

The recorder stores method, path, request body, status code, and timestamp.
Use `assert.api` with a wildcard path to avoid binding tests to the project id.

```yaml
assert:
  api:
    "POST /api/v4/projects/*/merge_requests/*/notes":
      called: true
      times: 1
      body:
        body:
          contain-substring: "ready"
```
