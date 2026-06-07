# Mock API

GLUT starts a mock GitLab API server for each test. Jobs receive
`CI_API_V4_URL` that points to this server. The server records each request so
`assert.api` can check it after the pipeline run.

The mock API is not a full GitLab clone. It supports the endpoints that CI
components commonly need in tests: project data, token data, user data, group
data, common project resources, and a range of special endpoints.

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
| `user` | object | User identity returned by `/user` and `/users/:id`. |
| `group` | object | Group data returned by `/groups/:id`. |

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

### `user`

Configures the user returned by `GET /api/v4/user` and `GET /api/v4/users/:id`.

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `id` | integer | `1` | User ID. |
| `name` | string | `Test User` | Display name. |
| `email` | string | `test@example.com` | Email address. |
| `login` | string | `test-user` | Username. |

```yaml
setup:
  api:
    user:
      id: 42
      name: "Alice"
      email: "alice@example.com"
      login: "alice"
```

### `group`

Configures the group returned by `GET /api/v4/groups/:id`.

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `id` | integer | `1` | Group ID. |
| `path` | string | namespace from project path | Full group path, e.g. `platform/infra`. |
| `name` | string | last component of path | Display name. |

```yaml
setup:
  api:
    group:
      id: 10
      path: "platform/infra"
      name: "Infra Team"
```

### `seed`

`seed` prepares resource data before the job starts.

| Field | Type | Meaning |
| --- | --- | --- |
| `releases` | list | Initial release objects. |
| `merge_requests` | list | Initial merge request objects. |
| `labels` | list | Initial label objects. |
| `pipelines` | list | Initial pipeline objects. |
| `jobs` | list | Initial job objects. |

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
      pipelines:
        - id: 10
          status: "success"
          ref: "main"
      jobs:
        - id: 1
          name: "build"
          stage: "build"
          status: "success"
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

### Global

| Method | Path | Auth | Response |
| --- | --- | --- | --- |
| `GET` | `/api/v4/version` | no | GitLab version object. |
| `GET` | `/api/v4/personal_access_tokens/self` | yes | Token state from `setup.api.token`. |
| `GET` | `/api/v4/user` | yes | Current user from `setup.api.user`. |
| `GET` | `/api/v4/users/:id` | yes | User by ID (same data as `/user`). |
| `GET` | `/api/v4/groups/:id` | yes | Group from `setup.api.group`. |

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
  "expires_at": "2030-01-01T00:00:00Z",
  "user_id": 1,
  "created_at": "2024-01-01T00:00:00.000Z",
  "last_used_at": "2024-01-01T00:00:00.000Z"
}
```

### Current User

```bash
curl \
  -H "PRIVATE-TOKEN: test-token" \
  "$CI_API_V4_URL/user"
```

Example response:

```json
{
  "id": 1,
  "name": "Test User",
  "username": "test-user",
  "email": "test@example.com",
  "state": "active"
}
```

### Project

```bash
curl \
  -H "PRIVATE-TOKEN: test-token" \
  "$CI_API_V4_URL/projects/1"
