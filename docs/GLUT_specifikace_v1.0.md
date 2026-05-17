# GLUT — GitLab Unit Tests: Specification

> **Status:** v1.0 — design complete, ready for implementation
> **Author:** Martin Vidensky
> **Date:** 2026-04-25
> **Changes from v0.2:** We decided on key open questions. These are: format of `setup:` section, mock binaries, scope of mock GitLab API, asserter extension, debugging, parallel run, and vendor dependencies. We rewrote this document for the implementation phase. We removed old context. We use general words now instead of specific components. We finished all open points.

---

## 1. Introduction

### 1.1 Motivation

GitLab CI components are pipeline templates. You can use them again and again. They combine YAML config and shell logic. You can share them via GitLab Catalog. You import them to project pipelines with the `include:` word.

Testing CI components is a hard problem. A component is not a simple function. It is a part of a pipeline definition. It only runs in a GitLab Runner. Normal unit testing does not work here because:

- The component needs a running CI runner, not just YAML reading.
- The logic is in YAML rules, shell scripts, and outside tools.
- The behavior depends on CI variables, git status, and outside APIs.
- Manual testing in a real GitLab pipeline is slow, expensive, and not stable.

Tools today only solve parts of the problem. **gitlab-ci-local** can run a pipeline locally. But it has no isolation between tests. It has no mock GitLab API. It has no structured asserts. Some projects make their own bash runners, but you cannot use them in other projects.

### 1.2 What GLUT gives you

**GLUT** (GitLab Unit Tests) is a test runner for GitLab CI components. It gives you:

- **Isolated test environment** — every test has its own workspace, fake git remote, mock GitLab API, and mock binaries.
- **Declarative test format** — one YAML file = one test. It mixes normal GitLab CI syntax with a GLUT assert section.
- **Structured asserts** — Goss-inspired resource-centric matchers for jobs, artifacts, git status, mock API calls, and mock binaries.
- **CLI tool** — distributed as a single binary or Docker image. You can put it into project pipelines.
- **Mock GitLab API** — embedded HTTP server. It covers normal endpoints (releases, merge requests, tags, etc.) without needing a real GitLab instance.

### 1.3 What GLUT is not

To stop confusion:

- **GLUT is not a new GitLab Runner.** It uses `gitlab-ci-local` as an outside dependency to run pipelines.
- **GLUT is not an integration testing tool for full pipelines.** It is for unit testing *single components* in isolation.
- **GLUT is not a GitLab API simulator.** The mock API only covers some endpoints needed for CI components. It does not have full GitLab features.
- **GLUT does not support Windows natively.** The tool is for POSIX systems (Linux, macOS). Windows users can use a Docker image with WSL2. This is an unofficial workaround.

---

## 2. Vision and Goals

### 2.1 Project Goals

GLUT is an open-source project. It is on GitHub. You can get it via standard channels (binary, Docker image, package managers).

Key features:

- **Standalone** — a separate tool, not a dependency of a specific repository.
- **Universal** — it works for any GitLab CI components without changing the runner.
- **Declarative** — tests describe *what* to check, not *how* to check it.
- **Diagnostic** — it catches errors early and reports them clearly.
- **Extensible** — you can easily add new mock API endpoints. You do not need to change the core.

### 2.2 Technology Stack

| Layer | Technology | Reason |
|---|---|---|
| Runner / CLI | **Go** | Single static binary, easy to share, embedded HTTP server |
| Mock GitLab API | **Go** (embedded goroutine) | Shared memory with runner — recorder pattern for `api:` asserts without IPC |
| CI executor | **gitlab-ci-local** (Node.js, outside dependency) | Hard to replace, too complex to write again |
| Docker image | Go binary + Node.js + gitlab-ci-local | Main way to share. It has all dependencies. |

`gitlab-ci-local` is still an outside dependency. For native install, the user must install it. The Docker image has everything you need.

### 2.3 Platform Support

| Platform | Method | Note |
|---|---|---|
| Linux | Native binary, Docker image | Full support |
| macOS | Native binary, Docker image | Full support |
| Windows | ❌ not supported natively | gitlab-ci-local does not work natively without WSL2. Docker image with WSL2 is an unofficial workaround. |

GLUT is for POSIX systems. All implementation (workspace isolation, mock binaries, shell scripts) needs a POSIX OS.

---

## 3. Environment Variables Rules

GLUT adds env vars to the test environment using these rules:

- **`GLUT_*`** — all env vars for GLUT internal work. The user must not use them for their own values. They would conflict with GLUT.
- **`CI_*`** — env vars that simulate the GitLab CI environment (`CI_COMMIT_BRANCH`, `CI_PROJECT_ID`, `CI_API_V4_URL`, etc.). GLUT follows GitLab rules.
- Everything else is for the user. GLUT does not touch it.

We explain specific env vars in the sections below. Summary table:

| Env var | Purpose | Section |
|---|---|---|
| `GLUT_ORIGIN_REPO` | Path to the bare repo of the fake origin | §6.7 |
| `GLUT_WORKSPACE` | Path to the workspace (`$TMPWORK`) | §6 |
| `GLUT_TEST_NAME` | Name of the current test | §6 |
| `GLUT_MOCK_LOG_DIR` | Path to JSONL logs of mock binaries (internal) | §9 |
| `GLUT_MOCK_BIN_REAL` | Path to real mock binaries (internal) | §9 |
| `GLUT_FORMAT`, `GLUT_REPORT`, `GLUT_TIMEOUT`, `GLUT_VERBOSE`, `GLUT_FAIL_FAST` | CLI overrides | §15 |

---

## 4. Test Format

### 4.1 Basic Idea

**One file = one test = one pipeline.** A test file has two YAML documents. The first document is normal GitLab CI YAML. The second document is GLUT metadata and has one top-level key: `.glut:`. GLUT reads the second document and gives only the first document to `gitlab-ci-local`.

The first document is pure GitLab CI syntax. The user can use any features (include, extends, rules, needs, multiple components). The tested component does not know it is being tested.

### 4.2 Test Organization

Tests are in test suites. You can use any folder structure. There is no link to a specific component. Suites are just logical groups.

```
tests/
├── image-build/
│   ├── branch-strategy-default.yml
│   ├── branch-strategy-mr-skip.yml
│   └── tag-strategy-semver.yml
├── release/
│   ├── live-release-creates-tag.yml
│   └── missing-token-fails.yml
└── manifest-update/
    └── version-update-commits.yml
```

### 4.3 File Structure

Full test files use YAML multi-document syntax. The first document is the
GitLab CI pipeline. The second document starts after `---` and contains `.glut:`.
Later examples often show only the `.glut:` metadata document for brevity.

```yaml
# Normal GitLab CI pipeline — given to gitlab-ci-local without changes
stages: [build]

include:
  - local: templates/image-build/template.yml
    inputs:
      image_name: my-app
      strategy: branch

"build:image":
  before_script:
    - echo "test-specific setup"

---

# GLUT metadata document
.glut:
  name: "build on default branch produces :latest tag"

  setup:
    branch: main
    pipeline_source: push

    mocks:
      binaries:
        image-builder:
          executable: |
            #!/bin/sh
            echo "Building $*"
            exit 0

  assert:
    job:
      "build:image":
        exit-status: 0
        stdout:
          - "Building"

    artifacts:
      "build.env":
        exists: true
        contents:
          - "/IMAGE_TAG=.+/"
```

### 4.4 Key Implementation Rules

These rules are true for every test:

**Workspace isolation.** For each test, GLUT makes a new workspace in a temp folder. After the test, GLUT deletes the workspace (unless you use `--keep-workspace` mode — see §11). Tests can run in any order. They do not depend on each other.

**Snapshot commit of the working tree.** After copying the repo, GLUT commits all unsaved changes as a snapshot. This makes HEAD always clean. It does not matter what the developer's repo state is. Components with `git switch --force-create` and similar commands work correctly.

