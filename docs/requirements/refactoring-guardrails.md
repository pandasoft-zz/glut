# Refactoring Guardrails Before Further Implementation

## Status

Draft requirement for the current implementation branch.

## Context

The repository is still in an early implementation stage. Several packages are
prepared as scaffolding, while the first implementation task already introduced
patterns that would become hard to maintain if copied into the remaining tasks.

Before adding more features, the project needs implementation guardrails:

- Clear package responsibilities.
- A validation strategy for GLUT YAML.
- A convention for constants and shared domain vocabulary.
- Meaningful tests.
- Explicit CLI options and runner boundaries.

## Goals

- Make the codebase easier to extend across the remaining implementation tasks.
- Prevent parser, workspace, and CLI code from becoming large mixed-responsibility
  files.
- Make JSON Schema part of runtime validation while preserving semantic linting.
- Keep all committed documentation and code in English.
- Improve review quality by documenting what belongs where.

## Non-Goals

- Implement the full GLUT runner.
- Implement every mock GitLab API endpoint.
- Replace the v1.0 specification.
- Build a custom YAML parser.

## Requirements

### Documentation Structure

The repository should contain these implementation guidance documents:

- `docs/architecture.md`
- `docs/conventions.md`
- `docs/gotchas.md`
- `docs/requirements/refactoring-guardrails.md`
- `docs/decisions/001-repository-language.md`
- `docs/decisions/002-parser-and-validation.md`
- `docs/decisions/003-package-boundaries.md`

### Parser

- Parsing must be split from linting.
- `.glut:` extraction must avoid generic full-file map marshal/unmarshal.
- The parser must expose typed config for GLUT-owned syntax.
- The pipeline YAML must remain suitable for `gitlab-ci-local`.
- Parser tests must include YAML features that are common in GitLab CI files.

### Schema Validation

- `schema/glut.schema.json` must become a real schema for the `.glut:` metadata
  document.
- The schema must be embedded into the binary.
- Structural validation should use the schema.
- Semantic validation should remain in Go lint rules.

### Workspace

- Workspace creation, git origin setup, and CI variable generation should be
  separated into smaller files.
- Shell and git command errors must be checked and wrapped with context.
- CI variable generation must have table-driven tests for each supported
  pipeline source.

### CLI

- Cobra handlers should be thin.
- CLI flags and environment variables should be converted into typed options.
- The command layer should delegate to runner, parser, or lint services.

### Tests

- Empty passing tests must be removed or replaced with behavior tests.
- Test names should describe behavior.
- Tests should cover edge cases introduced by the specification, not only happy
  paths.

## Acceptance Criteria

- The documentation files above exist and are written in English.
- Future implementation tasks can reference these docs as review criteria.
- `go test ./...` remains green.
- No new empty passing tests are introduced.
- Any new dependency added for YAML or schema validation is documented in the
  relevant decision record.
