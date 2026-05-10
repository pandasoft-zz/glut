# GLUT

GLUT is a CLI tool for testing GitLab CI components on your machine.
It runs a GitLab CI file with `gitlab-ci-local` and adds test setup, mocks,
and structured asserts. It is useful when a component changes git state,
calls the GitLab API, or calls tools such as `release-cli`.

## Install With Docker

The Docker image is the preferred install path because it includes runtime
dependencies.

```bash
docker pull ghcr.io/pandasoft-zz/glut:latest
docker run --rm -v "$PWD:/work" -w /work ghcr.io/pandasoft-zz/glut:latest run ./tests
```

If jobs need Docker, share the Docker socket:

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$PWD:/work" \
  -w /work \
  ghcr.io/pandasoft-zz/glut:latest run ./tests
```

## Install Native Binary

Download the binary archive for your operating system and CPU from
[GitHub Releases](https://github.com/pandasoft-zz/glut/releases).

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
