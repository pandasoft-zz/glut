# Installation

GLUT is best used from the Docker image. The image includes GLUT and the runtime
tools that tests need, such as `gitlab-ci-local`.

You can also use the native binary from GitHub Releases. Native use is useful
when your machine already has all required tools.

## Docker Image

Pull the image:

```bash
docker pull ghcr.io/pandasoft-zz/glut:latest
```

Run tests in the current repository:

```bash
docker run --rm \
  -v "$PWD:/work" \
  -w /work \
  ghcr.io/pandasoft-zz/glut:latest run ./tests
```

If the tested jobs need Docker, share the Docker socket:

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$PWD:/work" \
  -w /work \
  ghcr.io/pandasoft-zz/glut:latest run ./tests
```

For repeatable CI runs, pin a released tag instead of `latest`.

```bash
docker run --rm \
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

Use Docker if you do not want to manage these dependencies yourself.

## First Check

With Docker:

```bash
docker run --rm \
  -v "$PWD:/work" \
  -w /work \
  ghcr.io/pandasoft-zz/glut:latest lint ./tests
```

With the native binary:

```bash
glut lint ./tests
glut run ./tests
```

## Windows

Windows is not a target runtime. Use the Docker image on Windows.
