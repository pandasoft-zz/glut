# Assert Syntax

GLUT asserts check the result after the pipeline run. They are grouped by
resource type:

- `job`: GitLab CI jobs and logs.
- `artifacts`: files in the workspace.
- `git`: workspace and fake origin git state.
- `api`: calls recorded by the mock GitLab API.
- `binary`: calls recorded by mock binaries.

Most assert fields accept either an exact value or a matcher object. A matcher
object has one operator key.

```yaml
exit-status: 0
branch:
  have-prefix: "release/"
commits:
  ge: 2
```

## Operator Reference

Use the same operators across job, artifact, git, API, and binary asserts.

### Equality And Text Operators

| Operator | Value type | Actual value | Meaning | Example |
| --- | --- | --- | --- | --- |
| `equal` | any | any | Exact value match. | `equal: "main"` |
| `have-prefix` | string | string | Actual string starts with value. | `have-prefix: "feat/"` |
| `have-suffix` | string | string | Actual string ends with value. | `have-suffix: ".json"` |
| `contain-substring` | string | string | Actual string contains value. | `contain-substring: "ready"` |
| `match-regexp` | string | string | Actual string matches a regexp. | `match-regexp: "^v[0-9]+"` |
| `semver-constraint` | string | string | Actual value is a semver version in range. | `semver-constraint: ">=1.2.0"` |

Example:

```yaml
assert:
  git:
    origin:
      last-commit:
        message:
          contain-substring: "update manifest"
        sha:
          match-regexp: "^[a-f0-9]{40}$"
```

### Number Operators

| Operator | Value type | Actual value | Meaning | Example |
| --- | --- | --- | --- | --- |
| `gt` | number | number | Actual value is greater than value. | `gt: 0` |
| `ge` | number | number | Actual value is greater than or equal to value. | `ge: 1` |
| `lt` | number | number | Actual value is less than value. | `lt: 10` |
| `le` | number | number | Actual value is less than or equal to value. | `le: 10` |

Example:

```yaml
assert:
  artifacts:
    "dist/result.json":
      size:
        gt: 0
  binary:
    release-cli:
      times:
        ge: 1
```

### Collection Operators

| Operator | Value type | Actual value | Meaning | Example |
| --- | --- | --- | --- | --- |
| `contain-element` | any | list | At least one item matches value. | `contain-element: "create"` |
| `contain-elements` | list | list | All listed items are present. | `contain-elements: ["linux", "amd64"]` |
| `consist-of` | list | list | Lists contain the same values. Order does not matter. | `consist-of: ["a", "b"]` |
| `have-len` | integer | list, map, or string | Actual value has this length. | `have-len: 2` |
| `have-key` | string | map | Actual map has this key. | `have-key: "tag_name"` |

Example:

```yaml
assert:
  binary:
    docker:
      calls:
        - args:
            contain-elements:
              - "build"
              - "--pull"
              - "."
```

### JSON Operators

| Operator | Value type | Actual value | Meaning | Example |
| --- | --- | --- | --- | --- |
| `gjson` | map | JSON text or object | Read values with gjson paths and match them. | `"items.#": { ge: 1 }` |

Use `gjson` for JSON files and API request bodies.

```yaml
assert:
  artifacts:
    "dist/manifest.json":
      contents:
        gjson:
          "image.name": "registry.example.com/app"
          "image.tag":
            match-regexp: "^v[0-9]+\\.[0-9]+\\.[0-9]+$"
          "platforms.#":
            ge: 2
```

### Logic Operators

| Operator | Value type | Actual value | Meaning | Example |
| --- | --- | --- | --- | --- |
| `and` | list | any | All child matchers must pass. | `and: [{ have-prefix: "v" }, { semver-constraint: ">=1.0.0" }]` |
| `or` | list | any | At least one child matcher must pass. | `or: ["main", "master"]` |
| `not` | matcher | any | Child matcher must fail. | `not: { contain-substring: "dirty" }` |

Example:

```yaml
assert:
  git:
    workspace:
      branch:
        or:
          - "main"
          - "master"
      last-commit:
        message:
          and:
            - have-prefix: "chore:"
            - not:
                contain-substring: "WIP"
```

## Text Pattern Lists

`stdout`, `stderr`, `output`, and some `contents` fields can use a list of text patterns.
Each item is checked against the actual text.

| Pattern | Meaning |
| --- | --- |
| `plain text` | Text must contain this string. |
| `/regexp/` | Text must match this regexp. |
| `!/regexp/` | Text must not match this regexp. |
| `\!text` | Text must contain a value that starts with `!`. |

