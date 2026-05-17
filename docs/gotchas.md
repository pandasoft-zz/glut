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

## Docker Jobs: Host Bind-Mounts Are Unreliable

The Docker daemon always resolves `--volume host-path:container-path` using the
**host filesystem**, not the filesystem of the process that launches
`gitlab-ci-local`. In two common environments the host path is invisible:

- **Devcontainer / Docker Desktop on Windows**: the devcontainer root lives on
  Docker Desktop's overlay FS; paths under `/workspaces/...` are not visible to
  the Docker daemon.
- **Docker-in-Docker CI**: GLUT runs inside a container whose filesystem is not
  mounted into the outer Docker daemon's view.

GLUT therefore never uses bind-mounts for Docker jobs. It creates a named Docker
volume (`glut-<id>`), populates it by piping a tar archive through
`docker run -i … tar -x` (which works in all environments), and passes the
volume name as `--volume vol-name:path` to GCL. Named volumes are managed
entirely by the Docker daemon and are always accessible to job containers.