**CI environment isolation with `env -i`.** GLUT starts `gitlab-ci-local` with an empty environment. It only keeps needed system vars. All CI variables are given explicitly. No env vars from the host leak into the tests.

**Runner-injected variables.** Some variables (`GLUT_ORIGIN_REPO`, `CI_REPOSITORY_URL`, `CI_API_V4_URL`) must be injected by the runner at runtime. You cannot declare them in the test. `gitlab-ci-local` expands `variables:` when reading YAML. So, absolute paths that depend on the runtime workspace cannot be static.

**Mock binaries via PATH.** The folder with mock binaries is added to the start of `PATH`. Real tools on the host are hidden. See details in §8.

**Stage ordering.** `gitlab-ci-local` with `--force-shell-executor` does not force GitLab CI stage ordering for `.pre` and `.post`. If a test needs a specific job order (like setup before the tested job), it must use the `needs:` directive.

**Detection of missing `.post` stage.** If a test has a job in a stage that is not in the `stages:` block, GLUT sees this during linting and warns you. This catches a bad error where the assert job skips silently.

---

## 5. Setup Section — CI Context

`setup:` in the `.glut:` document defines the context for the test. CI variables do not come from the environment or git. They are part of the test definition. This is because they are key triggers for component behavior (rules, workflow rules).

GLUT looks at the declaration and automatically injects all needed CI variables. The user only declares the logical goal of the test. GLUT makes sure the variables match real GitLab behavior for that trigger.

### 5.1 Pipeline Sources

#### `push` (default)

A commit pushed to a branch. The most common trigger.

```yaml
.glut:
  setup:
    branch: main
    # pipeline_source: push  ← default, no need to write
```

GLUT sets:
```
CI_PIPELINE_SOURCE = push
CI_COMMIT_BRANCH = main
CI_COMMIT_REF_NAME = main
CI_COMMIT_REF_SLUG = main
CI_COMMIT_REF_PROTECTED = false
CI_COMMIT_BEFORE_SHA = 0000000000000000000000000000000000000000
```

These must NOT be set: `CI_COMMIT_TAG`, `CI_MERGE_REQUEST_*`

#### `web`

Manual start from the GitLab UI.

```yaml
.glut:
  setup:
    branch: main
    pipeline_source: web
```

Same as `push`, but `CI_PIPELINE_SOURCE = web`.

#### `schedule`

Started by a scheduled pipeline (cron job in GitLab).

```yaml
.glut:
  setup:
    branch: main
    pipeline_source: schedule
    schedule_description: "Nightly build"   # optional
```

#### tag push

Tag push is not a separate `CI_PIPELINE_SOURCE` value. GitLab uses `push`. The presence of `CI_COMMIT_TAG` is the difference.

```yaml
.glut:
  setup:
    tag: "1.2.0"
    # pipeline_source: push  ← automatic for tag:, no need to write
```

GLUT sets:
```
CI_PIPELINE_SOURCE = push
CI_COMMIT_TAG = 1.2.0
CI_COMMIT_REF_NAME = 1.2.0
CI_COMMIT_REF_SLUG = 1-2-0
```

These must NOT be set: `CI_COMMIT_BRANCH` — not available in tag pipelines (GitLab reality).

#### `merge_request_event`

Pipeline started by a merge request event. The most complex trigger.

**Important detail:** `CI_COMMIT_BRANCH` is not available in MR pipelines. This is the same in real GitLab. Components that check `$CI_COMMIT_BRANCH` will get an empty value. GLUT respects this.

```yaml
.glut:
  setup:
    branch: feature/my-feature      # MR source branch
    pipeline_source: merge_request_event
    merge_request:
      title: "feat: add login page"
      target_branch: main
      iid: 1
      draft: false
      labels: "frontend,ready"       # optional
      assignees: "john.doe"          # optional
```

GLUT sets all `CI_MERGE_REQUEST_*` variables (around 25 variables). `CI_COMMIT_BRANCH` is not set.

#### `trigger`

Pipeline started by a trigger token via API.

```yaml
.glut:
  setup:
    branch: main
    pipeline_source: trigger
```

Sets `CI_PIPELINE_TRIGGERED = true`, `CI_TRIGGER_SHORT_TOKEN = glut`.

#### `api`

Pipeline started via GitLab API.

```yaml
.glut:
  setup:
    branch: main
    pipeline_source: api
```

#### `parent_pipeline`

Downstream pipeline started by a parent pipeline via `trigger:` keyword.

```yaml
.glut:
  setup:
    branch: main
    pipeline_source: parent_pipeline
    upstream:
      pipeline_id: 100
      project_id: 1
      job_id: 200
```

Sets `CI_UPSTREAM_*` variables.

#### `chat`

ChatOps trigger (Slack/Teams integration).

```yaml
.glut:
  setup:
    branch: main
    pipeline_source: chat
    chat:
      channel: "#deployments"
      input: "deploy production"
      user_id: "U12345"
```

### 5.2 Key Rules for Derived Variables

| Rule | Reason |
|---|---|
| `CI_COMMIT_BRANCH` is not set for `merge_request_event` | GitLab does not provide it in MR pipelines. Components rely on this. |
| `CI_COMMIT_BRANCH` is not set for tag push | GitLab does not provide it in tag pipelines. |
| `CI_COMMIT_TAG` only for tag push | It is always empty in a branch pipeline. |
| `CI_MERGE_REQUEST_*` only for `merge_request_event` | Not available in other pipeline types. |
| `CI_PIPELINE_TRIGGERED = true` only for `trigger` | Differentiates trigger token from other API triggers. |

### 5.3 Default CI Variables

GLUT automatically sets standard CI variables. The user does not need to repeat boilerplate. Default values table:

| Variable | Default | Source |
|---|---|---|
| `CI` | `true` | fixed |
| `CI_SERVER_URL` | `http://127.0.0.1:{port}` | runner — mock server |
| `CI_API_V4_URL` | `http://127.0.0.1:{port}/api/v4` | runner — mock server |
| `CI_PROJECT_ID` | `1` | fixed |
| `CI_PROJECT_PATH` | `test-group/test-project` | fixed, you can rewrite it via `setup.api.project.path` |
| `CI_PROJECT_NAME` | `test-project` | fixed |
| `CI_PROJECT_NAMESPACE` | `test-group` | fixed |
| `CI_COMMIT_SHA` | `git rev-parse HEAD` | real SHA from workspace |
| `CI_COMMIT_SHORT_SHA` | `git rev-parse --short HEAD` | real short SHA |
| `CI_COMMIT_BRANCH` | from `setup.branch` | derived |
| `CI_COMMIT_REF_NAME` | from `setup.branch` or `setup.tag` | derived |
| `CI_COMMIT_REF_SLUG` | slugified `setup.branch` | derived |
| `CI_DEFAULT_BRANCH` | `main` | fixed, you can rewrite it |
| `CI_PIPELINE_SOURCE` | `push` | you can rewrite it via `setup.pipeline_source` |
| `CI_PIPELINE_ID` | `1` | fixed |
| `CI_JOB_TOKEN` | `mock-job-token` | fixed |
| `CI_REGISTRY` | `registry.example.com` | fixed, you can rewrite it |
| `CI_REGISTRY_IMAGE` | `registry.example.com/test-group/test-project` | fixed, you can rewrite it |
| `GITLAB_USER_NAME` | `Test User` | fixed, you can rewrite it |
| `GITLAB_USER_EMAIL` | `test@example.com` | fixed, you can rewrite it |
| `GITLAB_USER_LOGIN` | `test-user` | fixed, you can rewrite it |

**Overwriting:**
- **`setup:`** — for things defining CI context: trigger, branch, tag, MR metadata.
- **`variables:`** — for everything else. This is a standard GitLab CI mechanism, not a special GLUT mechanism.

```yaml
# setup: = CI context
.glut:
  setup:
    branch: main
    pipeline_source: merge_request_event
    merge_request:
      title: "feat: add feature"

# variables: = everything else
variables:
  GITLAB_USER_NAME: "Alice Example"
  CUSTOM_REGISTRY_URL: "registry.example.com"
```