Example:

```yaml
assert:
  job:
    release:
      stdout:
        - "created release"
        - "/tag: v[0-9]+\\.[0-9]+\\.[0-9]+/"
        - "!/panic|fatal/"
```

## Job Asserts

`assert.job` checks job presence, exit status, stdout, and stderr. The key is
the GitLab CI job name.

| Field | Type | Meaning |
| --- | --- | --- |
| `present` | boolean | Job must exist in the pipeline or must not exist. A job with a matching rule and `when: manual` **is present** even though it does not run. |
| `when` | enum | Evaluated `when:` value of the job: `on_success`, `on_failure`, `manual`, `delayed`, or `always`. **Opt-in:** when omitted, the job's `when` value is not checked at all — existing tests behave exactly as before. (`never` is not a valid value; a never-job is simply absent, use `present: false`.) |
| `exit-status` | value or matcher | Process exit status. |
| `stdout` | text list or matcher | Standard output. |
| `stderr` | text list or matcher | Standard error. |
| `output` | text list or matcher | Combined stdout and stderr (stdout first). Use when the stream does not matter. |

Basic success:

```yaml
assert:
  job:
    build:
      present: true
      exit-status: 0
      stdout:
        - "build complete"
```

Expected failure:

```yaml
assert:
  job:
    validate:
      exit-status: 1
      stderr:
        - "missing tag"
        - "!/panic|stack trace/"
```

Combined output (stream does not matter):

```yaml
assert:
  job:
    install:
      output:
        - "Composer Install"
        - "/Composer version/"
```

Skipped or missing job:

```yaml
assert:
  job:
    deploy-production:
      present: false
```

Manual job — rule matched, the job is in the pipeline, but it does not run
automatically:

```yaml
assert:
  job:
    release:job:
      present: true
      when: manual
```

The `when` field is opt-in: if you do not specify it, GLUT does not check the
job's `when` value and no default is assumed. Notes on combining fields:

- `exit-status`, `stdout`, `stderr`, and `output` require the job to have
  actually executed. Asserting them on a present-but-not-executed job (such as
  a manual job) fails with `job present but not executed` — there is no real
  output to match against.
- `when` combined with `present: false` is rejected by lint: an absent job has
  no `when` value. A job whose rule sets `when: never` is absent; assert it
  with `present: false`.

Flexible log check:

```yaml
assert:
  job:
    package:
      stdout:
        contain-substring: "archive created"
      stderr:
        not:
          contain-substring: "warning"
```

## Artifact Asserts

`assert.artifacts` checks files in the workspace. The key is the relative path.

| Field | Type | Meaning |
| --- | --- | --- |
| `exists` | boolean | Path must exist or must not exist. |
| `contents` | text list or matcher | File content. |
| `mode` | string | File permission mode as four octal digits, such as `0644`. |
| `size` | value or matcher | File size in bytes. |
| `md5` | string | Expected MD5 hash. |
| `sha256` | string | Expected SHA-256 hash. |
| `filetype` | string | `file`, `directory`, `symlink`, or `socket`. |

Text file:

```yaml
assert:
  artifacts:
    "dist/image.txt":
      exists: true
      filetype: "file"
      contents:
        - "registry.example.com/app"
        - "/tag: feature-[a-z0-9-]+/"
```

JSON file:

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
          "assets.#":
            ge: 1
```

File metadata:

```yaml
assert:
  artifacts:
    "dist/tool":
      exists: true
      mode: "0755"
      size:
        gt: 1024
      sha256: "3a6eb0790f39ac87c94f3856b2dd2c5d110e6811602261a9a923d3bb23adc8b7"
```

Absent file:

```yaml
assert:
  artifacts:
    "dist/debug.log":
      exists: false
```

## Git Asserts

`assert.git` checks the workspace repository and the fake origin repository.

```yaml
assert:
  git:
    workspace:
      clean: true
    origin:
      commits:
        ge: 2