```

Example response includes extended fields:

```json
{
  "id": 1,
  "path_with_namespace": "test-group/test-project",
  "name": "test-project",
  "description": "",
  "default_branch": "main",
  "visibility": "private",
  "web_url": "https://example.com/test-group/test-project",
  "http_url_to_repo": "https://example.com/test-group/test-project.git",
  "ssh_url_to_repo": "git@example.com:test-group/test-project.git",
  "namespace": {
    "id": 1,
    "name": "test-group",
    "path": "test-group",
    "kind": "group",
    "full_path": "test-group"
  },
  "permissions": {
    "project_access": { "access_level": 40 },
    "group_access": null
  },
  "created_at": "2024-01-01T00:00:00.000Z",
  "star_count": 0,
  "forks_count": 0
}
```

## Standard Project Resources

Standard project resources use common create, read, update, and delete behavior
under `/api/v4/projects/:id`. These endpoints are useful for GitLab objects
that behave like stored records.

All list responses include pagination headers:

| Header | Value |
| --- | --- |
| `X-Total` | Total number of records. |
| `X-Total-Pages` | Always `1` (mock returns all records in one page). |
| `X-Page` | Always `1`. |
| `X-Per-Page` | Always `100`. |

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
| Releases | `/releases` | `tag_name` | `tag_name`, `name`, `description`, `assets` |
| Merge requests | `/merge_requests` | `iid` | `iid`, `title`, `state`, `labels`, `source_branch`, `target_branch`, `author`, `draft` |
| Tags | `/repository/tags` | `name` | `name`, `message`, `commit` |
| Branches | `/repository/branches` | `name` | `name`, `protected`, `merged`, `default`, `commit` |
| Labels | `/labels` | `id` | `id`, `name`, `color` |
| Milestones | `/milestones` | `id` | `id` |
| Issues | `/issues` | `iid` | `iid`, `title`, `state`, `labels`, `author` |
| Hooks | `/hooks` | `id` | `id` |
| Variables | `/variables` | `key` | `key`, `value`, `variable_type`, `protected`, `masked` |
| Deployments | `/deployments` | `id` | `id`, `status`, `ref`, `sha` |
| Environments | `/environments` | `id` | `id`, `name`, `state` |
| Pipelines | `/pipelines` | `id` | `id`, `status`, `ref`, `sha`, `web_url` |
| Jobs | `/jobs` | `id` | `id`, `name`, `stage`, `status`, `ref` |

## Special Project Endpoints

Special project endpoints have custom behavior. They are still recorded for
`assert.api`, but they do more than store and return records.

### Commits

| Method | Path | Behavior |
| --- | --- | --- |
| `GET` | `/api/v4/projects/:id/repository/commits` | Returns empty commit list with pagination headers. |
| `GET` | `/api/v4/projects/:id/repository/commits/:sha` | Returns a mock commit object for the given SHA. |
| `POST` | `/api/v4/projects/:id/repository/commits` | Creates a mock commit response. |
| `GET` | `/api/v4/projects/:id/repository/commits/:sha/merge_requests` | Returns empty list. |
| `GET` | `/api/v4/projects/:id/repository/commits/:sha/statuses` | Returns stored statuses for this SHA (empty until POST). |
| `POST` | `/api/v4/projects/:id/statuses/:sha` | Stores status; subsequent GET returns it. |

#### Create Commit

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

#### Create Commit Status

```bash
curl \
  -X POST \
  -H "PRIVATE-TOKEN: test-token" \
  -H "Content-Type: application/json" \
  -d '{"state":"success","name":"ci/build","description":"Build passed"}' \
  "$CI_API_V4_URL/projects/1/statuses/abc123"
```

Response includes `sha`, `state`, `name`, `target_url`, `description`, and `created_at`.

### Merge Requests

| Method | Path | Behavior |
| --- | --- | --- |
| `GET` | `/api/v4/projects/:id/merge_requests/:iid/notes` | Returns stored notes (empty until POST). |
| `POST` | `/api/v4/projects/:id/merge_requests/:iid/notes` | Stores note; subsequent GET returns it. |
| `GET` | `/api/v4/projects/:id/merge_requests/:iid/approvals` | Returns approval state (not approved by default). |
| `POST` | `/api/v4/projects/:id/merge_requests/:iid/approve` | Returns an approved response. |
| `POST` | `/api/v4/projects/:id/merge_requests/:iid/unapprove` | Returns an unapproved response. |
| `PUT` | `/api/v4/projects/:id/merge_requests/:iid/merge` | Returns a merged MR response. |
| `GET` | `/api/v4/projects/:id/merge_requests/:iid/changes` | Returns empty changes list. |
| `GET` | `/api/v4/projects/:id/merge_requests/:iid/discussions` | Returns empty discussions list. |

#### Add Merge Request Note

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

#### Approve / Unapprove Merge Request

```bash
# Approve
curl -X POST -H "PRIVATE-TOKEN: test-token" \
  "$CI_API_V4_URL/projects/1/merge_requests/7/approve"

