# GLUT Implementation Gotchas

This document records non-obvious implementation details that should be kept in
mind during future tasks.

## GitLab CI YAML Is Not Ordinary YAML

GitLab CI files may contain anchors, aliases, custom-looking keys, includes,
extends, and syntax that users expect GitLab tooling to handle. GLUT should avoid
normalizing the pipeline YAML just because it needs to read the `.glut:`
metadata document.

Do not parse the whole file into `map[string]interface{}` and marshal it back as
the pipeline input unless the consequences are explicitly accepted.

## The `.glut:` Document Is GLUT Syntax

Only the second YAML document with `.glut:` is owned by GLUT. The first YAML
document is GitLab CI syntax and should be treated as pass-through content for
`gitlab-ci-local`.

This distinction is central to the product.

## JSON Schema Does Not Replace Semantic Linting

JSON Schema is excellent for structure, types, required fields, enums, and many
local constraints. It is not enough for every rule.

Examples that still need Go lint rules:

- `assert.job` references a job that does not exist in the pipeline section.
- A job uses a stage that is not listed in `stages:`.
- A test combines fields that are individually valid but invalid together in
  GitLab semantics.

## Schema Must Be Embedded

The schema is part of the runtime behavior. The released binary must validate
tests without needing external schema files on disk.

## CI_DEFAULT_BRANCH Is Not Derived From The Test Branch

`CI_DEFAULT_BRANCH` represents the project's default branch, not the branch
the test runs on. GLUT determines it with this priority:

1. `setup.default_branch` — explicit value in the test file.
2. `setup.api.project.default_branch` — deprecated; still accepted.
3. Auto-detection from `refs/remotes/origin/HEAD` in the source repository.
4. `"main"` — hard fallback.

`setup.git.origin.branch` never influences `CI_DEFAULT_BRANCH`. That field
controls only which branch the workspace is cloned from. A test that simulates
a feature-branch pipeline must set both fields independently:

```yaml
setup:
  branch: "feature/my-thing"        # CI_COMMIT_BRANCH
  default_branch: "main"            # CI_DEFAULT_BRANCH
  git:
    origin:
      branch: "feature/my-thing"    # workspace checkout branch
```

## CI Variable Semantics Are Product Behavior

The values and presence of `CI_*` variables are not implementation details.
They are part of GLUT's contract with component authors.

Use table-driven tests for every supported pipeline source and for branch, tag,
and merge request contexts.

## Workspace Paths Are Easy To Confuse

The temporary root and the actual cloned repository are different paths.

- The temporary root contains GLUT internals such as origin and logs.
- The workspace repository is where the pipeline should run.

Names and environment variables must make this distinction clear.

## Shell And Git Commands Are Boundary Code

Git and shell calls are fragile integration points. Always check their errors
and include command output in the returned error where it helps debugging.

Ignoring errors during setup creates misleading failures later in the test run.

## Scaffold Should Not Pretend To Be Coverage

Empty structs and empty tests are acceptable only as very short-lived scaffolding.
They should not survive as a green test suite because they make implementation
progress look stronger than it is.

When a package is not implemented yet, prefer a clear follow-up document or
issue over an empty passing test.

## Native Windows Is Not A Target

The product targets POSIX systems. Windows support through Docker or WSL2 may be
useful for development, but implementation choices should not compromise the
POSIX contract described in the specification.

## Docker Jobs: stdout Is Not Captured After Slow Commands

`gitlab-ci-local` 4.72.0 runs Docker job scripts by piping `. /gcl-cmd` into
`docker start --attach -i` and immediately closing stdin (`stdin.end()`). When
stdin closes, Docker terminates the attach session. Any container output that
arrives **after** a long-running command (e.g. `apk add` taking ~8 s) is never
relayed to GCL and therefore never captured by GLUT's output parser.

Practical consequence: `assert.job.<name>.stdout` assertions **will not work
reliably in Docker jobs that install packages via `apk add`, `apt-get`, or any
other slow first command**. The job still runs and its exit code is captured
correctly.

Workaround: assert on `exit-status`, `api.called`, or `binary.called` instead
of `stdout` for Docker jobs with slow setup commands. If stdout capture is
essential, use a Docker image that already has all required tools installed so
no slow install step is needed.

## Docker Jobs: Host Bind-Mounts Are Unreliable Inside Containers

The Docker daemon always resolves `--volume host-path:container-path` using the
**host filesystem**, not the filesystem of the process that launches
`gitlab-ci-local`. In two common environments the host path is invisible:

- **Devcontainer / Docker Desktop on Windows**: the devcontainer root lives on
  Docker Desktop's overlay FS; paths under `/workspaces/...` are not visible to
  the Docker daemon.
- **Docker-in-Docker CI**: GLUT runs inside a container whose filesystem is not
  mounted into the outer Docker daemon's view.

In both cases GLUT falls back to a named Docker volume (`glut-<id>`), populated
by piping a tar archive through `docker run -i … tar -x` (which works in all
environments), and passes the volume name as `--volume vol-name:path` to GCL.
Named volumes are managed entirely by the Docker daemon and are always
accessible to job containers.

