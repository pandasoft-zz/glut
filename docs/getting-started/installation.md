# Installation

GLUT is best used from the Docker image. The image includes GLUT and the runtime
tools that tests need, such as `gitlab-ci-local`.

`gitlab-ci-local` runs GitLab CI jobs in Docker containers. Because of that, the
GLUT Docker image must be able to reach a Docker daemon. Use socket sharing for
local development. Use Docker-in-Docker when socket sharing is not available or
when you need stronger isolation.

You can also use the native binary from GitHub Releases. Native use is useful
when your machine already has all required tools.

## Docker Image

Pull the image:

```bash
docker pull ghcr.io/pandasoft-zz/glut:latest
```

### Local Run With Docker Socket

This is the most common local setup. The GLUT container uses the Docker daemon
from your host machine.

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$PWD:/work" \
  -w /work \
  ghcr.io/pandasoft-zz/glut:latest run ./tests
```

Use this when you run GLUT on Linux, macOS, or Docker Desktop with a working
Docker socket.

The mounted socket gives the container control over the host Docker daemon.
Use it only with images and tests you trust.

### GitLab CI Integration With Docker Socket

This is the simplest pipeline when your GitLab Runner already mounts the host
Docker socket into job containers.

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

Your GitLab Runner must mount `/var/run/docker.sock` into the job container.
That is a runner configuration setting, not a normal job-level script setting.
The exact setup depends on your GitLab Runner executor.

### GitLab CI Integration With Docker-in-Docker

Use Docker-in-Docker when you cannot or do not want to share the host socket.
The GLUT container talks to a Docker daemon that runs in a sibling service.

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

The runner must allow privileged services for Docker-in-Docker.

`glut lint` does not need `gitlab-ci-local` or a Docker daemon. It only reads
test files, validates `.glut:` metadata, and runs semantic checks. The JSON
artifact is useful for debugging and AI tools. GitLab does not read it as a
native test report. The JUnit report belongs to `glut run`, because that command
executes tests.

For a local manual check, you can start a Docker daemon container and point GLUT
at it:

```bash
docker network create glut-dind

docker run --rm -d \
  --privileged \
  --network glut-dind \
  --name glut-docker \
  -e DOCKER_TLS_CERTDIR= \
  docker:25-dind

docker run --rm \
  --network glut-dind \
  -e DOCKER_HOST=tcp://glut-docker:2375 \
  -v "$PWD:/work" \
  -w /work \
  ghcr.io/pandasoft-zz/glut:latest run ./tests
```

For repeatable CI runs, pin a released tag instead of `latest`.

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$PWD:/work" \
  -w /work \
  ghcr.io/pandasoft-zz/glut:v1.0.0 run ./tests
```

## Native Binary

Download the archive for your operating system and CPU from GitHub Releases:

```text
https://github.com/pandasoft-zz/glut/releases
```

Unpack the archive and put `glut` on `PATH`.

```bash
glut version
```

Native runs need these tools on `PATH`:

- POSIX shell
- `git`
- `bash`
- `gitlab-ci-local`
- Docker daemon access

Use Docker if you do not want to manage these dependencies yourself.

## First Check

With Docker:

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$PWD:/work" \
  -w /work \
  ghcr.io/pandasoft-zz/glut:latest lint ./tests
```

With the native binary:

```bash
glut lint ./tests
glut doctor ./tests
glut run ./tests
```

`doctor` does not need Docker or `gitlab-ci-local`. It reads test files,
reports lint issues, prints authoring hints, and shows job coverage. Run it
before `run` to catch weak or incomplete tests early.

## Windows

Windows is not a target runtime. Use the Docker image on Windows.