---

## 6. Setup Section — Git Origin and Workspace

### 6.1 Idea

Components often do git operations — `git push` to origin remote, create branches, read files from repository. For each test, GLUT makes a **fake origin remote** (a bare git repository in a temp folder). It configures the workspace so `origin` points to it.

The component does not know it talks to a fake origin. To the component, it is a normal git remote. The path to the bare repo is injected as `$GLUT_ORIGIN_REPO`.

### 6.2 Origin Remote Configuration

You configure the origin remote in `setup.git.origin`. We use a hybrid approach: declarative `files:` for normal cases, and `commands:` as an escape hatch for complex git operations.

```yaml
.glut:
  setup:
    git:
      user: { name: "test", email: "test@test.com" }   # default author for commits

      origin:
        branch: main                # default branch in origin

        # Declarative part — files are committed as one initial commit
        files:
          "config/app.yaml": |
            version: 1.0.0
          "data/items.json": |
            {"items": []}

        # Escape hatch — shell commands running after the initial commit
        commands:
          - git tag v1.0.0 HEAD
          - git checkout -b release/v1
          - |
            echo '{"items": ["legacy"]}' > data/items.json
            git add . && git commit -m "old version" --author='Bob <bob@x.com>'
          - git checkout main
```

### 6.3 setup.git.origin Contract

**Order of operations:**

1. GLUT makes a bare repository in `$TMPWORK/.glut-origin.git`.
2. It makes a temp worktree.
3. It writes files from the `files:` map.
4. It commits them as an initial commit with the author from `git.user`.
5. It runs commands from `commands:` one by one (each as a separate shell command with `bash`).
6. It pushes all changes to the bare repo.
7. It clones the bare repo into the workspace (`$TMPWORK`).
8. The workspace is ready for the pipeline.

**What `commands:` can do:**
- Edit files in the worktree.
- Create commits, branches, tags.
- Call any git operations.
- Call any shell utilities (`yq`, `sed`, `grep`, etc.).

**What `commands:` cannot do:**
- Change the workspace (use `before_script` of the tested job or `.pre` job for this).
- Call mock GitLab API (use `setup.api:` for this).
- Change the global system (sandbox: only worktree is available).

**Sandbox:**
- Commands run with `bash` (not just POSIX `sh`. Bash is always available in target environments).
- The working directory is the temp worktree, not the workspace.
- `$GLUT_ORIGIN_REPO` points to the bare repo (useful for advanced changes).
- Host env vars are not available (`env -i` isolation).

### 6.4 Relation to Tested Job

After the setup phase, the workspace is a normal git repository. `origin` is set to the bare repo. The component in the tested job can:

- `git fetch` / `git pull` from origin (it sees all seed content).
- `git push` to origin (changes go to the bare repo. Asserts will see the result).
- Change the workspace (commits, branches) without changing the origin, until it pushes.

For normal tests, declarative `files:` is enough. You use `commands:` for:
- Multiple commits with different authors in history.
- Creating tags and branches in the initial state.
- Complex seed data (processing via `yq`, `jq`).
- Simulating long git history.

### 6.5 Intentions: 80% in `setup:`, before_script only rarely

Recommendations for writing tests:

- **Mock binaries** → `setup.mocks.binaries` (§8)
- **Initial git state** → `setup.git.origin`
- **Mock API config** → `setup.api`
- **Logic tied to tested job** (specific input for the test) → `before_script` of that job
- **Anything else (rarely)** → `.pre` job in the pipeline part

Goal: 80% of test logic in `setup:`, 20% in `before_script`. The `.pre` job is only for very exotic cases.

---

## 7. Mock GitLab API

### 7.1 Idea

Components call the GitLab API for tasks. They create releases, open MRs, or check tokens. The Mock GitLab API server runs as an embedded goroutine in GLUT. It is available on a random localhost port.

```go
listener, _ := net.Listen("tcp", "127.0.0.1:0")
port := listener.Addr().(*net.TCPAddr).Port
// → this is injected as CI_API_V4_URL=http://127.0.0.1:{port}/api/v4
```

The component does not know it talks to a mock. It gets `CI_API_V4_URL` pointing to localhost. A recorder pattern inside the goroutine saves all calls for asserts.

### 7.2 Configuration in the Test

The user only configures things when the default is not enough:

```yaml
.glut:
  setup:
    api:
      token:
        valid: true                 # default
        # Options:
        # valid: false              → API returns 401 for all auth calls
        # expires_at: "2026-01-01T00:00:00Z"  → token expires soon
        # scopes: ["read_api"]      → token can read API but cannot write API
        # scopes: ["write_repository"] → token cannot authenticate API calls

      project:
        default_branch: "main"      # default
        path: "test-group/test-project"

      # Prepared state before the test (optional)
      seed:
        releases:
          - tag_name: "v1.0.0"
            name: "Release 1.0.0"
        merge_requests:
          - iid: 1
            title: "Old MR"
            state: "merged"
        labels:
          - name: "bug"
            color: "#FF0000"
```

For everything else, the mock server returns sensible defaults.

### 7.3 Endpoint Coverage

GLUT covers endpoints on three levels.

#### Tier 1 — Dedicated Handlers

These are endpoints with special behavior or structure. Generic CRUD cannot do this. GLUT guarantees these match the real GitLab API exactly.

| Endpoint | Purpose |
|---|---|
| `GET /api/v4/personal_access_tokens/self` | Auth check, returns caller data (token validity, scopes) |
| `GET /api/v4/projects/:id` | Single resource without `:id` in the path (`:id` is the project itself) |
| `GET /api/v4/version` | GitLab version info |
| `POST /api/v4/projects/:id/repository/commits` | Special payload (`actions` array) |
| `POST /api/v4/projects/:id/merge_requests/:iid/notes` | Sub-resource with its own logic |
| `POST /api/v4/projects/:id/merge_requests/:iid/approve` | Action endpoint, not CRUD |

#### Tier 2 — Generic CRUD for Resources

These resources use a generic CRUD handler over an in-memory store. Standard HTTP methods: `GET` (list/get), `POST` (create), `PUT` (update), `DELETE` (delete).

The state persists per test (lifecycle = one test).

| Resource | Endpoint Base |
|---|---|
| `releases` | `/api/v4/projects/:id/releases` |
| `merge_requests` | `/api/v4/projects/:id/merge_requests` |
| `repository/tags` | `/api/v4/projects/:id/repository/tags` |
| `repository/branches` | `/api/v4/projects/:id/repository/branches` |
| `labels` | `/api/v4/projects/:id/labels` |
| `milestones` | `/api/v4/projects/:id/milestones` |
| `issues` | `/api/v4/projects/:id/issues` |
| `hooks` | `/api/v4/projects/:id/hooks` |
| `variables` | `/api/v4/projects/:id/variables` |
| `deployments` | `/api/v4/projects/:id/deployments` |
| `environments` | `/api/v4/projects/:id/environments` |
| `pipelines` | `/api/v4/projects/:id/pipelines` |

Every resource has a sensible default body (a typical GitLab response). For POST, it merges: the fields provided by the user **overwrite** the defaults. It returns the complete object. For GET, it returns what was saved.

#### Tier 3 — 404 Catch-All

Endpoints not in Tier 1 or 2 return a standard 404 (just like real GitLab for a missing resource). In `--verbose` mode, GLUT logs all calls that got 404. The test author can easily see what is missing.

You can explicitly assert this:

```yaml
.glut:
  assert:
    api:
      "POST /api/v4/projects/*/some_unsupported":
        called: false
```

### 7.4 Expansion Workflow

If a component calls an endpoint outside Tier 1 and 2, a contributor can add it:

**Resource in Tier 2 (generic CRUD):**
1. Add a line to the `resources` registry.
2. Define the default body object.
3. Add a test.

This is ~30 lines of code and ~50 lines of tests. A simple PR.

**Endpoint outside Tier 2:**
1. Write a dedicated handler in `internal/mockserver/handlers.go`.
2. Add a test.