On a native Linux host running GLUT directly (not inside a container), the
daemon and GLUT share the same filesystem, so GLUT uses a plain bind mount
instead — see [Volume Strategy Auto-Detection](#docker-jobs-volume-strategy-auto-detection)
below for the detection logic and the `--docker-volume-strategy` override.

## Docker Jobs: Parallel Job Limit in Docker Desktop

When running more than two Docker jobs in the **same stage**, Docker Desktop on
WSL2 (and similar environments) can fail with errors such as:

```
Error: Command failed with exit code 1: docker start --attach <id>
Error response from daemon: No such container: <id>
```

**Root cause**: Each parallel job runs a `copied to docker volumes` step that
reads from the shared GLUT Docker volume while simultaneously writing to a new
per-job build volume. Docker Desktop's internal VM cannot reliably handle more
than two concurrent volume copy operations against the same source volume.

**Current limitation**: GLUT tests with Docker mode should place no more than
**two Docker jobs in any single stage**. Split four parallel jobs across two
stages (`wave-one` and `wave-two`) as a workaround:

```yaml
stages:
  - wave-one
  - wave-two

job-alpha:
  stage: wave-one
  image: debian:12-slim
  script: [...]

job-beta:
  stage: wave-one
  image: debian:12-slim
  script: [...]

job-gamma:
  stage: wave-two
  image: debian:12-slim
  script: [...]

job-delta:
  stage: wave-two
  image: debian:12-slim
  script: [...]
```

This limitation is specific to Docker Desktop on Windows/WSL2 and may not
affect a native Linux Docker daemon. It is a known issue to investigate and
fix in a future release.

## Docker Jobs: Container Output Capture Races With docker rm

GLUT captures Docker container stdout/stderr by watching `docker events` for
container start events and then running `docker logs` after the container exits
(`docker wait` + `docker logs`). This races against `gitlab-ci-local`'s
container cleanup (`docker rm`).

For fast-exiting containers (jobs that finish in under ~1 second), the container
may be removed by the time `docker logs` runs, causing `No such container`
errors in `job.Stdout`. The `docker wait` approach generally wins because
`gitlab-ci-local` calls `docker rm` through Node.js async Promises (multiple
event loop ticks after exit), giving the Go goroutine enough time to run
`docker logs` first.

Practical guideline: if a Docker job exits in under one second and you need
`stdout` assertions, add a small sleep (e.g., `sleep 0.1` between output
lines) so the container lives long enough for GLUT to capture its logs.

## Docker Jobs: Volume Cleanup Timing on WSL2 / Docker Desktop

On WSL2 with Docker Desktop, `docker run --rm` returns to the caller as soon
as the container process exits. The daemon removes the container object
**asynchronously** — it may still appear in `docker ps -a` for a brief window
after the command returns. Two consequences occur when tests run back-to-back:

1. **Volume removal fails**: `docker volume rm` refuses to remove a volume
   while any container still references it. Containers spawned by
   `gitlab-ci-local` (or by GLUT's own `ReadLogsFromDockerVolume` /
   `FetchGitOriginTar`) may still be registered at the moment GLUT calls
   `docker volume rm`, leaving the volume orphaned.

2. **Next test's populate fails**: While the daemon processes cleanup from test
   N, a `docker run` started by test N+1 may receive a transient tar or chown
   error because daemon resources are partially busy.

**Mitigations implemented in GLUT**:

- `DestroyDockerVolume` runs `docker ps -a --filter volume=<name>` before
  removing the volume and force-removes any lingering containers so that
  `docker volume rm` can always succeed.

- `CreateDockerVolume` retries the populate `docker run` up to three times
  with exponential backoff (500 ms, 1 s) to survive transient daemon busy
  errors between sequential tests.

This behaviour is specific to Docker Desktop on Windows/WSL2. It does not
reproduce on a native Linux Docker daemon.

## Docker Jobs: Volume Strategy Auto-Detection

GLUT automatically selects between two strategies for providing workspace
files to Docker job containers:

- **bind** (native Linux): the host workspace directory is bind-mounted
  directly at the same absolute path. No Docker named volume or Alpine
  populate container is needed.
- **volume** (Docker Desktop / WSL2): a Docker named volume is created,
  populated via an Alpine container, and destroyed after all tests finish.

Auto-detection checks for `/.dockerenv`, which Docker creates in every
container it starts:

- **Inside a container** (devcontainer on Docker Desktop, Docker-in-Docker
  in CI): the daemon resolves bind-mount paths against the host or outer
  daemon filesystem — not the inner container's filesystem. Named volumes
  are required. `/.dockerenv` is present → strategy `volume`.
- **Native Linux host** (no container): the daemon and GLUT share the same
  filesystem. `/.dockerenv` is absent → strategy `bind`.

Override with `--docker-volume-strategy=bind|volume` if auto-detection
produces the wrong result for an unusual environment.
