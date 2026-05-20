# Your First Test

This page walks you from a working GitLab CI job to a passing GLUT test.

## What You Need

- GLUT installed — see [Installation](installation.md).
- One GitLab CI job you want to test.

## Step 1: Write the Job

Start with the simplest job that does something observable. Here the job writes
an image reference to a file:

```yaml
stages:
  - build

build-image:
  stage: build
  script:
    - mkdir -p dist
    - echo "registry.example.com/app:${CI_COMMIT_REF_SLUG}" > dist/image.txt
```

## Step 2: Wrap It in a GLUT Test

A GLUT test is one YAML file with two documents separated by `---`. The first
document is the GitLab CI pipeline. The second document is GLUT metadata.

Save this as `tests/build-image.yml`:

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
  name: "build image on feature branch"
  setup:
    branch: "feature/my-change"
    pipeline_source: "push"
  assert:
    job:
      build-image:
        exit-status: 0
    artifacts:
      "dist/image.txt":
        exists: true
        contents:
          - "registry.example.com/app:feature-my-change"
```

Each `.glut:` field does one thing:

| Field | What it does |
| --- | --- |
| `name` | Display name shown in test output. |
| `setup.branch` | Sets `CI_COMMIT_BRANCH`. GLUT derives `CI_COMMIT_REF_SLUG` from it. |
| `setup.pipeline_source` | Sets `CI_PIPELINE_SOURCE`. `push` is the default for branch pipelines. |
| `assert.job.<name>.exit-status` | The job must exit with this code. `0` means success. |
| `assert.artifacts.<path>.exists` | The file must exist after the pipeline runs. |
| `assert.artifacts.<path>.contents` | The file must contain this string. |

`feature/my-change` becomes `feature-my-change` in `CI_COMMIT_REF_SLUG` — slash
and non-alphanumeric characters are replaced with `-`, then lowercased.

## Step 3: Check for Errors With `glut lint`

Run `glut lint` before executing the test. It catches structural and semantic
mistakes without needing Docker.

```bash
glut lint ./tests/
```

A clean lint produces no output and exits with code `0`. A broken file prints
one line per error:

```
[ERROR] tests/build-image.yml:12: .glut.assert.job.build-image: unknown key "xit-status"
```

Fix any errors before running.

## Step 4: Run the Test

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$PWD:/work" \
  -w /work \
  ghcr.io/pandasoft-zz/glut:latest run ./tests/
```

Or with the native binary:

```bash
glut run ./tests/
```

A passing test looks like this:

```
🧪 GLUT  Running 1 test

[1/1] ✓  tests/build-image.yml  12.4s

────────────────────────────────────────────────────────────
✓ 1 passed  in 12.4s
```

## Step 5: Make It Fail On Purpose

Change the expected string to something wrong:

```yaml
contents:
  - "wrong-image-name"
```

Run again. GLUT prints what failed, what was expected, and what was found:

```
[1/1] ✗  tests/build-image.yml  11.8s  FAILED

✗ FAILED  tests/build-image.yml
  test: "build image on feature branch"

  assert.artifacts."dist/image.txt".contents
    expected: pattern "wrong-image-name"
    actual:   no match

────────────────────────────────────────────────────────────
✗ 1 failed  in 11.8s
```

Fix the assert and run again. The test should pass.

## What Is Happening

GLUT creates an isolated workspace with a fake git remote. Your source files are
committed to that remote before the pipeline starts.

By default GLUT copies the entire repository. For a large repo where the
pipeline only needs a subdirectory (for example `component/`), pass
`--include` to limit what is copied:

```bash
glut run --include component ./tests/
```

This can reduce workspace setup from several seconds to under a second.

GLUT derives all `CI_*` variables from the `setup` block and passes them to
`gitlab-ci-local`. In this example, `setup.branch: "feature/my-change"` sets
`CI_COMMIT_BRANCH` and `CI_COMMIT_REF_SLUG`.

After the pipeline finishes, GLUT checks every assert. A test passes only when
all asserts pass.

See [CI Variables Set by GLUT](../reference/test-format.md#ci-variables-set-by-glut)
for the full list of variables GLUT sets per pipeline source.

## Adding More Asserts

A test that only checks `exit-status: 0` tells you the job did not crash. It
does not tell you the job did the right thing.

To make the test stronger, match the side effect:

- Job produces a file → `assert.artifacts`
- Job pushes a git commit → `assert.git`
- Job calls the GitLab API → `assert.api`
- Job calls an external tool → `assert.binary`

Run `glut doctor ./tests/` after adding asserts. It tells you which side effects
have no coverage yet.

## Next Steps

- [Test Format reference](../reference/test-format.md) — full `setup` and `assert` syntax
- [Assert Syntax reference](../reference/assert-syntax.md) — all matcher forms
- [Examples](../examples/image-build.md) — image build, manifest update, release, merge request
- [Debugging](debugging.md) — how to inspect a failing workspace
