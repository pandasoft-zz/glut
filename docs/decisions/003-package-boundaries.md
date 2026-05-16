# 003. Package Boundaries

## Status

Accepted

## Decision

GLUT implementation packages should follow clear responsibility boundaries:

- `cmd/glut` handles CLI wiring and process exit.
- `internal/config` owns shared typed configuration and product vocabulary.
- `internal/parser` reads test files and extracts GLUT config.
- `schema` embeds and validates JSON Schema.
- `internal/workspace` prepares filesystem and git state.
- `internal/executor` runs `gitlab-ci-local`.
- `internal/mockserver` provides the mock GitLab API.
- `internal/mockwrapper` implements mock binary execution.
- `internal/asserter` evaluates assertions.
- `internal/runner` orchestrates full test execution.
- `internal/reporter` formats results.

## Rationale

The product has several integration boundaries: YAML parsing, git state, local
process execution, HTTP serving, and reporting. Mixing these concerns makes the
code harder to test and harder to extend.

Explicit package ownership gives future tasks a stable place to put new code and
gives reviews a clear standard for rejecting misplaced responsibilities.

## Consequences

- Shared constants should move out of implementation-specific packages.
- Cobra handlers should delegate rather than implement business logic directly.
- Runner should coordinate packages but avoid owning their internal details.
- Empty scaffold packages should either gain real contracts or stay minimal
  without fake passing tests.

