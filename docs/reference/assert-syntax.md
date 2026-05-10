# Assert Syntax

GLUT asserts are grouped by resource type. Each assert checks state after the
pipeline run.

## Job Asserts

`assert.job` checks job status and logs. The key is the GitLab CI job name.

```yaml
assert:
  job:
    build:
      present: true
      exit-status: 0
      stdout:
        - "build complete"
      stderr:
        not: "panic"
```

Fields:

- `present`: job must exist or must not exist.
- `exit-status`: expected process exit status.
- `stdout`: expected stdout text.
- `stderr`: expected stderr text.

## Artifact Asserts

`assert.artifacts` checks files in the workspace.

```yaml
assert:
  artifacts:
    "dist/result.json":
      exists: true
      contents:
        gjson:
          "name": "demo"
      size:
        gt: 0
      filetype: "file"
```

Fields:

- `exists`: file must exist or must not exist.
- `contents`: text or JSON content matcher.
- `mode`: file mode string.
- `size`: file size matcher.
- `md5`: expected MD5 hash.
- `sha256`: expected SHA-256 hash.
- `filetype`: `file`, `directory`, `symlink`, or `socket`.

## Git Asserts

`assert.git` checks the workspace repository or the fake origin repository.

```yaml
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

Fields for `workspace` and `origin`:

- `commits`: commit count matcher.
- `last-commit`: checks author, message, or SHA.
- `file`: checks a file in the repository.
- `branch`: current branch matcher. This is only for `workspace`.
- `clean`: workspace must be clean or dirty. This is only for `workspace`.

## API Asserts

`assert.api` checks calls recorded by the mock GitLab API. The key is
`METHOD path`. Use `*` to match a path segment.

```yaml
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

Fields:

- `called`: call must happen or must not happen.
- `times`: number of calls.
- `body`: JSON body matcher.

## Binary Asserts

`assert.binary` checks calls to mock binaries.

```yaml
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
          stdin:
            contain-substring: "payload"
      never-called-with:
        args:
          contain-element: "--dry-run"
```

Fields:

- `called`: binary must be called or must not be called.
- `times`: number of calls.
- `calls`: ordered call expectations.
- `never-called-with`: a call shape that must not appear.

## Text Patterns

For text lists, plain strings must exist in the text.

```yaml
stdout:
  - "created release"
```

Use `/.../` for regular expressions.

```yaml
stdout:
  - "/tag: v[0-9]+\\.[0-9]+\\.[0-9]+/"
```

Use `!/.../` to reject a regular expression.

```yaml
stdout:
  - "!/panic|fatal/"
```

Use `\!text` when the wanted text starts with `!`.

## Matchers

Matchers are objects with one key.

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

Use `gjson` when the actual value is JSON. Paths use the gjson path syntax.

```yaml
contents:
  gjson:
    "image.tag": "v1.2.0"
    "assets.#":
      ge: 1
```
