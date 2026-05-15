---
name: glut-tests
description: Write, review, and fix GLUT test files for GitLab CI components. Use when an AI assistant needs to create valid GLUT YAML tests, improve existing GLUT tests, choose setup/assert syntax, mock GitLab API calls, mock binaries, or explain why a GLUT test is invalid.
---

# GLUT Test Author

Use this skill to write better GLUT tests. Optimize for valid YAML, realistic CI
context, and useful asserts.

## Workflow

1. Identify the behavior under test: job output, artifact, git change, API call,
   or binary call.
2. Write the GitLab CI pipeline as the first YAML document.
3. Write the `.glut:` metadata as the second YAML document.
4. Put CI context in `setup:`. Do not hard-code runner-injected variables in the
   pipeline.
5. Put checks in `assert:`. Prefer checking the side effect that matters, not
   only `exit-status: 0`.
6. Run or suggest `glut lint` before `glut run`.
7. When GLUT is available locally, prefer structured feedback:
   `glut lint --format=json <file>` for validation and
   `glut doctor --format=json <file>` for validation plus authoring hints.

## Use GLUT Feedback

When a GLUT binary is available, use it instead of guessing.

Use lint for validity:

```bash
glut lint --format=json path/to/test.yml
```

The JSON output has this shape:

```json
{
  "files": [
    {
      "file": "path/to/test.yml",
      "issues": [
        {
          "level": "error",
          "category": "schema",
          "path": ".glut.setup.pipeline_source",
          "message": "glut schema: setup.pipeline_source: must be one of..."
        }
      ]
    }
  ],
  "has_errors": true
}
```

Use `category` to choose the fix:

- `schema`: fix `.glut:` keys, value types, enum values, or matcher shape.
- `semantic`: fix cross-document logic, such as `assert.job` pointing to a
  missing job.
- `parse`: fix YAML syntax, missing files, or invalid file structure.

Use doctor when the test is valid but may be weak:

```bash
glut doctor --format=json path/to/test.yml
```

Doctor returns `issues` plus `hints`. Treat hints as authoring advice, not as
hard failures. Good fixes for hints usually add stronger asserts:

- add `assert.artifacts` for generated files
- add `assert.git` for pushed commits or changed files
- add `assert.api` for GitLab API calls
- add `assert.binary` for mocked tools

If `has_errors` is true, fix `issues` first. Then use `hints` to improve test
quality.

## Required File Shape

A GLUT test has two YAML documents.

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
        stdout:
          - "ok"
```

Rules:

- Keep `.glut:` only in the second document.
- Keep GitLab CI YAML pass-through. Do not rewrite anchors or aliases.
- Make each `assert.job` key match a real job name.
- Do not set `setup.branch` and `setup.tag` together.
- Add `setup.merge_request` when `setup.pipeline_source` is
  `merge_request_event`.

## `setup:` Cheat Sheet

Use `setup:` to define test context.

```yaml
setup:
  branch: "main"
  pipeline_source: "push"
```

Allowed `pipeline_source` values:

- `push`
- `web`
- `merge_request_event`
- `schedule`
- `trigger`
- `api`
- `parent_pipeline`
- `chat`

Merge request context:

```yaml
setup:
  branch: "feature/release"
  pipeline_source: "merge_request_event"
  merge_request:
    title: "Release change"
    target_branch: "main"
    iid: 42
    draft: false
    labels: "release,ready"
    assignees: "dev"
```

Fake origin seed:

```yaml
setup:
  git:
    origin:
      branch: "main"
      files:
        "manifest.yaml": "image: old\n"
```

Git setup fields:

- `git.user.name`
- `git.user.email`
- `git.origin.branch`
- `git.origin.files`
- `git.origin.commands`

`git.origin.commands` accepts either a YAML sequence or any block scalar (`|`,
`>`, `|-`, `>-`). Use `|` to write a multi-line shell script as a single bash
invocation:

```yaml
git:
  origin:
    commands: |
      git tag v1.0.0
      git commit --allow-empty -m "feat: next release"
```

Or use a sequence when each command should be a separate invocation:

```yaml
git:
  origin:
    commands:
      - git tag v1.0.0
      - git commit --allow-empty -m "feat: next release"
```

Mock GitLab API:

```yaml
setup:
  api:
    token:
      valid: true
      scopes:
        - "api"
    project:
      path: "test-group/test-project"
      default_branch: "main"
    seed:
      releases:
        - tag_name: "v1.0.0"
          name: "Old release"
```

`api.token.scopes` accepts either a sequence or a plain string:

```yaml
api:
  token:
    valid: true
    scopes: "api"          # plain string
    # scopes: ["api", "read_repository"]  # or sequence
```

Mock API seed supports:

- `releases`
- `merge_requests`
- `labels`

Common mock API project resources:

- `/releases`
- `/merge_requests`
- `/repository/tags`
- `/repository/branches`
- `/labels`
- `/milestones`
- `/issues`
- `/hooks`
- `/variables`
- `/deployments`
- `/environments`
- `/pipelines`

Special mock API endpoints:

- `POST /api/v4/projects/:id/repository/commits`
- `POST /api/v4/projects/:id/merge_requests/:iid/notes`
- `POST /api/v4/projects/:id/merge_requests/:iid/approve`

Mock binary:

```yaml
setup:
  mocks:
    binaries:
      release-cli:
        executable: |
          #!/bin/sh
          echo "release-cli $*"
