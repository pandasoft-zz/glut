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

Use `ref_protected` to set `CI_COMMIT_REF_PROTECTED`. The default is `false`.
Set it to `true` to test jobs that only run on protected branches.

```yaml
setup:
  branch: "main"
  ref_protected: true
```

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
| `CI_SERVER_PROTOCOL` | `"http"` |
| `CI_SERVER_HOST` | mock server host (`127.0.0.1`, or the Docker bridge IP with `docker: true`) |
| `CI_SERVER_PORT` | mock server port |
| `CI_SERVER_FQDN` | mock server `host:port` |
| `CI_API_V4_URL` | mock server `/api/v4` URL |
| `CI_PROJECT_ID` | `"1"` |
| `CI_PROJECT_PATH` | `"test-group/test-project"` (or `setup.api.project.path`) |
| `CI_PROJECT_NAME` | last segment of project path |
| `CI_PROJECT_NAMESPACE` | namespace prefix of project path |
| `CI_PROJECT_PATH_SLUG` | slugified project path (e.g. `test-group-test-project`) |
| `CI_PROJECT_ROOT_NAMESPACE` | first segment of project path |
| `CI_PROJECT_TITLE` | last segment of project path |
| `CI_COMMIT_SHA` | real SHA of workspace HEAD |
| `CI_COMMIT_SHORT_SHA` | short SHA of workspace HEAD |
| `CI_COMMIT_MESSAGE` | full message of workspace HEAD commit |
| `CI_COMMIT_TITLE` | first line of workspace HEAD commit message |
| `CI_COMMIT_DESCRIPTION` | HEAD commit message without the first line |
| `CI_COMMIT_TIMESTAMP` | committer date of workspace HEAD (ISO 8601) |
| `CI_DEFAULT_BRANCH` | `setup.default_branch`, or auto-detected from source repo, or `"main"` |
| `CI_PIPELINE_SOURCE` | from `setup.pipeline_source`, default `"push"` |
| `CI_PIPELINE_ID` | `"1"` |
| `CI_PIPELINE_IID` | `"1"` |
| `CI_JOB_TOKEN` | `"mock-job-token"` |
| `GITLAB_CI` | `"true"` (matches real GitLab; gitlab-ci-local alone would set `"false"`) |
| `CI_DEPENDENCY_PROXY_SERVER` | mock server `host:port` |
| `CI_DEPENDENCY_PROXY_GROUP_IMAGE_PREFIX` | `<host:port>/<root namespace>/dependency_proxy/containers` (same for `_DIRECT_GROUP_`) |
| `CI_DEPENDENCY_PROXY_USER` / `_PASSWORD` | `gitlab-ci-token` / `CI_JOB_TOKEN` |
| `CI_REGISTRY` | `"registry.example.com"` |
| `CI_REGISTRY_IMAGE` | `"registry.example.com/<CI_PROJECT_PATH>"` |
| `GITLAB_USER_NAME` | `setup.pipeline.user.name` → `setup.git.user.name` → `"Test User"` |
| `GITLAB_USER_EMAIL` | `setup.pipeline.user.email` → `setup.git.user.email` → `"test@example.com"` |
| `GITLAB_USER_LOGIN` | `setup.pipeline.user.login` → `"test-user"` |
| `CI_REPOSITORY_URL` | `http://gitlab-ci-token:<CI_JOB_TOKEN>@<CI_SERVER_HOST>:<CI_SERVER_PORT>/<CI_PROJECT_PATH>.git` — served by GLUT's git smart HTTP handler |

**Branch pipelines** (`push`, `web`, `api`, `trigger`, `schedule`,
`parent_pipeline`, `chat` — any source except `merge_request_event` and tag):

| Variable | Value |
| --- | --- |
| `CI_COMMIT_BRANCH` | `setup.branch` (default `"main"`) |
| `CI_COMMIT_REF_NAME` | same as `CI_COMMIT_BRANCH` |
| `CI_COMMIT_REF_SLUG` | slugified branch name |
| `CI_COMMIT_REF_PROTECTED` | `"true"` if `setup.ref_protected: true`, otherwise `"false"` |
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

