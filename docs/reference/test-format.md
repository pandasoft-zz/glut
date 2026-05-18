# Test Format

A GLUT test is one YAML file with two documents.

The first document is normal GitLab CI YAML. GLUT passes it to
`gitlab-ci-local`.

The second document is GLUT metadata. It must have one top-level key:
`.glut:`.

```yaml
stages:
  - test

test-job:
  stage: test
  script:
    - echo "ok"
---
.glut:
  name: "basic test"
  setup:
    branch: "main"
  assert:
    job:
      test-job:
        exit-status: 0
```

## `.glut.name`

`name` is the display name for the test. Use a short name that explains the
case.

```yaml
.glut:
  name: "release from tag"
```

## `setup`

`setup` defines the CI context and prepared state.

### Branch And Tag

Use `branch` for branch pipelines.

```yaml
setup:
  branch: "main"
```

Use `default_branch` to set `CI_DEFAULT_BRANCH`. GLUT auto-detects this from
the source repository's `refs/remotes/origin/HEAD`. Set it explicitly when the
detection gives the wrong result or when you need a specific value for the test.

```yaml
setup:
  branch: "feature/my-thing"
  default_branch: "main"
```

`branch` and `default_branch` are independent. `branch` controls which branch
the pipeline runs on. `default_branch` controls `CI_DEFAULT_BRANCH`. A typical
feature-branch test sets both to different values.

Use `tag` for tag pipelines.

```yaml
setup:
  tag: "v1.2.0"
```

Do not set `branch` and `tag` together.

A tag pipeline often needs a prepared repository state. Use `git.origin` to
seed files into the origin before the pipeline runs:

```yaml
setup:
  tag: "v1.2.3"
  pipeline_source: "push"
  git:
    origin:
      branch: "main"
      files:
        "Dockerfile": "FROM scratch\n"
```

`git.origin.branch` is the branch that was tagged. The pipeline runs as a tag
event while the workspace still contains the correct source files.

### Pipeline Source

`pipeline_source` sets `CI_PIPELINE_SOURCE`.

Allowed values are:

- `push`
- `web`
- `merge_request_event`
- `schedule`
- `trigger`
- `api`
- `parent_pipeline`
- `chat`

```yaml
setup:
  branch: "feature/test"
  pipeline_source: "merge_request_event"
```

When the source is `merge_request_event`, add `merge_request`.

```yaml
setup:
  branch: "feature/test"
  pipeline_source: "merge_request_event"
  merge_request:
    title: "Update release job"
    target_branch: "main"
    iid: 42
    draft: false
    labels: "release,ready"
    assignees: "dev"
```

When the pipeline source is `merge_request_event`, set `git.origin.branch` to
the feature branch so the workspace matches the branch being tested:

```yaml
setup:
  branch: "feature/my-thing"
  pipeline_source: "merge_request_event"
  merge_request:
    target_branch: "main"
    iid: 1
  git:
    origin:
      branch: "feature/my-thing"
      files:
        "Dockerfile": "FROM scratch\n"
```

`git.origin.branch` should match `setup.branch`. This ensures `git checkout`
inside the pipeline finds the expected branch name.

Use `schedule` for scheduled pipelines.

```yaml
setup:
  pipeline_source: "schedule"
  schedule:
    description: "nightly"
```

Use `chat` for chat pipelines.

```yaml
setup:
  pipeline_source: "chat"
  chat:
    channel: "release"
    input: "ship"
    user_id: "100"
```

Use `upstream` for parent or trigger context.

```yaml
setup:
  pipeline_source: "parent_pipeline"
  upstream:
    pipeline_id: 1000
    project_id: 20
    job_id: 300
```

### CI Variables Set by GLUT

GLUT derives all `CI_*` variables from `setup` fields. Never hard-code CI
variable values in scripts or tests.

**Always present:**

