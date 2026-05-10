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

Use `tag` for tag pipelines.

```yaml
setup:
  tag: "v1.2.0"
```

Do not set `branch` and `tag` together.

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
      default_branch: "main"
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
