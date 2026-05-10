# GLUT

GLUT is a CLI tool for testing GitLab CI components on your machine.
It runs a GitLab CI file with `gitlab-ci-local` and adds test setup, mocks,
and structured asserts. It is useful when a component changes git state,
calls the GitLab API, or calls tools such as `release-cli`.

## Install With Docker

The Docker image is the preferred install path because it includes runtime
dependencies. GLUT uses `gitlab-ci-local`, and `gitlab-ci-local` runs jobs in
Docker containers. The GLUT container must be able to reach a Docker daemon.

```bash
docker pull ghcr.io/pandasoft-zz/glut:latest
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$PWD:/work" \
  -w /work \
  ghcr.io/pandasoft-zz/glut:latest run ./tests
```

For GitLab CI, use socket sharing when your runner mounts the host Docker
socket:

```yaml
stages:
  - lint
  - test

lint:glut:
  stage: lint
  image: ghcr.io/pandasoft-zz/glut:latest
  script:
    - mkdir -p reports
    - glut lint --format=json ./tests > reports/glut-lint.json
  artifacts:
    when: always
    paths:
      - reports/glut-lint.json

test:glut:
  stage: test
  image: ghcr.io/pandasoft-zz/glut:latest
  needs:
    - lint:glut
  variables:
    DOCKER_HOST: "unix:///var/run/docker.sock"
  script:
    - mkdir -p reports
    - glut run --report=junit:reports/glut-junit.xml ./tests
  artifacts:
    when: always
    reports:
      junit: reports/glut-junit.xml
    paths:
      - reports/glut-junit.xml
```

Use Docker-in-Docker when your runner supports privileged services:

```yaml
stages:
  - lint
  - test

lint:glut:
  stage: lint
  image: ghcr.io/pandasoft-zz/glut:latest
  script:
    - mkdir -p reports
    - glut lint --format=json ./tests > reports/glut-lint.json
  artifacts:
    when: always
    paths:
      - reports/glut-lint.json

test:glut:
  stage: test
  image: ghcr.io/pandasoft-zz/glut:latest
  needs:
    - lint:glut
  services:
    - name: docker:25-dind
      alias: docker
  variables:
    DOCKER_HOST: "tcp://docker:2375"
    DOCKER_TLS_CERTDIR: ""
  script:
    - mkdir -p reports
    - glut run --report=junit:reports/glut-junit.xml ./tests
  artifacts:
    when: always
    reports:
      junit: reports/glut-junit.xml
    paths:
      - reports/glut-junit.xml
```

`lint:glut` does not need Docker daemon access. Its JSON artifact is for debug
and AI tools. `test:glut` writes the JUnit report that GitLab can show in the
pipeline UI.

## Install Native Binary

Download the binary archive for your operating system and CPU from
[GitHub Releases](https://github.com/pandasoft-zz/glut/releases).

## Native Requirements

Native runs need:

- POSIX shell
- `git`
- `bash`
- `gitlab-ci-local`
- Docker daemon access

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