| Variable | Value |
| --- | --- |
| `CI` | `"true"` |
| `CI_SERVER_URL` | mock server URL |
| `CI_API_V4_URL` | mock server `/api/v4` URL |
| `CI_PROJECT_ID` | `"1"` |
| `CI_PROJECT_PATH` | `"test-group/test-project"` (or `setup.api.project.path`) |
| `CI_PROJECT_NAME` | last segment of project path |
| `CI_PROJECT_NAMESPACE` | namespace prefix of project path |
| `CI_COMMIT_SHA` | real SHA of workspace HEAD |
| `CI_COMMIT_SHORT_SHA` | short SHA of workspace HEAD |
| `CI_DEFAULT_BRANCH` | `setup.default_branch`, or auto-detected from source repo, or `"main"` |
| `CI_PIPELINE_SOURCE` | from `setup.pipeline_source`, default `"push"` |
| `CI_PIPELINE_ID` | `"1"` |
| `CI_JOB_TOKEN` | `"mock-job-token"` |
| `CI_REGISTRY` | `"registry.example.com"` |
| `CI_REGISTRY_IMAGE` | `"registry.example.com/<CI_PROJECT_PATH>"` |
| `GITLAB_USER_NAME` | `"Test User"` (or `setup.git.user.name`) |
| `GITLAB_USER_EMAIL` | `"test@example.com"` (or `setup.git.user.email`) |
| `GITLAB_USER_LOGIN` | `"test-user"` |
| `CI_REPOSITORY_URL` | `file://` path to the fake origin |

**Branch pipelines** (`push`, `web`, `api`, `trigger`, `schedule`,
`parent_pipeline`, `chat` — any source except `merge_request_event` and tag):

| Variable | Value |
| --- | --- |
| `CI_COMMIT_BRANCH` | `setup.branch` (default `"main"`) |
| `CI_COMMIT_REF_NAME` | same as `CI_COMMIT_BRANCH` |
| `CI_COMMIT_REF_SLUG` | slugified branch name |
| `CI_COMMIT_REF_PROTECTED` | `"false"` |
| `CI_COMMIT_BEFORE_SHA` | `"0000000000000000000000000000000000000000"` |

**Tag pipelines** (`setup.tag` is set):

| Variable | Value |
| --- | --- |
| `CI_COMMIT_TAG` | `setup.tag` |
| `CI_COMMIT_REF_NAME` | `setup.tag` |
| `CI_COMMIT_REF_SLUG` | slugified tag |
| `CI_COMMIT_BRANCH` | **not set** |

**MR pipelines** (`merge_request_event`):

| Variable | Value |
| --- | --- |
| `CI_COMMIT_REF_NAME` | `setup.branch` |
| `CI_COMMIT_REF_SLUG` | slugified `setup.branch` |
| `CI_COMMIT_BRANCH` | **not set** |
| `CI_MERGE_REQUEST_IID` | `setup.merge_request.iid` |
| `CI_MERGE_REQUEST_TITLE` | `setup.merge_request.title` |
| `CI_MERGE_REQUEST_TARGET_BRANCH_NAME` | `setup.merge_request.target_branch` |
| `CI_MERGE_REQUEST_SOURCE_BRANCH_NAME` | `setup.branch` |
| `CI_MERGE_REQUEST_DRAFT` | `setup.merge_request.draft` |
| `CI_MERGE_REQUEST_LABELS` | `setup.merge_request.labels` |
| `CI_MERGE_REQUEST_ASSIGNEES` | `setup.merge_request.assignees` |
| `CI_MERGE_REQUEST_PROJECT_ID` | `"1"` |
| `CI_MERGE_REQUEST_PROJECT_PATH` | same as `CI_PROJECT_PATH` |

**Source-specific extras:**

- `schedule`: `CI_PIPELINE_SCHEDULE=true`, `CI_SCHEDULE_DESCRIPTION` from `setup.schedule.description`
- `trigger`: `CI_PIPELINE_TRIGGERED=true`, `CI_TRIGGER_SHORT_TOKEN=glut`
- `parent_pipeline`: `CI_UPSTREAM_PIPELINE_ID`, `CI_UPSTREAM_PROJECT_ID`, `CI_UPSTREAM_JOB_ID` from `setup.upstream.*`
- `chat`: `CI_CHAT_INPUT`, `CI_CHAT_CHANNEL` from `setup.chat.*`