This is more work, but it is still isolated.

This process is explained in `CONTRIBUTING.md`.

### 7.5 Stateful vs. Stateless Mock

Key design decision: the mock server is **partially stateful**. Some operations need persistence across calls:

```
1. Component calls: GET /releases/v1.2.0 → 404 (does not exist yet)
2. Component calls: POST /releases with tag_name=v1.2.0 → 201
3. Component calls: GET /releases/v1.2.0 → 200 + object (release-cli checks this)
```

**Stateful (in-memory store, lifecycle = one test):**
- All Tier 2 resources (created by POST or seeded).

**Stateless (always same response):**
- Personal access token info.
- Project info.
- Everything else in Tier 1.

---

## 8. Mock Binaries

### 8.1 Idea

Components call external tools (CLI tools, scripts) for normal tasks — image build, package management, deployment automation. In tests, these tools are often missing or not stable. GLUT lets you inject mock versions. The component calls them instead of the real tools.

A mock binary is a simple shell script in `setup.mocks.binaries`. GLUT wraps every mock binary with a **wrapper**. This wrapper automatically logs all calls for asserts.

### 8.2 Configuration

```yaml
.glut:
  setup:
    mocks:
      binaries:
        image-builder:
          executable: |
            #!/bin/sh
            echo "Building image $*"
            exit 0

        release-cli:
          executable: |
            #!/bin/sh
            case "$*" in
              *"--version"*)
                echo "release-cli 22.0.0"
                exit 0
                ;;
              *)
                printf 'NEXT_VERSION=1.2.0\n' > /tmp/release.env
                echo "Published release 1.2.0"
                exit 0
                ;;
            esac
```

Each binary has an `executable:` key. The content is written to a file with execute permissions. The shebang sets the interpreter (`#!/bin/sh`, `#!/bin/bash`, `#!/usr/bin/env python3`, etc.). GLUT does not change the content.

### 8.3 Architecture

GLUT makes two levels of binaries:

```
$TMPWORK/
├── bin/                          # at the start of PATH, gitlab-ci-local sees this
│   ├── image-builder             # symlink → /usr/local/bin/glut
│   └── release-cli               # symlink → /usr/local/bin/glut
├── bin-real/                     # NOT on PATH, only the wrapper knows it
│   ├── image-builder             # real `executable:` content
│   └── release-cli               # real `executable:` content
└── mock-logs/                    # JSONL call logs
    ├── image-builder.jsonl
    └── release-cli.jsonl
```

**Re-entrant `glut` as wrapper.** Every symlink in `$TMPWORK/bin/` points to the `glut` binary. When gitlab-ci-local calls `image-builder`, `argv[0]` is `image-builder` (resolved through symlink). `glut` detects this and starts mock-wrapper mode.

Pseudocode:

```go
func main() {
    if filepath.Base(os.Args[0]) != "glut" {
        // Re-entrant mode: act as mock wrapper
        mockwrapper.Run()
        return
    }

    // Normal CLI mode
    cmd.Execute()
}
```

Benefits:
- No second binary to distribute.
- Go has `encoding/json` — perfect JSON escaping for free.
- Speed: Go startup is ~5-10ms. Shell logging is ~50-100ms.

### 8.4 Logging Calls

For every call, the wrapper:

1. Records timestamp, PID, PPID, CWD, argv, stdin.
2. Builds a JSON record.
3. Appends it safely to `$GLUT_MOCK_LOG_DIR/<n>.jsonl` (using `flock`).
4. Runs the real binary from `$GLUT_MOCK_BIN_REAL/<n>` with the same arguments and stdin.
5. Passes stdout, stderr, and exit code.

**Atomic append:**
- Primary: `flock -x` on the file descriptor using `syscall.Flock`.
- Fallback: on POSIX systems, `write()` calls under `PIPE_BUF` (4096 B) are atomic without a lock.

Record in JSONL:

```json
{"ts":"2026-04-25T10:00:01.123Z","pid":1234,"ppid":1230,"cwd":"/builds/test","name":"release-cli","args":["semantic-release","--ci"],"stdin":""}
{"ts":"2026-04-25T10:00:02.456Z","pid":1235,"ppid":1230,"cwd":"/builds/test","name":"release-cli","args":["--version"],"stdin":""}
```

**Stdin handling:** The wrapper detects stdin pipe (`stat.Mode() & os.ModeCharDevice == 0`). It reads all content, logs it to JSONL, and passes it to the real binary. In v1, there is no truncation. The user sees the full content. Limit: if stdin is more than a few MB, the log fills up. But in reality, mock binaries do not get such large stdin.

**Logging failure does not fail the test.** If writing the log fails (disk full, lock problem, etc.), the wrapper logs an error to stderr. Then it continues running the real binary normally.

### 8.5 Asserts for Mock Binaries

```yaml
.glut:
  assert:
    binary:
      release-cli:
        called: true
        times: 2
        calls:                          # array — i-th element = i-th call
          - args:
              contain-element: "semantic-release"
            cwd:
              have-suffix: "/builds"
          - args:
              equal: ["--version"]
        never-called-with:              # no call met this matcher
          args:
            contain-element: "--dry-run"

      image-builder:
        called: false                   # must not be called at all
```

**Attributes:**

| Attribute | Type | Description |
|---|---|---|
| `called` | bool | `true` = called at least once; `false` = must not be called |
| `times` | int or matcher | Exact or conditional number of calls |
| `calls` | array | I-th element = i-th call. Each element is an object with `args`, `cwd`, `stdin` attributes. |
| `never-called-with` | object | No call can match this matcher |

---

## 9. Assert Syntax

