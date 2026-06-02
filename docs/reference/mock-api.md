# Mock API

GLUT starts a mock GitLab API server for each test. Jobs receive
`CI_API_V4_URL` that points to this server. The server records each request so
`assert.api` can check it after the pipeline run.

The mock API is not a full GitLab clone. It supports the endpoints that CI
components commonly need in tests: project data, token data, common project
resources, and a few special endpoints.

## How Jobs Call It

Use the normal GitLab CI variable in the job:

```bash
curl \
  -H "PRIVATE-TOKEN: $GITLAB_TOKEN" \
  "$CI_API_V4_URL/projects/1"
```

GLUT sets `CI_API_V4_URL` to the local mock server. The test decides token and
project behavior in `setup.api`.

```yaml
.glut:
  setup:
    api:
      token:
        valid: true
        scopes:
          - "api"
      project:
        path: "group/project"
        default_branch: "main"
```

## `setup.api` Reference

| Field | Type | Meaning |
| --- | --- | --- |
| `token` | object | Token state used by auth checks and token self response. |
| `project` | object | Project data returned by the project endpoint. |
| `seed` | object | Initial API resources available before the job starts. |

### `token`

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `valid` | boolean | `true` | When false, protected endpoints return `401 Unauthorized`. |
| `expires_at` | string | empty | Value returned by token self endpoint. |
| `scopes` | list of strings | empty | When set, API reads need `api` or `read_api`. API writes need `api`. |

```yaml
setup:
  api:
    token:
      valid: true
      expires_at: "2030-01-01T00:00:00Z"
      scopes:
        - "api"
```

Use an invalid token to test failure paths:

```yaml
setup:
  api:
    token:
      valid: false
```

Use read-only scope to reject writes:

```yaml
setup:
  api:
    token:
      valid: true
      scopes:
        - "read_api"
```

### `project`

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `path` | string | `test-group/test-project` | Project path with namespace. |
| `default_branch` | string | `main` | Project default branch. |
| `access_level` | string or integer | `maintainer` | Member access level in `permissions.project_access`. String values: `guest` (10), `reporter` (20), `developer` (30), `maintainer` (40), `owner` (50). |

```yaml
setup:
  api:
    project:
      path: "platform/components"
      default_branch: "trunk"
      access_level: developer
```

The mock accepts two project ids:

- `1`
- the URL-escaped project path, such as `platform%2Fcomponents`

### `seed`

`seed` prepares resource data before the job starts.

| Field | Type | Meaning |
| --- | --- | --- |
| `releases` | list | Initial release objects. |
| `merge_requests` | list | Initial merge request objects. |
| `labels` | list | Initial label objects. |

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
          state: "opened"
      labels:
        - id: 1
          name: "ready"
          color: "#00ff00"
```

Seeded data uses the same store as data created during the test. A job can read,
update, or delete seeded objects.

## Auth Rules

Most endpoints need one of these headers:

```text
PRIVATE-TOKEN: test-token
```

```text
Authorization: Bearer test-token
```

Auth behavior:

| Case | Result |
| --- | --- |
| No token header | `401 Unauthorized` |
| `setup.api.token.valid: false` | `401 Unauthorized` |
| Read request with `api` or `read_api` | Allowed |
| Write request with `read_api` | `403 Forbidden` |
| API request with only `write_repository` | `401 Unauthorized` |
| Request with no scopes set | Allowed |
| `GET /api/v4/version` | Allowed without auth |

Write methods are `POST`, `PUT`, `DELETE`, and `PATCH`.

## Built-in Endpoints

| Method | Path | Auth | Response |
| --- | --- | --- | --- |
| `GET` | `/api/v4/version` | no | GitLab version object. |
| `GET` | `/api/v4/personal_access_tokens/self` | yes | Token state from `setup.api.token`. |
| `GET` | `/api/v4/projects/:id` | yes | Project state from `setup.api.project`. |

### Version

```bash
curl "$CI_API_V4_URL/version"
```

Example response:

```json
{
  "version": "16.11.0",
  "revision": "mock"
}
```

### Token Self

```bash
curl \
  -H "PRIVATE-TOKEN: test-token" \
  "$CI_API_V4_URL/personal_access_tokens/self"
