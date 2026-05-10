# GLUT Test Author Skill

## What GLUT Is

GLUT tests GitLab CI components locally. A test file has normal GitLab CI YAML
first and GLUT metadata second. GLUT runs the pipeline, records side effects,
and checks structured asserts.

## Minimal Test Structure

Use two YAML documents. The first document is passed to `gitlab-ci-local`. The
second document has one top-level `.glut:` key.

```yaml
stages:
  - test

test-job:
  stage: test
  script:
    - echo "ok"
---
.glut:
  name: "minimal"
  setup:
    branch: "main"
  assert:
    job:
      test-job:
        exit-status: 0
        stdout:
          - "ok"
```

## Full `setup:` Reference

`setup:` defines CI context and prepared state.

```yaml
.glut:
  setup:
    branch: "main"
    pipeline_source: "push"
```

Use `tag` instead of `branch` for tag pipelines. Do not set both.

```yaml
.glut:
  setup:
    tag: "v1.2.0"
    pipeline_source: "push"
```

Use `merge_request` with `merge_request_event`.

```yaml
.glut:
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

Other pipeline sources are `web`, `schedule`, `trigger`, `api`,
`parent_pipeline`, and `chat`.

```yaml
.glut:
  setup:
    pipeline_source: "schedule"
    schedule:
      description: "nightly"
```

```yaml
.glut:
  setup:
    pipeline_source: "chat"
    chat:
      channel: "release"
      input: "ship"
      user_id: "100"
```

Use `upstream` for parent or trigger context.

```yaml
.glut:
  setup:
    pipeline_source: "parent_pipeline"
    upstream:
      pipeline_id: 1000
      project_id: 20
      job_id: 300
```

Use `git.origin` to seed the fake origin.

```yaml
.glut:
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

Use `api` to set mock GitLab API state.

```yaml
.glut:
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

Use `mocks.binaries` to replace tools in `PATH`.

```yaml
.glut:
  setup:
    mocks:
      binaries:
        release-cli:
          executable: |
            #!/bin/sh
            echo "mock release"
```

## Full `assert:` Reference

`assert.job` checks job result data.

```yaml
.glut:
  assert:
    job:
      build:
        present: true
        exit-status: 0
        stdout:
          - "built"
        stderr:
          not: "panic"
```

`assert.artifacts` checks files from the workspace.

```yaml
.glut:
  assert:
    artifacts:
      "dist/image.txt":
        exists: true
        contents:
          contain-substring: "registry.example.com/app"
        mode: "-rw-r--r--"
        size:
          gt: 0
        filetype: "file"
```

`assert.git` checks workspace or fake origin git state.

```yaml
.glut:
  assert:
    git:
      workspace:
        branch: "main"
        clean: true
      origin:
        commits:
          ge: 1
        last-commit:
          message:
            have-prefix: "chore:"
        file:
          "manifest.yaml":
            contents:
              contain-substring: "image:"
```

`assert.api` checks recorded mock GitLab API calls.

```yaml
.glut:
  assert:
    api:
      "POST /api/v4/projects/*/releases":
        called: true
        times: 1
        body:
          tag_name: "v1.2.0"
          gjson:
            "assets.links.#":
              ge: 1
```

`assert.binary` checks calls to mock binaries.

```yaml
.glut:
  assert:
    binary:
      release-cli:
        called: true
        times:
          ge: 1
        calls:
          - args:
              contain-element: "create"
            cwd:
              have-suffix: "/builds"
        never-called-with:
          args:
            contain-element: "--dry-run"
```

## Pattern Matching Syntax

For text lists, each string is a pattern.

```yaml
stdout:
  - "plain text must exist"
  - "/image: [a-z0-9._-]+/"
  - "!/fatal|panic/"
```

Use `/.../` for a regular expression. Use `!/.../` to reject a regular
expression. Use `\!text` when the wanted text starts with `!`.

## Advanced Matcher Cheat Sheet

Use matcher objects when exact values are not enough.

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
contain-element: "release"
contain-elements:
  - "linux"
  - "amd64"
consist-of:
  - "a"
  - "b"
have-len: 2
have-key: "tag_name"
semver-constraint: ">=1.2.0"
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

## Common Mistakes

- Do not put `.glut:` in the first YAML document.
- Do not set `branch` and `tag` at the same time.
- Use `merge_request` when `pipeline_source` is `merge_request_event`.
- Make every `assert.job` key match a real pipeline job.
- Add job stages to `stages:` when stages are explicit.
- Use `setup.mocks.binaries` for tool calls that must be recorded.
- Keep GitLab CI YAML pass-through. Do not rewrite anchors or aliases.

## Sample Test: Image Build

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

## Sample Test: Manifest Update

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

## Sample Test: Release

```yaml
stages:
  - release

release:
  stage: release
  script:
    - release-cli create --tag-name "$CI_COMMIT_TAG" --name "$CI_COMMIT_TAG"
---
.glut:
  name: "release"
  setup:
    tag: "v1.2.0"
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