# Unapprove
curl -X POST -H "PRIVATE-TOKEN: test-token" \
  "$CI_API_V4_URL/projects/1/merge_requests/7/unapprove"
```

#### Merge a Merge Request

```bash
curl -X PUT -H "PRIVATE-TOKEN: test-token" \
  "$CI_API_V4_URL/projects/1/merge_requests/7/merge"
```

Response includes `state: "merged"` and `merge_commit_sha`.

### Issues

| Method | Path | Behavior |
| --- | --- | --- |
| `GET` | `/api/v4/projects/:id/issues/:iid/notes` | Returns stored notes (empty until POST). |
| `POST` | `/api/v4/projects/:id/issues/:iid/notes` | Stores note; subsequent GET returns it. |

### Pipelines

| Method | Path | Behavior |
| --- | --- | --- |
| `POST` | `/api/v4/projects/:id/pipeline` | Triggers a new pipeline. Returns pending pipeline. |
| `POST` | `/api/v4/projects/:id/pipelines/:id/retry` | Returns pending pipeline. |
| `POST` | `/api/v4/projects/:id/pipelines/:id/cancel` | Returns pending pipeline. |
| `GET` | `/api/v4/projects/:id/pipelines/:id/jobs` | Returns jobs from the store filtered by `pipeline_id`. Jobs without `pipeline_id` are always included. |

#### Trigger Pipeline

```bash
curl \
  -X POST \
  -H "PRIVATE-TOKEN: test-token" \
  -H "Content-Type: application/json" \
  -d '{"ref":"main"}' \
  "$CI_API_V4_URL/projects/1/pipeline"
```

Response includes `id`, `status: "pending"`, `ref`, `sha`, and `created_at`.

## CI Environment Variables

In addition to the mock API server, GLUT injects CI environment variables for
each job. These variables simulate what real GitLab would set.

### Standard Variables

| Variable | Value |
| --- | --- |
| `CI_SERVER_URL` | Mock server URL, e.g. `http://127.0.0.1:<port>` |
| `CI_SERVER_HOST` | `127.0.0.1` |
| `CI_SERVER_NAME` | `GitLab` |
| `CI_SERVER_VERSION` | `16.11.0` |
| `CI_SERVER_REVISION` | `mock` |
| `CI_API_V4_URL` | Mock API URL, e.g. `http://127.0.0.1:<port>/api/v4` |
| `CI_PROJECT_ID` | `1` |
| `CI_PROJECT_PATH` | `test-group/test-project` (or from `setup.api.project.path`) |
| `CI_PROJECT_NAME` | Last component of `CI_PROJECT_PATH` |
| `CI_PROJECT_NAMESPACE` | Everything before the last `/` in `CI_PROJECT_PATH` |
| `CI_PROJECT_URL` | `CI_SERVER_URL/CI_PROJECT_PATH` |
| `CI_PIPELINE_ID` | `1` |
| `CI_PIPELINE_URL` | `CI_PROJECT_URL/-/pipelines/1` |
| `CI_JOB_ID` | `1` |
| `CI_JOB_URL` | `CI_PROJECT_URL/-/jobs/1` |
| `CI_JOB_TOKEN` | `mock-job-token` |
| `CI_DEFAULT_BRANCH` | From `setup.default_branch` or `setup.api.project.default_branch` (default: `main`) |
| `GITLAB_USER_ID` | `1` |
| `GITLAB_USER_NAME` | From `setup.pipeline.user` or `setup.git.user` (default: `Test User`) |
| `GITLAB_USER_EMAIL` | From `setup.pipeline.user` or `setup.git.user` (default: `test@example.com`) |
| `GITLAB_USER_LOGIN` | From `setup.pipeline.user` (default: `test-user`) |

### MR Pipeline Variables

Available when `setup.pipeline_source: merge_request_event`.