```

Example response:

```json
{
  "id": 1,
  "name": "glut token",
  "active": true,
  "revoked": false,
  "scopes": ["api"],
  "expires_at": "2030-01-01T00:00:00Z"
}
```

### Project

```bash
curl \
  -H "PRIVATE-TOKEN: test-token" \
  "$CI_API_V4_URL/projects/1"
```

Example response:

```json
{
  "id": 1,
  "path_with_namespace": "test-group/test-project",
  "name": "test-project",
  "default_branch": "main",
  "permissions": {
    "project_access": {
      "access_level": 40
    },
    "group_access": null
  }
}
```

## Standard Project Resources

Standard project resources use common create, read, update, and delete behavior
under `/api/v4/projects/:id`. These endpoints are useful for GitLab objects
that behave like stored records.

| Action | Method | Path | Status |
| --- | --- | --- | --- |
| List | `GET` | `/api/v4/projects/:id/<resource>` | `200 OK` |
| Create | `POST` | `/api/v4/projects/:id/<resource>` | `201 Created` |
| Get one | `GET` | `/api/v4/projects/:id/<resource>/<identifier>` | `200 OK` or `404 Not Found` |
| Update | `PUT` | `/api/v4/projects/:id/<resource>/<identifier>` | `200 OK` or `404 Not Found` |
| Delete | `DELETE` | `/api/v4/projects/:id/<resource>/<identifier>` | `200 OK` or `404 Not Found` |

Supported resources:

| Resource | Path | Identifier | Default fields |
| --- | --- | --- | --- |
| Releases | `/releases` | `tag_name` | `tag_name`, `name`, `description` |
| Merge requests | `/merge_requests` | `iid` | `iid`, `title`, `state`, `labels` |
| Tags | `/repository/tags` | `name` | `name`, `message` |
| Branches | `/repository/branches` | `name` | `name`, `protected` |
| Labels | `/labels` | `id` | `id`, `name`, `color` |
| Milestones | `/milestones` | `id` | `id` |
| Issues | `/issues` | `iid` | `iid` |
| Hooks | `/hooks` | `id` | `id` |
| Variables | `/variables` | `key` | `key`, `value` |
| Deployments | `/deployments` | `id` | `id` |
| Environments | `/environments` | `id` | `id` |
| Pipelines | `/pipelines` | `id` | `id` |

### Create A Release

Job script:

```bash
curl \
  -H "PRIVATE-TOKEN: test-token" \
  -H "Content-Type: application/json" \
  -d '{"tag_name":"v1.2.0","name":"v1.2.0"}' \
  "$CI_API_V4_URL/projects/1/releases"
```

Assert:

```yaml
assert:
  api:
    "POST /api/v4/projects/*/releases":
      called: true
      times: 1
      body:
        tag_name: "v1.2.0"
        name: "v1.2.0"
```

### Read A Seeded Release

Setup:

```yaml
setup:
  api:
    seed:
      releases:
        - tag_name: "v1.2.0"
          name: "v1.2.0"
```

Job script:

```bash
curl \
  -H "PRIVATE-TOKEN: test-token" \
  "$CI_API_V4_URL/projects/1/releases/v1.2.0"
```

Assert:

```yaml
assert:
  api:
    "GET /api/v4/projects/*/releases/*":
      called: true
```

## Special Project Endpoints

Special project endpoints have custom behavior. They are still recorded for
`assert.api`, but they do more than store and return records.

| Method | Path | Behavior |
| --- | --- | --- |
| `POST` | `/api/v4/projects/:id/repository/commits` | Returns a mock commit response. |
| `POST` | `/api/v4/projects/:id/merge_requests/:iid/notes` | Returns a mock note response. |
| `POST` | `/api/v4/projects/:id/merge_requests/:iid/approve` | Returns an approve response. |

### Create Commit

Request:

```bash
curl \
  -H "PRIVATE-TOKEN: test-token" \
  -H "Content-Type: application/json" \
  -d '{"branch":"main","commit_message":"update manifest","actions":[]}' \
  "$CI_API_V4_URL/projects/1/repository/commits"
