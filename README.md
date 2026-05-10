# GLUT

GLUT is a CLI tool for testing GitLab CI components on your machine.
It runs a GitLab CI file with `gitlab-ci-local` and adds test setup, mocks,
and structured asserts. It is useful when a component changes git state,
calls the GitLab API, or calls tools such as `release-cli`.

## Install With Docker

The Docker image is the preferred install path once releases are published.
Until then, build the image from this repository:

```bash
docker build -t glut:local .
docker run --rm -v "$PWD:/work" -w /work glut:local run ./tests
```

If jobs need Docker, share the Docker socket:

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$PWD:/work" \
  -w /work \
  glut:local run ./tests
```

## Install With Go

```bash
go install github.com/pandasoft-zz/glut/cmd/glut@latest
```

This installs only the GLUT binary. You still need the native tools listed
below.

## Native Requirements

Native runs need:

- POSIX shell
- `git`
- `bash`
- `gitlab-ci-local`

Windows is not a target runtime. Use Docker or WSL2 on Windows.

## Quickstart

Create `tests/simple.yml`:

```yaml
stages:
  - test

test-job:
  stage: test
  script:
    - echo "hello from GLUT"
---
.glut:
  name: "simple job"
  setup:
    branch: "main"
  assert:
    job:
      test-job:
        exit-status: 0
        stdout:
          - "hello from GLUT"
```

Run it:

```bash
glut run ./tests/simple.yml
```

## Documentation

The documentation source is in [`docs/`](docs/index.md). It is built with
MkDocs Material.