```

## `assert:` Cheat Sheet

Use exact values for stable facts. Use matchers for variable output.

Common matchers:

```yaml
equal: "main"
have-prefix: "feat/"
have-suffix: ".json"
contain-substring: "ready"
match-regexp: "^v[0-9]+\\.[0-9]+\\.[0-9]+$"
gt: 0
ge: 1
lt: 10
le: 10
contain-element: "create"
contain-elements:
  - "linux"
  - "amd64"
have-len: 2
have-key: "tag_name"
gjson:
  "assets.links.#":
    ge: 1
and:
  - have-prefix: "v"
  - semver-constraint: ">=1.0.0"
or:
  - "main"
  - "master"
not:
  contain-substring: "dirty"
```

Text pattern lists:

```yaml
stdout:
  - "created release"
  - "/tag: v[0-9]+\\.[0-9]+\\.[0-9]+/"
  - "!/panic|fatal/"
```

## Assert Patterns

Job result:

```yaml
assert:
  job:
    build:
      present: true
      exit-status: 0
      stdout:
        - "build complete"
      stderr:
        - "!/panic|fatal/"
```

Job assert fields:

- `present`
- `exit-status`
- `stdout`
- `stderr`

Artifact content:

```yaml
assert:
  artifacts:
    "dist/manifest.json":
      exists: true
      contents:
        gjson:
          "image.name": "registry.example.com/app"
          "image.tag":
            have-prefix: "v"
```

Artifact assert fields:

- `exists`
- `contents`
- `mode`
- `size`
- `md5`
- `sha256`
- `filetype`

Git side effect:

```yaml
assert:
  git:
    origin:
      commits:
        ge: 2
      last-commit:
        message:
          contain-substring: "update manifest"
      file:
        "manifest.yaml":
          contents:
            contain-substring: "image: new"
```

Git assert fields:

- `commits`
- `last-commit.author-name`
- `last-commit.author-email`
- `last-commit.message`
- `last-commit.sha`
- `file`
- `branch` for workspace
- `clean` for workspace

API call:

```yaml
assert:
  api:
    "POST /api/v4/projects/*/releases":
      called: true
      times: 1
      body:
        tag_name: "v1.2.0"
```

API assert fields:

- `called`
- `times`
- `body`

Binary call:

```yaml
assert:
  binary:
    release-cli:
      called: true
      calls:
        - args:
            contain-elements:
              - "create"
              - "--tag-name"
              - "v1.2.0"
```

Binary assert fields:

- `called`
- `times`
- `calls[].args`
- `calls[].cwd`
- `calls[].stdin`
- `never-called-with`

## Complete Templates

### Image Build

```yaml
stages:
  - build

build-image:
  stage: build
  script:
    - mkdir -p dist
    - echo "registry.example.com/app:${CI_COMMIT_REF_SLUG}" > dist/image.txt
---
.glut:
  name: "image build"
  setup:
    branch: "feature/image"
    pipeline_source: "push"
  assert:
    job:
      build-image:
        exit-status: 0
    artifacts:
      "dist/image.txt":
        exists: true
        contents:
          - "/registry.example.com\\/app:feature-image/"
```

### Manifest Update

```yaml
stages:
  - update

update-manifest:
  stage: update
  script:
    - git config user.name "Test User"
    - git config user.email "test@example.com"
    - sed -i 's/old/new/' manifest.yaml
    - git add manifest.yaml
    - git commit -m "chore: update manifest"
    - git push origin HEAD:main
---
.glut:
  name: "manifest update"
  setup:
    branch: "main"
    git:
      origin:
        branch: "main"
        files:
          "manifest.yaml": "image: old\n"
  assert:
    job:
      update-manifest:
        exit-status: 0
    git:
      origin:
        commits:
          ge: 2
        file:
          "manifest.yaml":
            contents:
              contain-substring: "image: new"
```

### Release With Mock Binary

```yaml
stages:
  - release

release:
  stage: release
  script:
    - release-cli create --tag-name "$CI_COMMIT_TAG" --name "$CI_COMMIT_TAG"
---
.glut:
  name: "release from tag"
  setup:
    tag: "v1.2.0"
    pipeline_source: "push"
    mocks:
      binaries:
        release-cli:
          executable: |
            #!/bin/sh
            echo "release-cli $*"
  assert:
    job:
      release:
        exit-status: 0
    binary:
      release-cli:
        called: true
        calls:
          - args:
              contain-elements:
                - "create"
                - "--tag-name"
                - "v1.2.0"
```

## Docker Executor

By default GLUT runs jobs without Docker (`--shell-executor-no-image`). Scripts
run directly on the host — fast, no image pull, suitable for testing logic that
does not depend on a specific runtime environment.

Set `setup.docker: true` to enable Docker. GLUT will let gitlab-ci-local pull
and run the `image:` defined in each job. Use this when:

- The component under test relies on tools only available in a specific image.
- You want the full realistic runtime as used in production.
- You are writing tests for components that define `image:`.

```yaml
setup:
  branch: "main"
  pipeline_source: "push"
  docker: true
```

Docker tests are slower (image pull on first run) and require a Docker daemon
to be accessible. Omit `docker:` or set `docker: false` for tests that only
need shell commands — this is the default and keeps the suite fast.

**Rule of thumb**: start without Docker to cover logic. Add `docker: true` when
you need to prove the component works in its actual runtime image.

## Review Checklist

- The file has exactly two YAML documents.
- The second document has `.glut:` at the top.
- `.glut.name` is short and specific.
- `setup:` describes the trigger and external state.
- `assert:` checks the important side effect.
- Job names in `assert.job` exist in the pipeline.
- Matchers are used where output is variable.
- Mock API and mock binary calls use asserts, not only log checks.