```

Response fields:

| Field | Value |
| --- | --- |
| `id` | `mock-commit-sha` |
| `short_id` | `mock-com` |
| `title` | `commit_message` from the request body |
| `message` | `commit_message` from the request body |
| `committed_date` | Current time in RFC3339 format |

Assert:

```yaml
assert:
  api:
    "POST /api/v4/projects/*/repository/commits":
      called: true
      body:
        commit_message:
          contain-substring: "update manifest"
```

### Add Merge Request Note

Request:

```bash
curl \
  -H "PRIVATE-TOKEN: test-token" \
  -H "Content-Type: application/json" \
  -d '{"body":"release is ready"}' \
  "$CI_API_V4_URL/projects/1/merge_requests/7/notes"
```

Response:

```json
{
  "id": 1,
  "body": "release is ready"
}
```

Assert:

```yaml
assert:
  api:
    "POST /api/v4/projects/*/merge_requests/*/notes":
      called: true
      body:
        body:
          contain-substring: "ready"
```

### Approve Merge Request

Request:

```bash
curl \
  -X POST \
  -H "PRIVATE-TOKEN: test-token" \
  "$CI_API_V4_URL/projects/1/merge_requests/7/approve"
```

Response:

```json
{
  "approved": true
}
```

Assert:

```yaml
assert:
  api:
    "POST /api/v4/projects/*/merge_requests/*/approve":
      called: true
      times: 1
```

## Recorded Calls

The recorder stores:

| Field | Meaning |
| --- | --- |
| Method | HTTP method. |
| Path | Escaped request path. |
| Request body | Raw request body bytes. |
| Status code | Response status code. |
| Timestamp | Time when the request was recorded. |

Use `assert.api` to check recorded calls. Prefer `*` for project ids so tests do
not depend on `1` or an escaped path.

```yaml
assert:
  api:
    "POST /api/v4/projects/*/releases":
      called: true
      times:
        ge: 1
```

Body checks use the same matcher syntax as other asserts.

```yaml
assert:
  api:
    "POST /api/v4/projects/*/releases":
      body:
        gjson:
          "assets.links.#":
            ge: 1
          "assets.links.0.name":
            have-suffix: ".tar.gz"
```

## Common Test Patterns

### Test A Missing Token Path

Setup:

```yaml
setup:
  api:
    token:
      valid: false
```

Assert that the release was not created:

```yaml
assert:
  api:
    "POST /api/v4/projects/*/releases":
      called: true
      times: 1
```

Also assert the job failed or printed the expected error:

```yaml
assert:
  job:
    release:
      exit-status: 1
      stderr:
        - "401 Unauthorized"
```

### Test A Read-only Token

Setup:

```yaml
setup:
  api:
    token:
      valid: true
      scopes:
        - "read_api"
```

Assert that a write was attempted:

```yaml
assert:
  api:
    "POST /api/v4/projects/*/releases":
      called: true
```

Then assert the job handled the `403 Forbidden` response.

### Test Seeded Data

Setup:

```yaml
setup:
  api:
    seed:
      releases:
        - tag_name: "v1.0.0"
          name: "Existing release"
```

Assert that the job looked up the release:

```yaml
assert:
  api:
    "GET /api/v4/projects/*/releases/*":
      called: true
```

### Test No Destructive Call

```yaml
assert:
  api:
    "DELETE /api/v4/projects/*/releases/*":
      called: false
```

## Limits

The mock API is made for component tests. It gives stable, local behavior for
common GitLab API use. It does not validate every GitLab field and it does not
try to match all GitLab server rules.

Add new HTTP behavior in `internal/mockserver`. Keep request recording there so
`assert.api` can see the calls.