**`CI_COMMIT_REF_SLUG` derivation:** Non-alphanumeric characters are replaced
with `-`, the result is lowercased, leading and trailing dashes are stripped.
Examples: `feature/my-fix` → `feature-my-fix`, `v1.2.0` → `v1-2-0`.

### Git Setup

`git.user` sets the user used by prepared git commands. `git.origin` prepares
the fake remote repository.

```yaml
setup:
  git:
    user:
      name: "Test User"
      email: "test@example.com"
    origin:
      branch: "main"
      files:
        "manifest.yaml": "image: old\n"
      commands:
        - "git checkout -b release"
        - "printf 'note\n' > note.txt"
        - "git add note.txt"
        - "git commit -m 'add note'"
```

`files` is best for simple seed files. `commands` is for cases that need custom
git history.

Files placed under `git.origin` are committed into the fake remote repository
before the pipeline runs. This gives the workspace a clean git state. Scripts
inside your pipeline can safely run `git add`, `git diff`, `git fetch`, or
`git push` without errors. Use `git.origin` rather than plain pipeline files
whenever your jobs interact with the git repository at runtime.

### Mock GitLab API Setup

`api` configures the mock GitLab API server.

```yaml
setup:
  api:
    token:
      valid: true
      expires_at: "2030-01-01T00:00:00Z"
      scopes:
        - "api"
    project:
      default_branch: "main"   # deprecated: use setup.default_branch
      path: "test-group/test-project"
    seed:
      releases:
        - tag_name: "v1.0.0"
          name: "v1.0.0"
      merge_requests:
        - iid: 7
          title: "Open change"
      labels:
        - name: "ready"
```

### Mock Binary Setup

`mocks.binaries` adds mock tools to `PATH`. GLUT records each call.

```yaml
setup:
  mocks:
    binaries:
      release-cli:
        executable: |
          #!/bin/sh
          echo "release-cli $*"
```

### Docker Executor

By default GLUT uses `--shell-executor-no-image`: jobs that declare `image:` run
in Docker; jobs without `image:` run in the shell. This matches normal
`gitlab-ci-local` behaviour.

Set `docker: true` to enable full Docker mode. GLUT mounts the temporary
workspace into each container so mock binaries and the fake git origin are
reachable from inside Docker, and rewrites `CI_API_V4_URL` to use `glut-mock`
— GLUT's own stable hostname injected via `--extra-host` — so containers can
reach the mock API server regardless of how Docker networking is configured.
`host.docker.internal` is also injected for compatibility, but `glut-mock` is
the address that `CI_API_V4_URL` uses.

```yaml
setup:
  docker: true
```

Set `docker: false` to force all jobs to the shell executor regardless of
`image:` declarations (`--force-shell-executor`). This is useful when you want a
fast shell run of a pipeline that normally targets a specific Docker image, or
when Docker is unavailable.

```yaml
setup:
  docker: false
```

| Value | Behaviour |
| --- | --- |
| omitted | same as `true` — full Docker mode |
| `true` | Docker with volume/extra-host support. `CI_API_V4_URL` uses the `glut-mock` hostname. |
| `false` | all jobs forced to shell, `image:` ignored |

## `assert`

`assert` describes the expected result. It can check jobs, artifacts, git state,
API calls, and mock binary calls.

```yaml
assert:
  job:
    test-job:
      exit-status: 0
  artifacts:
    "dist/result.txt":
      exists: true
  git:
    workspace:
      clean: true
  api:
    "POST /api/v4/projects/*/releases":
      called: true
  binary:
    release-cli:
      called: true
```

See the assert syntax reference for all matcher forms.

## Validation

GLUT validates the `.glut:` document with the JSON schema in
`schema/glut.schema.json`. The schema rejects unknown keys, invalid types, and
invalid enum values. Semantic lint rules also check cross-document errors, such
as an `assert.job` key that does not match a pipeline job.
