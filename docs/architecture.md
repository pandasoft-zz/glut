# GLUT Architecture

This document describes how GLUT should be structured as the implementation grows.
The goal is to keep the codebase easy to extend while preserving the v1.0
specification as the source of product behavior.

## Product Shape

GLUT is a single-binary CLI tool for testing GitLab CI components locally.
It orchestrates these pieces:

- Parse a GLUT test file.
- Extract the `.glut:` metadata document.
- Pass the remaining GitLab CI YAML to `gitlab-ci-local`.
- Create an isolated workspace and fake git origin.
- Start a mock GitLab API server.
- Install mock binaries into `PATH`.
- Execute the pipeline.
- Evaluate structured assertions.
- Report results.

The project is intentionally not a public Go library. All implementation code
lives under `internal/`.

## Package Boundaries

`cmd/glut`

Thin CLI entrypoint only. It owns Cobra command wiring, process exit codes, and
conversion from flags/environment variables to typed options. It should delegate
real work to internal packages.

`internal/config`

Typed domain configuration shared by parser, runner, workspace, mock server, and
asserter. Constants that describe GLUT syntax, such as pipeline source names,
belong here rather than in implementation-specific packages.

`internal/parser`

Reads YAML files and extracts the GLUT test definition. It should not execute
tests, create workspaces, or contain runner behavior. It may expose lint helpers,
but lint rules should remain separated from basic parsing.

`internal/schema`

Embeds and validates `schema/glut.schema.json`. Schema validation is part of the
runtime binary and must not depend on external files being present.

`internal/workspace`

Creates and destroys isolated workspaces, fake git origin repositories, and git
state required by tests. It should not know how to run `gitlab-ci-local` or
evaluate assertions.

`internal/executor`

Runs `gitlab-ci-local` with an isolated environment. It owns command execution,
timeouts, stdout/stderr capture, and executor-specific errors.

`internal/mockserver`

Starts the embedded mock GitLab API and records calls for later assertions. It
owns HTTP routing and mock API state.

`internal/mockwrapper`

Implements re-entrant binary mock behavior. This package is entered when the
GLUT binary is invoked under a mock binary name.

`internal/asserter`

Evaluates assertions against job results, artifacts, git state, mock API calls,
and mock binary logs. Each resource type should be implemented in its own file.

`internal/runner`

Coordinates the complete test lifecycle. It should be the only package that
knows the full order of parsing, workspace setup, mock setup, execution,
assertion, reporting, and cleanup.

`internal/reporter`

Formats results for console and report outputs. It should consume typed result
objects and avoid reaching back into runner internals.

## Execution Flow

1. CLI parses options and selects input paths.
2. Parser discovers test files and extracts `.glut:` config from the second YAML document.
3. Schema validation checks structural correctness.
4. Lint rules check semantic and GitLab-specific mistakes.
5. Runner creates an isolated workspace.
6. Runner starts mock API and mock binaries.
7. Workspace-derived and setup-derived CI variables are created.
8. Executor runs `gitlab-ci-local`.
9. Asserter evaluates the configured resources.
10. Reporter emits human and machine-readable results.
11. Runner cleans up or preserves the workspace based on options.

## Design Principles

- Keep the specification behavior in one typed model.
- Keep parsing, validation, orchestration, and execution separate.
- Prefer explicit options structs over package-level globals.
- Prefer small packages with clear ownership over large utility packages.
- Use mature libraries for complex formats and validation.
- Treat shell and git command execution as boundary code with strong errors.
- Make tests assert behavior, not just package existence.