The syntax is based on the [Goss](https://github.com/goss-org/goss) project — resource-centric YAML, extended with CI-specific resource types.

### 9.1 Matchers — Overview

Matchers are a way to set the expected value. GLUT supports three levels:

**Direct value (default matcher)** — behavior depends on the type:
- String → equality check
- Integer → equality check
- Bool → equality check
- Array → subset match (expected list must be in the real list)
- io.Reader (stdout, file contents) → pattern matching line by line

**Advanced matchers** — objects with the key as the matcher type (see §9.3)
**Logical matchers** — `and`, `or`, `not` for combining (see §9.4)

### 9.2 Pattern Matching for Text Outputs (io.Reader)

Used for `stdout`, `stderr`, `contents` of files and artifacts. Every pattern is checked line by line:

| Pattern | Behavior |
|---|---|
| `"foo"` | At least one line has the substring `foo` |
| `"!foo"` | No line has the substring `foo` |
| `"/[Rr]egex/"` | At least one line matches the regular expression |
| `"!/[Rr]egex/"` | No line matches the regular expression |
| `"\\!foo"` | Escape — at least one line has the literal text `!foo` |

> **Warning:** Regex uses the Go regexp engine. Special characters like `\s`, `\d` must be escaped with a double backslash: `"\\d+"`.

```yaml
stdout:
  - "Building image"               # has substring
  - "!/^FATAL:/"                   # no line starts with FATAL:
  - "/version \\d+\\.\\d+/"        # matches version regex
  - "!unexpected output"           # must not have this
```

### 9.3 Advanced Matchers

#### String Matchers

| Matcher | Description | Example |
|---|---|---|
| `have-prefix: str` | Starts with string | `have-prefix: "v"` |
| `have-suffix: str` | Ends with string | `have-suffix: ".0"` |
| `match-regexp: pattern` | Matches regular expression | `match-regexp: "\\d{3}"` |
| `contain-substring: str` | Contains substring | `contain-substring: "error"` |

#### Numeric Matchers

| Matcher | Description | Example |
|---|---|---|
| `gt: n` | Greater than | `gt: 0` |
| `ge: n` | Greater or equal | `ge: 1` |
| `lt: n` | Less than | `lt: 100` |
| `le: n` | Less or equal | `le: 10` |

#### Array Matchers

| Matcher | Description |
|---|---|
| `contain-element: matcher` | Array has at least one element matching the matcher |
| `contain-elements: [m1, m2]` | Array has all listed elements (superset) |
| `consist-of: [m1, m2]` | Array has exactly these elements, order does not matter |
| `equal: [v1, v2]` | Array is exactly equal, order matters |
| `have-len: n` | Array has exactly n elements |

#### Misc Matchers

| Matcher | Description |
|---|---|
| `have-len: n` | Length of array/string/map is n |
| `have-key: "k"` | Map has the key k |
| `equal: v` | Exact equality (overrides default matcher) |
| `semver-constraint: c` | Version meets semver constraint |

#### gjson Matcher

Allows extracting values from JSON content using [gjson](https://gjson.dev/) path syntax and asserting them. Can be used in `artifacts.contents` and `api.body`.

```yaml
artifacts:
  "release-info.json":
    contents:
      gjson:
        tag_name:
          match-regexp: "v\\d+\\.\\d+\\.\\d+"
        "assets.count":
          gt: 0
        "author.name":
          contain-substring: "bot"
```

### 9.4 Logical Matchers

| Matcher | Description |
|---|---|
| `and: [m1, m2, ...]` | All matchers must pass |
| `or: [m1, m2, ...]` | At least one matcher must pass |
| `not: matcher` | Matcher must not pass |

```yaml
job:
  "build:image":
    exit-status:
      and:
        - not: 2
        - or:
            - 0
            - 1
```

### 9.5 Resource: `job`

Asserts the output and status of a CI job after the pipeline finishes.

```yaml
assert:
  job:
    "build:image":
      exit-status: 0
      present: true
      stdout:
        - "Building image"
        - "/tag: [a-z0-9]+/"
        - "!FATAL"
      stderr:
        - "!/^Error:/"
```

| Attribute | Type | Description |
|---|---|---|
| `exit-status` | int or matcher | Expected exit code of the job |
| `present` | bool | `true` = job must be in the pipeline; `false` = job must not exist |
| `stdout` | patterns or matcher | Pattern matching on job stdout |
| `stderr` | patterns or matcher | Pattern matching on job stderr |

`present: false` is used for skip scenarios. It checks via `gitlab-ci-local --list` that the job is not in the pipeline.

### 9.6 Resource: `artifacts`

Asserts files created by jobs. Paths are relative to the workspace root.

```yaml
assert:
  artifacts:
    "build.env":
      exists: true
      contents:
        - "/IMAGE_TAG=.+/"
        - "!PLACEHOLDER"

    "reports/junit.xml":
      exists: true
      contents:
        - "/<testsuite.*tests=\"[1-9]/"

    "output.json":
      exists: true
      contents:
        gjson:
          status: "success"
          "items.#":
            gt: 0
```

| Attribute | Type | Description |
|---|---|---|
| `exists` | bool | File or directory exists |
| `contents` | patterns or matcher | Pattern matching of content |
| `mode` | string | File permissions, e.g., `"0644"` |
| `size` | int or matcher | File size in bytes |
| `md5`, `sha256` | string | Checksum |
| `filetype` | string | `file`, `directory`, `symlink`, `socket` |

### 9.7 Resource: `git`

Asserts the state of the git repository. There are two matching sub-resources:

- **`git.origin`** — state of the fake origin remote (after push from workspace)
- **`git.workspace`** — state of the workspace after the pipeline finishes

```yaml
assert:
  git:
    origin:                              # what was pushed
      commits: 2
      last-commit:
        author-name: "Alice Example"
        author-email: "alice@example.com"
        message: "/chore: update.*/"
      file:
        "config/app.yaml":
          exists: true
          contents:
            - "version: 2.0.0"

    workspace:                           # local state after running
      branch: "feature/new-version"
      clean: true
      last-commit:
        message: "/feat:.*/"
      file:
        "local-only.tmp":
          exists: false
```

**Attributes for `git.origin` and `git.workspace` (the same):**

| Attribute | Type | Description |
|---|---|---|
| `commits` | int or matcher | Number of commits in history |
| `last-commit` | object | Attributes of the last commit (see below) |
| `file` | path map | Asserts for file contents (same attributes as `artifacts`) |

**`git.workspace` also has:**

| Attribute | Type | Description |
|---|---|---|
| `branch` | string or matcher | Currently checked out branch |
| `clean` | bool | Working tree is clean (no modified files) |

**Attributes for `last-commit`:**

| Attribute | Type | Description |
|---|---|---|
| `author-name` | string or matcher | Commit author name |
| `author-email` | string or matcher | Commit author email |
| `message` | string or matcher | Commit message |
| `sha` | string or matcher | Commit SHA |

**When to use what:**

- `git.origin` — for testing components that push (release flow, GitOps update).
- `git.workspace` — for testing components that do not push (they make a local commit and open an MR, or for debugging).

### 9.8 Resource: `api`

Asserts calls to the mock GitLab API server. The key is `"METHOD /path"`. The path can have `*` as a wildcard for dynamic parts.

```yaml
assert:
  api:
    "POST /api/v4/projects/*/releases":
      called: true
      times: 1
      body:
        # Top-level subset match (like v0.2)
        tag_name: "/[0-9]+\\.[0-9]+\\.[0-9]+/"
        name: "/.+/"

        # Array matchers (new in v1.0)
        assignee_ids:
          have-len: 2
        labels:
          contain-elements: ["frontend", "ready"]

        # gjson for deep structures (new in v1.0)
        gjson:
          "milestone.id":
            equal: 5
          "assets.links.#":
            gt: 0
          "assets.links.0.name":
            equal: "binary"

    "GET /api/v4/personal_access_tokens/self":
      called: true

    "DELETE /api/v4/projects/*/releases/*":
      called: false                         # must not be called at all
```

| Attribute | Type | Description |
|---|---|---|
| `called` | bool | `true` = endpoint was called; `false` = must not be called |
| `times` | int or matcher | Number of calls |
| `body` | map | Subset match on request body, plus optional `gjson:` block for nested fields |

**`body:` accepts a mix of:**
- Top-level keys with direct values or matchers.
- Array matchers for arrays (assignee_ids, labels).
- Optional `gjson:` block for deep structures.

### 9.9 Resource: `binary`

Asserts for mock binaries — see §8.5.

---

## 10. Example Tests

### 10.1 Image Build Component — Branch Strategy Produces :latest Tag

```yaml
# Normal GitLab CI pipeline
stages: [build]

include:
  - local: templates/image-build/template.yml
    inputs:
      image_name: my-app
      strategy: branch

# GLUT section
.glut:
  name: "build on default branch produces :latest tag"

  setup:
    branch: main
    pipeline_source: push

    mocks:
      binaries:
        image-builder:
          executable: |
            #!/bin/sh
            echo "Building image $*"
            exit 0

  assert:
    job:
      "build:image":
        exit-status: 0
        stdout:
          - "Building image"
          - "registry.example.com/my-app:latest"
          - "!ERROR"

    artifacts:
      "build.env":
        exists: true
        contents:
          - "IMAGE_URL=registry.example.com/my-app:latest"

    binary:
      image-builder:
        called: true
        times: 1
```

**What the scenario checks:**
- The component builds the `:latest` image tag for the default branch correctly.
- The mock builder is called exactly once.
- The `build.env` artifact has the correct URL.

### 10.2 Manifest Update Component — Creates Commit with Author Data

```yaml
stages: [deploy]

include:
  - local: templates/manifest-update/template.yml
    inputs:
      target_environment: production
      component_name: backend

variables:
  GITLAB_USER_NAME: "Alice Example"
  GITLAB_USER_EMAIL: "alice@example.com"
  VERSION: "3.0.0"

.glut:
  name: "version update creates commit with correct author"

  setup:
    branch: main
    pipeline_source: web

    git:
      user: { name: "test", email: "test@test.com" }
      origin:
        branch: main
        files:
          "production/versions.yaml": |
            backend: "2.9.0"
            frontend: "1.5.0"

  assert:
    job:
      "deploy:manifest":
        exit-status: 0

    git:
      origin:
        commits: 2                       # initial seed + update
        last-commit:
          author-name: "Alice Example"
          author-email: "alice@example.com"
          message:
            and:
              - have-prefix: "chore:"
              - contain-substring: "backend"
        file:
          "production/versions.yaml":
            contents:
              - "backend: \"3.0.0\""
              - "frontend: \"1.5.0\""    # unchanged
```

**What the scenario checks:**
- The component reads versions from origin. It updates the correct component. It does not overwrite others.
- A commit is created with author data from CI variables.
- A push to origin happened (origin has 2 commits instead of 1).

### 10.3 Release Component — Creates GitLab Release via API

```yaml
stages: [release]

include:
  - local: templates/release-publish/template.yml
    inputs:
      job_when: always

.glut:
  name: "release flow creates GitLab release with semantic version"

  setup:
    branch: main
    pipeline_source: web

    api:
      token:
        valid: true
        scopes: ["api"]

    mocks:
      binaries:
        release-cli:
          executable: |
            #!/bin/sh
            case "$*" in
              *"--version"*)
                echo "release-cli 22.0.0"
                exit 0
                ;;
              *)
                printf 'NEXT_VERSION=1.2.0\nCURRENT_VERSION=1.1.0\n' > /tmp/release.env
                echo "Published release 1.2.0"
                echo "Created tag 1.2.0"
                exit 0
                ;;
            esac

  assert:
    job:
      "release:publish":
        exit-status: 0
        stdout:
          - "/Released version: [0-9]+\\.[0-9]+\\.[0-9]+/"
          - "All done"

    binary:
      release-cli:
        called: true
        times:
          ge: 1                          # at least once (maybe more because of --version check)

    api:
      "GET /api/v4/personal_access_tokens/self":
        called: true

      "POST /api/v4/projects/*/releases":
        called: true
        times: 1
        body:
          tag_name: "/[0-9]+\\.[0-9]+\\.[0-9]+/"
          name: "/.+/"
          gjson:
            "assets.links.#":
              ge: 0

      "POST /api/v4/projects/*/repository/tags":
        called: true
```

**What the scenario checks:**
- Token validation passes (scopes are enough).
- Mock release-cli is called and the component processes the output correctly.
- A GitLab release is created via API with the correct tag_name.
- A tag is created in GitLab.

---

## 11. Debugging

When a test fails, GLUT provides three levels of diagnostics.

### 11.1 Default Error Format

Without any flags, every assert failure prints:

```
FAIL  tests/release/live-release.yml
  Test: "release flow creates GitLab release with semantic version"

  ✗ assert.job."release:publish".exit-status
      expected: 0
      actual:   1

  ✗ assert.api."POST /api/v4/projects/*/releases".called
      expected: true (any number of times)
      actual:   not called

  ✗ assert.git.origin.commits
      expected: 2
      actual:   1

  Stdout of "release:publish":
  ────────────────────────────────────────────
  release-cli 22.0.0
  ERR: Cannot find module 'release-cli/lib/plugins/normalize'
  ────────────────────────────────────────────

  Hint: run with --debug for full job logs and mock call history
  Hint: run with --keep-workspace to preserve $TMPWORK for inspection
```

Key elements:
- **Specific path to the assert** — the user knows right away which assertion to fix.
- **Expected vs. actual** always shown.
- **Stdout of the failing job** automatically (shortened, last 50 lines).
- **Hints for other flags.**

### 11.2 `--debug` Flag

Turns off output trimming. Shows everything:

```bash
glut run --debug ./tests/release/live-release.yml
```

What `--debug` adds:
- Full stdout/stderr of **all jobs** (not just the failed one).
- Full JSONL log of mock binaries (when and with what they were called).
- Full log of mock API server calls (request + response).
- State of workspace and origin (`git log --oneline`).
- Timing of every phase (setup, gitlab-ci-local, asserts).

The output can be long, so this is an opt-in flag.

### 11.3 `--keep-workspace` Flag

```bash
glut run --keep-workspace ./tests/release/live-release.yml
# ... test fails ...
# Test workspace preserved at: /tmp/glut-abc123
# To inspect:
#   cd /tmp/glut-abc123
#   ls -la bin/                          # mock binaries (symlinks)
#   ls -la mock-logs/                    # call logs
#   git log --all                        # workspace history
#   git --git-dir=.glut-origin.git log --all   # origin history
```

After the test finishes, `$TMPWORK` is kept. The user can look at everything. The default behavior is to delete it after the test.

**Bonus:** GLUT automatically keeps the workspace for the **last N failed tests** (configurable, default = 3). This gives you debuggability for CI runs too. You don't have to run it again.

### 11.4 `--debug-pause` Flag

For interactive debugging:

```bash
glut run --debug-pause=before-asserts ./tests/release/live-release.yml
```

Pause points:
- `before-pipeline` — after setup, before running gitlab-ci-local.
- `after-pipeline` — after the pipeline finishes, before asserts (synonym `before-asserts`).
- `on-fail` — pause only if an assertion fails.

When paused, GLUT prints the path to the workspace and waits for Enter before continuing (or cleaning up).

---

## 12. Time and Performance

### 12.1 Time Budget for One Test

| Phase | Estimate | Bottleneck? |
|---|---|---|
| `mktemp -d` + `rsync -a` repo | 100-500ms | No |
| `git add -A && git commit` (snapshot) | 50-200ms | No |
| Start mock server (Go goroutine) | 5-20ms | No |
| Setup mock binaries (symlinks + bin-real) | 10-50ms | No |
| **`gitlab-ci-local` cold start** | **1-3 seconds** | **Yes** |
| The job itself (per job) | 100ms - 2s | Depends on the component |
| Asserts | 50-200ms | No |
| Cleanup | 20-100ms | No |
| **Total per test (1 job)** | **~2-5 seconds** | |

**Main bottleneck:** `gitlab-ci-local` cold start. Every test starts it again (because of `env -i` isolation). So ~50% of the time goes to Node.js startup and YAML parsing.

### 12.2 What This Means in Practice

| Number of Tests | Sequential Time |
|---|---|
| 10 | ~30 seconds |
| 50 | ~3 minutes |
| 100 | ~5-8 minutes |
| 500 | ~30-50 minutes |

For a repository with 20-50 tests, this is OK for the dev loop. For a project that grows to 200+ tests, this starts to be a limit.

### 12.3 Recommendations for Large Test Suites

In v1 (no parallel execution):

- **Selective running via `-k`** — `glut run -k "release"` to run only relevant tests.
- **Fail-fast via `-x`** — stop after the first failure during the dev loop.
- **CI run of full suite** — instead of running everything locally.

**Parallel execution is in the backlog for v2** — see §17.

---

## 13. Dependencies

### 13.1 External Dependencies

| Dependency | Use | Pinning |
|---|---|---|
| `gitlab-ci-local` (Node.js) | CI executor, external binary | Pinned in Dockerfile during image build |
| `git` CLI | Git operations in workspace | POSIX standard |
| `bash` | Shell for `setup.commands` and mock binaries | POSIX common |
| `flock` | Atomic append to JSONL logs | POSIX common, fallback to PIPE_BUF |
| `rsync` | Workspace isolation | POSIX common, fallback to `cp -r` |

### 13.2 Pinning Strategy

**Docker image:** The GLUT release artifact contains `gitlab-ci-local` in a specific version pinned in the Dockerfile. When building a new GLUT version, you decide which GCL version to use.

**Native installation:** The user downloads a released GLUT binary. The user installs `gitlab-ci-local` themselves only for native runs. The recommended version is documented in README. GLUT does not do a runtime version check. Testing against the correct version is the user's responsibility.

### 13.3 Other

System requirements are described in the README. Docker is the preferred user install path because it includes runtime dependencies. The spec does not mention all deployment details.

---

## 14. Versioning

### 14.1 Semver

GLUT uses [Semantic Versioning](https://semver.org/) — `MAJOR.MINOR.PATCH`.

| Change | Version |
|---|---|
| Backwards incompatible change of `.glut:` syntax or CLI | MAJOR |
| New feature, new assert resource, new trigger source | MINOR |
| Bug fix, performance improvement, documentation | PATCH |

**Rule:** new features are added as optional keys. The old syntax must work in the new version too. If that is not possible, it is a breaking change and a MAJOR version.

### 14.2 Semantic-release

Versions are generated automatically from commits using [semantic-release](https://github.com/semantic-release/semantic-release). Commit convention: [Conventional Commits](https://www.conventionalcommits.org/).

| Commit Prefix | Effect |
|---|---|
| `fix:` | PATCH |
| `feat:` | MINOR |
| `feat!:` or `BREAKING CHANGE:` in body | MAJOR |
| `chore:`, `docs:`, `test:`, `refactor:` | no version |

Every push to `main` with a relevant commit automatically:
1. Determines the new version.
2. Generates a CHANGELOG.
3. Creates a GitHub Release with a tag.
4. Runs GoReleaser — binaries + Docker image.

---

## 15. CLI Interface

### 15.1 Basic Structure

```
glut <command> [flags] [path...]
```

`path` can be a folder (runs all `*.yml` files recursively), a specific file, or multiple paths at once. Without an argument, it searches the current folder.

### 15.2 Commands

#### `glut run` — run tests

```bash
glut run                                    # everything in current folder
glut run ./tests/                           # specific folder
glut run ./tests/release/live.yml           # one test
glut run ./tests/ ./other/tests/            # multiple paths at once
```

| Flag | Shortcut | Description |
|---|---|---|
| `--run <pattern>` | `-k` | Filter by test name — substring or regex |
| `--fail-fast` | `-x` | Stop after the first failure |
| `--maxfail <n>` | | Stop after n failures |
| `--verbose` | `-v` | More detailed output — shows stdout of every job |
| `--quiet` | `-q` | Only a summary at the end, no progress output |
| `--format <fmt>` | | Console format: `pretty` (default), `dots`, `json` |
| `--report <fmt:path>` | | Writes a report to a file, repeatable |
| `--timeout <duration>` | | Timeout for one test, default: `10m` |
| `--debug` | | Full logs of all jobs + mock call history (see §11.2) |
| `--keep-workspace` | | Keeps `$TMPWORK` after the test (see §11.3) |
| `--debug-pause <point>` | | Pause at a given point in the test (see §11.4) |

```bash
# Examples
glut run -k "release"                                      # name filter
glut run -x ./tests/                                       # fail-fast
glut run --report=junit:report.xml ./tests/                # JUnit for GitLab CI
glut run --report=junit:report.xml --report=tap:report.tap # multiple formats
glut run -v --debug ./tests/release/live.yml               # debugging
```

#### `glut list` — list tests without running

```bash
glut list                   # all tests in current folder
glut list ./tests/release/  # tests in a specific suite
```

#### `glut lint` — static analysis of tests

Checks YAML syntax and the `.glut:` metadata document:
- Missing `.post` in `stages:` when an assert job exists.
- A job in `assert:` that does not exist in the pipeline part.
- Empty `assert:` section.
- Unknown keys in the `.glut:` metadata document.

```bash
glut lint ./tests/
glut lint ./tests/live.yml
```

#### `glut doctor` — authoring hints and coverage for AI tools

`doctor` runs the same checks as `lint`. It also returns authoring hints and
a job coverage summary. Use it when writing or reviewing tests, or when a
coding assistant needs structured feedback about test quality.

**Hints** catch patterns that are technically valid but weak:
- Most job asserts check only exit status — no artifact, git, API, or binary asserts.
- A tag pipeline test has no release API or binary assert, and no git assert.
- Mock binaries are configured but `assert.binary` is missing. The hint names the binaries.
- Git setup is present but `assert.git` is missing.
- A scheduled pipeline test has no assert that covers scheduled-only behavior.
- An upstream-triggered pipeline test has no job asserts.
- API seed data is configured but `assert.api` is missing.
- The assert block is entirely empty.

**Coverage** shows how many pipeline jobs have at least one `assert.job` entry.

```bash
glut doctor ./tests/
glut doctor -k release ./tests/        # filter by test name substring
glut doctor --format=json ./tests/     # structured output for AI tools
```

| Flag | Shortcut | Description |
|---|---|---|
| `--run <pattern>` | `-k` | Filter by test name substring |
| `--format <fmt>` | | Output format: `text` (default) or `json` |

Text output writes `[HINT]` and `[COVERAGE]` lines to stdout. Parse errors go
to stderr. JSON output groups issues, hints, and coverage by file.

#### `glut version`

```bash
glut version
# glut v1.0.0 (commit: abc1234, built: 2026-04-25)
# gitlab-ci-local: v4.72.0 (vendored in Docker image)
```

### 15.3 Exit Codes

| Code | Meaning |
|---|---|
| `0` | All tests passed |
| `1` | At least one test failed |
| `2` | Runner error — bad syntax, missing dependency, invalid flag |

### 15.4 Report Formats

The `--report` flag takes a value in the format `<format>:<path>`. It can be repeated for multiple outputs. Console output is always present.

| Format | Description |
|---|---|
| `junit` | JUnit XML — integration with GitLab CI test reports |
| `tap` | TAP (Test Anything Protocol) |

### 15.5 Environment Variables

| Variable | Matches Flag |
|---|---|
| `GLUT_FORMAT` | `--format` |
| `GLUT_REPORT` | `--report` (repeatable, comma separated) |
| `GLUT_TIMEOUT` | `--timeout` |
| `GLUT_VERBOSE` | `--verbose` |
| `GLUT_FAIL_FAST` | `--fail-fast` |
| `GLUT_DEBUG` | `--debug` |
| `GLUT_KEEP_WORKSPACE` | `--keep-workspace` |

`GLUT_REPORT` uses a comma as a separator:
```bash
export GLUT_REPORT="junit:report.xml,tap:report.tap"
glut run ./tests/
```

### 15.6 Output Example (`pretty` format)

```
tests/image-build/branch-default.yml     PASS   3.2s
tests/image-build/mr-skip.yml            PASS   2.8s
tests/release/live-release.yml           PASS   5.1s
tests/release/missing-token.yml          FAIL   1.2s

  FAILED  tests/release/missing-token.yml
    ✗ job "release:publish": exit-status expected 1, got 0
    ✗ job "release:publish": stdout must not contain "Published release"

──────────────────────────────────────────────────────
1 failed, 3 passed in 12.3s
```

---

## 16. Project Structure

### 16.1 Repository Directory Structure

```
glut/
├── cmd/
│   └── glut/
│       └── main.go              # entry point — only import and call internal
│
├── internal/
│   ├── runner/                  # test orchestration
│   │   ├── runner.go
│   │   └── runner_test.go
│   ├── workspace/               # workspace isolation
│   │   ├── workspace.go
│   │   └── workspace_test.go
│   ├── parser/                  # parsing GLUT YAML files
│   │   ├── parser.go            # read .glut: metadata document
│   │   └── parser_test.go
│   ├── executor/                # running gitlab-ci-local
│   │   ├── executor.go
│   │   └── executor_test.go
│   ├── asserter/                # evaluating assert sections
│   │   ├── asserter.go
│   │   ├── job.go               # resource: job
│   │   ├── artifacts.go         # resource: artifacts
│   │   ├── git.go               # resource: git (origin + workspace)
│   │   ├── api.go               # resource: api
│   │   ├── binary.go            # resource: binary
│   │   └── asserter_test.go
│   ├── mockserver/              # embedded mock GitLab API server
│   │   ├── server.go            # HTTP server, goroutine, random port
│   │   ├── handlers.go          # Tier 1 dedicated handlers
│   │   ├── crud.go              # Tier 2 generic CRUD
│   │   ├── recorder.go          # records calls for asserts
│   │   └── mockserver_test.go
│   ├── mockwrapper/             # re-entrant glut as mock binary wrapper
│   │   ├── wrapper.go
│   │   └── wrapper_test.go
│   └── reporter/                # output formats
│       ├── console.go
│       ├── junit.go
│       ├── tap.go
│       └── reporter_test.go
│
├── tests/                       # integration tests of GLUT itself
│   ├── passing/
│   │   └── simple-job.yml
│   └── failing/
│       └── bad-assert.yml
│
├── docs/                        # documentation
│   ├── spec.md                  # this specification
│   ├── authoring.md             # guide on writing tests
│   └── mock-api.md              # reference of mock API endpoints
│
├── schema/
│   └── glut.schema.json         # JSON schema for .glut: metadata
│
├── skill/
│   └── SKILL.md                 # AI skill for writing GLUT tests
│
├── .github/
│   ├── workflows/
│   │   ├── ci.yml
│   │   ├── release.yml
│   │   └── docs.yml
│   └── ISSUE_TEMPLATE/
│
├── Dockerfile
├── .goreleaser.yaml             # build matrix: linux/darwin × amd64/arm64
├── .golangci.yml
├── go.mod
├── go.sum
├── Makefile
├── LICENSE
└── README.md
```

### 16.2 Key Structure Decisions

**`cmd/glut/main.go` is a thin layer** — it parses CLI arguments, detects the re-entrant mock-wrapper mode (via `argv[0]`), and delegates. All logic is in `internal/`.

**`internal/` for everything, no `pkg/`** — GLUT does not provide public libraries. `internal/` ensures you cannot import the code from outside. This allows free refactoring.

**Go unit tests right next to source files** — every package has its own `_test.go` file. Standard Go convention.

**`tests/` are integration tests of GLUT** — GLUT tests itself using its own format. Full GLUT scenarios.

**`mockserver/recorder.go`** — shares state with the runner via a goroutine. After the test, the runner passes the recorder to the asserter.

**`mockwrapper/`** — re-entrant logic, where the `glut` binary acts as a mock wrapper. Detected via `filepath.Base(os.Args[0]) != "glut"`.

### 16.3 Build and Release

**Makefile:**

```makefile
make build      # local binary
make test       # go test ./...
make lint       # golangci-lint
make docker     # docker build
make release    # goreleaser release --snapshot --clean (local test)
```

**GoReleaser** — `git tag v1.0.0 && git push --tags` triggers the full release pipeline:
- Binaries for linux/darwin × amd64/arm64.
- Docker image on GHCR.
- GitHub Release with checksums.

**GitHub Actions:**

`ci.yml` — on every push and PR:
- `go test ./...`
- `golangci-lint run`
- `docker build`

`release.yml` — on tag push:
- GoReleaser → binaries + Docker image + GitHub Release.

### 16.4 Docker Image

**Base image:** Ubuntu 24.04 LTS — we skip Alpine because of musl libc (problems with DNS resolving and gitlab-ci-local dependency compatibility).

**Image contents:**
- GLUT binary (Go, statically linked).
- Node.js LTS.
- `gitlab-ci-local` in a pinned version (via `ARG GCL_VERSION` in Dockerfile).
- Docker CLI.

**Docker-in-Docker vs. Socket Sharing:**

`gitlab-ci-local` runs CI jobs in Docker containers — the image needs access to the Docker daemon.

| Approach | Use |
|---|---|
| **Socket sharing** | `docker run -v /var/run/docker.sock:/var/run/docker.sock glut:latest run` |
| **DIND** | `docker run --privileged glut:latest run` |

**Entrypoint:** GLUT binary.

```bash
docker run --rm \
  -v $(pwd)/tests:/tests \
  -v /var/run/docker.sock:/var/run/docker.sock \
  ghcr.io/martinvidensky/glut:latest \
  run /tests/

# With JUnit report
docker run --rm \
  -v $(pwd)/tests:/tests \
  -v $(pwd)/reports:/reports \
  -v /var/run/docker.sock:/var/run/docker.sock \
  ghcr.io/martinvidensky/glut:latest \
  run --report=junit:/reports/report.xml /tests/
```

---

## 17. Backlog — Future Versions

### v2

- **Parallel test execution** — worker pool, opt-in via `--parallel N`. Workspace isolation allows this, but risks of race conditions in gitlab-ci-local must be tested experimentally. A major benefit for large test suites (100+ tests).

- **Programmable mocks for stateful logic** — `starlark:` key in `setup.mocks.binaries`. Embedded Starlark interpreter for mock binaries with sequential/stateful logic (counting calls, lazy responses, conditional behavior). The design is in internal documents, waiting to test performance overhead.

- **Fake artifacts via API** — components downloading artifacts from upstream jobs via `GET /api/v4/projects/:id/jobs/:job_id/artifacts` will get prepared content instead of real GitLab.

- **Snapshot testing** — inspired by Jest/Vitest. The first run saves output as a snapshot. Next runs compare it. Useful for complex artifacts.

- **`since-seed` delta asserts** — asserting *what changed* since the initial state, not just the final state. For complex git scenarios with many commits.

- **Contract tests for mock API** — automatic check that the mock GitLab API returns answers compatible with the real GitLab API. Usually integrated with recordings from a real server.

### v3

- **VS Code extension** — running tests from the editor, inline results, autocomplete with a live runner.

- **Programmable mocks — DSL alternative** — if Starlark is too much, a declarative DSL for mock binaries (cases, when, do).

---

## 18. Documentation

Documentation is written after the API is stable.

### 18.1 README

Starting point on GitHub:
- What GLUT is and what it is for (2-3 sentences).
- Installation — Docker image and release binaries.
- Quickstart — minimal working example.
- System requirements.
- Link to full documentation.

### 18.2 Full Documentation — MkDocs Material

Hosted on GitHub Pages. Generator: [MkDocs](https://www.mkdocs.org/) with [Material theme](https://squidfunk.github.io/mkdocs-material/).

```
docs/
├── index.md
├── getting-started/
│   ├── installation.md
│   └── first-test.md
├── reference/
│   ├── test-format.md
│   ├── assert-syntax.md
│   ├── mock-api.md
│   └── cli.md
└── examples/
    ├── image-build.md
    ├── release.md
    └── manifest-update.md
```

### 18.3 Automatic Deploy

GitHub Actions workflow `docs.yml` on every push to `main` with changes in `docs/` or `mkdocs.yml`:
- `pip install mkdocs-material`
- `mkdocs gh-deploy --force`

---

## 19. JSON Schema

The GLUT YAML format will be fully covered by a JSON schema. The schema has two purposes:

**Lint** — `glut lint` uses the schema internally to validate tests. Errors like unknown keys in the `.glut:` metadata document, wrong value types, or missing required attributes are caught statically before running.

**IDE Support** — the schema will be published on [SchemaStore](https://www.schemastore.org/). Editors download it automatically from there. VS Code, IntelliJ, Neovim, and others will offer:
- Autocomplete for keys in the `.glut:` metadata document.
- Inline validation with error descriptions.
- Hover documentation for every attribute.

Schema location in repository: `schema/glut.schema.json`

---

## 20. AI Skill

There will be a separate skill file (`SKILL.md`) for AI assistants. Purpose: a developer can load the skill into their AI tool (Claude, Cursor, Copilot, etc.). The AI will then be able to write correct GLUT tests without knowing the whole documentation.

### 20.1 Skill Content

- Short description of what GLUT is and how it works.
- Complete format of the `.glut:` metadata document with examples.
- All assert resource types (`job`, `artifacts`, `git`, `api`, `binary`) with examples.
- Pattern matching syntax (`/regex/`, `!negation`).
- Mock API configuration (`token`, available endpoints).
- Mock binaries configuration.
- Common mistakes and how to avoid them.
- Example tests for the most common use cases.

### 20.2 Location

The skill will be part of the repository as `skill/SKILL.md`. The user can load it directly into their AI tool or link to it.

---

*This document is in v1.0-rc1 state. The design is complete, ready for implementation. Feedback and comments are still welcome before finalizing v1.0.*