| Variable | Source |
| --- | --- |
| `CI_MERGE_REQUEST_IID` | `setup.merge_request.iid` |
| `CI_MERGE_REQUEST_TITLE` | `setup.merge_request.title` |
| `CI_MERGE_REQUEST_DESCRIPTION` | `setup.merge_request.description` |
| `CI_MERGE_REQUEST_LABELS` | `setup.merge_request.labels` |
| `CI_MERGE_REQUEST_ASSIGNEES` | `setup.merge_request.assignees` |
| `CI_MERGE_REQUEST_DRAFT` | `setup.merge_request.draft` |
| `CI_MERGE_REQUEST_SQUASH` | `setup.merge_request.squash` (default: `false`) |
| `CI_MERGE_REQUEST_MILESTONE` | `setup.merge_request.milestone` |
| `CI_MERGE_REQUEST_TARGET_BRANCH_NAME` | `setup.merge_request.target_branch` |
| `CI_MERGE_REQUEST_SOURCE_BRANCH_NAME` | `setup.branch` |
| `CI_MERGE_REQUEST_PROJECT_ID` | `1` |
| `CI_MERGE_REQUEST_PROJECT_PATH` | Same as `CI_PROJECT_PATH` |
| `CI_MERGE_REQUEST_SOURCE_PROJECT_ID` | `1` |
| `CI_MERGE_REQUEST_SOURCE_PROJECT_PATH` | Same as `CI_PROJECT_PATH` |
| `CI_MERGE_REQUEST_SOURCE_PROJECT_URL` | `CI_SERVER_URL/CI_PROJECT_PATH` |
| `CI_MERGE_REQUEST_APPROVED` | `setup.merge_request.approved` (default: `false`) |
| `CI_MERGE_REQUEST_EVENT_TYPE` | `setup.merge_request.event_type` (default: `detached`) |
| `CI_MERGE_REQUEST_DIFF_BASE_SHA` | `setup.merge_request.diff_base_sha` (default: `000…0`) |

Example MR setup:

```yaml
setup:
  pipeline_source: merge_request_event
  branch: feature/my-feature
  merge_request:
    iid: 7
    title: "Add my feature"
    description: "Implements the new X behavior"
    target_branch: main
    labels: "ready,backend"
    squash: true
    milestone: "v2.0"
    approved: true
    event_type: merged_result
    diff_base_sha: "abc123def456"
```

### Tag Pipeline Variables

Available when `setup.tag` is set.

| Variable | Source |
| --- | --- |
| `CI_COMMIT_TAG` | `setup.tag` |
| `CI_COMMIT_TAG_MESSAGE` | `setup.tag_message` (empty if not set) |
| `CI_COMMIT_REF_NAME` | `setup.tag` |

Example tag setup:

```yaml
setup:
  tag: "v1.2.3"
  tag_message: "Release 1.2.3 — stability improvements"
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

### Test Commit Status Reporting

```yaml
setup:
  api:
    token:
      valid: true
      scopes:
        - "api"
assert:
  api:
    "POST /api/v4/projects/*/statuses/*":
      called: true
      body:
        state: "success"
```

### Test Pipeline Trigger

```yaml
assert:
  api:
    "POST /api/v4/projects/*/pipeline":
      called: true
      body:
        ref: "main"
```

### Test Current User Lookup

```yaml
setup:
  api:
    user:
      id: 99
      login: "release-bot"
assert:
  api:
    "GET /api/v4/user":
      called: true
```

## Limits

The mock API is made for component tests. It gives stable, local behavior for
common GitLab API use. It does not validate every GitLab field and it does not
try to match all GitLab server rules.

Known limitations:

- Pagination always returns all records in a single page. `X-Total-Pages` is always `1`.
- List endpoints ignore filtering and sorting query parameters.
- `/api/v4/users/:id` always returns the same configured user regardless of the requested ID.
- `/api/v4/groups/:id` always returns the same configured group regardless of the requested ID.
- Repository file content (`/repository/files/:path`) is not implemented.
- `GET /repository/commits` always returns an empty list — individual commits are only accessible via `GET /repository/commits/:sha`.

Add new HTTP behavior in `internal/mockserver`. Keep request recording there so
`assert.api` can see the calls.