`git.user` sets the committer identity for workspace git operations. It also
drives `GITLAB_USER_NAME` and `GITLAB_USER_EMAIL` by default. Use
`setup.pipeline.user` when the pipeline trigger user must differ from the git
committer. `git.origin` prepares the fake remote repository.

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

### Pipeline User

`pipeline.user` controls the simulated GitLab user who triggered the pipeline.
It sets `GITLAB_USER_NAME`, `GITLAB_USER_EMAIL`, and `GITLAB_USER_LOGIN`.

In a real GitLab pipeline these variables are injected by the platform based on
the user who pushed or triggered the run. They are not set by the pipeline YAML
`variables:` block.

**Priority chain** (first non-empty value wins):

| Variable | Source 1 | Source 2 | Default |
| --- | --- | --- | --- |
| `GITLAB_USER_NAME` | `setup.pipeline.user.name` | `setup.git.user.name` | `"Test User"` |
| `GITLAB_USER_EMAIL` | `setup.pipeline.user.email` | `setup.git.user.email` | `"test@example.com"` |
| `GITLAB_USER_LOGIN` | `setup.pipeline.user.login` | — | `"test-user"` |

When `setup.pipeline.user` is absent, `GITLAB_USER_NAME` and `GITLAB_USER_EMAIL`
are derived from `setup.git.user`. This keeps the git committer identity and the
pipeline trigger identity consistent by default — you only need `pipeline.user`
when the two identities must differ.

**Example: single identity (git and pipeline user are the same person)**

```yaml
setup:
  git:
    user:
      name: "ci-bot"
      email: "ci-bot@example.com"
# GITLAB_USER_NAME=ci-bot, GITLAB_USER_EMAIL=ci-bot@example.com
# Git commits in the workspace are also attributed to ci-bot.
```

**Example: separate identities**

Use this when the pipeline is triggered by a human but workspace git operations
are performed by a service account.

```yaml
setup:
  git:
    user:
      name: "release-bot"
      email: "release-bot@example.com"
  pipeline:
    user:
      name: "Jan Novak"
      email: "jan.novak@example.com"
      login: "jan-novak"
# GITLAB_USER_NAME=Jan Novak  (who triggered the pipeline)
# Git commits are attributed to release-bot (the bot running the job)
```

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

By default GLUT uses full Docker mode: jobs with `image:` run in Docker
containers; jobs without `image:` run in the shell. Both can reach the mock API
server because `CI_API_V4_URL` uses the bridge IP address — a numeric address
reachable from Docker containers (same bridge network) and from shell jobs
(server binds to `0.0.0.0`). GLUT also injects `glut-mock` and
`host.docker.internal` as `/etc/hosts` aliases via `--extra-host` for
compatibility, but the URL uses the bridge IP directly.

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
| `true` | Docker with volume/extra-host support. `CI_API_V4_URL` uses the bridge IP (reachable from Docker and shell). |
| `false` | all jobs forced to shell, `image:` ignored |

#### Privileged containers

Set `privileged: true` to run all job containers with the Docker `--privileged` flag.
This is required for images that create user namespaces, such as `moby/buildkit:rootless`
used for building container images.

```yaml
setup:
  docker: true
  privileged: true
```

> **Security note:** Privileged containers have full access to the host kernel.
> Use only in isolated test environments.

### Docker image entrypoint

GLUT follows GitLab Runner Docker behavior for image entrypoint.
By default, GLUT does not override image `ENTRYPOINT`.
Some images have an entrypoint that is not compatible with CI script shell mode.
In that case, set the entrypoint in the job config.

```yaml
build-image:
  image:
    name: moby/buildkit:rootless
    entrypoint: [""]
  script:
    - buildctl-daemonless.sh build --frontend dockerfile.v0
```

Use this only when the image entrypoint blocks normal job script execution.

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