```

Fields for `workspace` and `origin`:

| Field | Type | Meaning |
| --- | --- | --- |
| `commits` | value or matcher | Commit count. |
| `last-commit` | object | Last commit metadata. |
| `file` | map | Files in the repository. |
| `branch` | value or matcher | Current branch. Workspace only. |
| `clean` | boolean | Workspace has no changes. Workspace only. |

Fields for `last-commit`:

| Field | Type | Meaning |
| --- | --- | --- |
| `author-name` | value or matcher | Last commit author name. |
| `author-email` | value or matcher | Last commit author email. |
| `message` | value or matcher | Last commit message. |
| `sha` | value or matcher | Last commit SHA. |

Fields for `file` entries (key is the path relative to the repository root):

| Field | Type | Meaning |
| --- | --- | --- |
| `exists` | boolean | File must exist or must not exist. |
| `contents` | text list or matcher | File content. |
| `mode` | string | File permission mode as four octal digits, such as `0644`. |
| `size` | value or matcher | File size in bytes. |
| `md5` | string | Expected MD5 hash. |
| `sha256` | string | Expected SHA-256 hash. |
| `filetype` | string | `file`, `directory`, `symlink`, or `socket`. |

These fields are identical to `assert.artifacts` fields.

Check a pushed manifest update:

```yaml
assert:
  git:
    origin:
      commits:
        ge: 2
      last-commit:
        author-email: "test@example.com"
        message:
          contain-substring: "update manifest"
      file:
        "manifest.yaml":
          contents:
            contain-substring: "image: new"
```

Check the workspace state:

```yaml
assert:
  git:
    workspace:
      branch: "main"
      clean: true
```

Check a commit SHA shape:

```yaml
assert:
  git:
    origin:
      last-commit:
        sha:
          match-regexp: "^[a-f0-9]{40}$"
```

Check a file was deleted by the pipeline:

```yaml
assert:
  git:
    origin:
      file:
        "CHANGELOG.md":
          exists: false
        "dist/release.json":
          exists: true
          contents:
            - "/\"version\": \"v[0-9]+/"
```

## API Asserts

`assert.api` checks calls recorded by the mock GitLab API. The key is
`METHOD path`. Use `*` to match a path segment.

| Field | Type | Meaning |
| --- | --- | --- |
| `called` | boolean | Request must happen or must not happen. |
| `times` | value or matcher | Number of matching requests. |
| `body` | map | Request JSON body matcher. |

Release creation:

```yaml
assert:
  api:
    "POST /api/v4/projects/*/releases":
      called: true
      times: 1
      body:
        tag_name: "v1.2.0"
        name:
          contain-substring: "v1.2.0"
```

Merge request comment:

```yaml
assert:
  api:
    "POST /api/v4/projects/*/merge_requests/*/notes":
      called: true
      body:
        body:
          contain-substring: "ready"
```

No production call:

```yaml
assert:
  api:
    "DELETE /api/v4/projects/*/releases/*":
      called: false
```

Nested JSON body:

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

## Binary Asserts

`assert.binary` checks calls to mock binaries. The key is the binary name.

| Field | Type | Meaning |
| --- | --- | --- |
| `called` | boolean | Binary must be called or must not be called. |
| `times` | value or matcher | Number of calls. |
| `calls` | list | Ordered call expectations. |
| `never-called-with` | object | A call shape that must not appear. |

Call fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `args` | value or matcher | Process arguments. |
| `cwd` | value or matcher | Working directory. |
| `stdin` | value or matcher | Standard input. |

Release CLI call:

```yaml
assert:
  binary:
    release-cli:
      called: true
      times: 1
      calls:
        - args:
            contain-elements:
              - "create"
              - "--tag-name"
              - "v1.2.0"
          cwd:
            have-suffix: "/builds"
```

Docker build call:

```yaml
assert:
  binary:
    docker:
      called: true
      calls:
        - args:
            and:
              - contain-element: "build"
              - contain-element: "--pull"
              - contain-element: "."
```

Command must not use a flag:

```yaml
assert:
  binary:
    release-cli:
      never-called-with:
        args:
          contain-element: "--dry-run"
```

Check stdin:

```yaml
assert:
  binary:
    kubectl:
      calls:
        - args:
            contain-elements:
              - "apply"
              - "-f"
              - "-"
          stdin:
            contain-substring: "apiVersion:"
```

## Full Example

This example combines job, artifact, git, API, and binary asserts.

```yaml
assert:
  job:
    release:
      exit-status: 0
      stdout:
        - "release-cli create"
        - "!/panic|fatal/"
  artifacts:
    "dist/release.json":
      exists: true
      contents:
        gjson:
          "tag": "v1.2.0"
  git:
    workspace:
      clean: true
  api:
    "POST /api/v4/projects/*/releases":
      called: true
      times: 1
  binary:
    release-cli:
      called: true
      calls:
        - args:
            contain-elements:
              - "create"
              - "v1.2.0"
```
