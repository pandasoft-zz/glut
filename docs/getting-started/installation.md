# Installation

GLUT can run from Docker or as a native Go binary. Docker is the safer path
because the project targets POSIX systems and uses `gitlab-ci-local`.

## Docker

Build the image from this repository:

```bash
docker build -t glut:local .
```

Run tests from the current repository:

```bash
docker run --rm -v "$PWD:/work" -w /work glut:local run ./tests
```

If the tested jobs need Docker, share the Docker socket:

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$PWD:/work" \
  -w /work \
  glut:local run ./tests
```

## Go Install

```bash
go install github.com/pandasoft-zz/glut/cmd/glut@latest
```

This installs only `glut`. Native runs also need:

- POSIX shell
- `git`
- `bash`
- `gitlab-ci-local`

## First Check

```bash
glut version
glut lint ./tests
glut run ./tests
```

## Windows

Windows is not a target runtime. Use Docker or WSL2 on Windows.
