# GLUT Implementation Conventions

This document defines coding conventions for GLUT. These rules are intended to
guide future implementation tasks and code reviews.

## Repository Language

All committed repository content is written in English:

- Code identifiers and comments.
- Documentation.
- Test names and fixtures.
- Commit messages and PR descriptions.

Use simple technical English, roughly at A2 level. Prefer short sentences,
common words, and direct structure. Avoid complex idioms and long nested
sentences.

## Go Style

- Use small functions with one clear responsibility.
- Return errors with enough context to diagnose the failing phase.
- Avoid package-level mutable state.
- Prefer typed options structs over long argument lists once a function has more
  than a few related parameters.
- Keep comments focused on non-obvious decisions. Do not restate simple code.
- Do not ignore returned errors unless the behavior is intentionally best-effort
  and documented at the call site.

## Constants

Constants belong near the domain that owns them.

- GLUT syntax constants, such as pipeline source names, belong in
  `internal/config`.
- Workspace-only constants belong in `internal/workspace`.
- CLI flag names and environment variable names belong near CLI option parsing,
  unless they are shared runtime concepts.

Do not put shared product vocabulary into a package merely because that package
currently happens to use it first.

## Package Responsibilities

Packages should not reach across layers casually.

- `cmd/glut` wires commands and exits the process.
- `runner` orchestrates.
- `parser` parses.
- `schema` validates structure.
- `workspace` prepares filesystem and git state.
- `executor` runs `gitlab-ci-local`.
- `mockserver` serves and records API calls.
- `mockwrapper` records binary calls.
- `asserter` evaluates expectations.
- `reporter` formats results.

When a package starts doing work from another package, split the behavior before
adding more features.

## Parser and Validation

YAML parsing must preserve GitLab CI semantics. The pipeline part of a test file
must not be rewritten through a generic map marshal/unmarshal cycle.

The parser should:

- Read YAML into an AST.
- Extract the top-level `.glut:` node from the second YAML document.
- Decode only the `.glut:` node into typed config.
- Preserve or faithfully re-render the remaining pipeline YAML.
- Report useful file and line information where possible.

JSON Schema is the authoritative structural validator for the `.glut:` metadata
document.
Additional Go lint rules should cover semantic checks that JSON Schema cannot
express clearly, such as references between pipeline jobs and assertions.

## Embedded Assets

Runtime validation assets must be embedded into the binary. GLUT is distributed
as a single executable, so it must not require `schema/glut.schema.json` to exist
next to the binary at runtime.

Use Go `embed` for schema and other runtime assets.

## Error Handling

Boundary operations must include phase and command context:

- File reads and writes.
- Git commands.
- Shell commands.
- HTTP server startup.
- `gitlab-ci-local` execution.
- Schema validation.

Prefer errors that answer: what phase failed, what input was involved, and what
the underlying error said.

## Tests

Tests must assert behavior. Empty tests are not allowed.

Use table-driven tests for:

- CI variable generation.
- Pipeline source behavior.
- YAML parsing edge cases.
- Schema validation failures.
- Lint rules.
- Git origin setup scenarios.

Scaffold packages may exist before implementation, but they should not contain
empty tests that make `go test ./...` look more complete than it is.

## Commit Messages

All commits must follow the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) specification.

Format:

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

Allowed types:

| Type | When to use |
|------|-------------|
| `feat` | New feature visible to users |
| `fix` | Bug fix |
| `docs` | Documentation only |
| `refactor` | Code change with no feature or fix |
| `test` | Adding or fixing tests |
| `chore` | Tooling, CI, dependencies, config |
| `perf` | Performance improvement |
| `build` | Build system or external dependencies |
| `ci` | CI configuration |

Rules:

- Use lowercase for type and description.
- Do not end the description with a period.
- Keep the description under 72 characters.
- Use the imperative mood: "add feature" not "added feature".
- Breaking changes must include `!` after the type/scope or a `BREAKING CHANGE:` footer.

Examples:

```
feat(parser): add support for multi-document YAML files
fix(executor): handle non-zero exit codes from gitlab-ci-local
chore: update devcontainer lock file
feat!: change pipeline source name format
```

## Dependencies

Prefer mature libraries for complex domains:

- YAML AST parsing and source positions.
- JSON Schema validation.
- CLI flag handling.
- XML/TAP report generation when appropriate.

New dependencies should be justified by clarity, correctness, or maintainability.
Avoid writing custom parsers for mature formats unless there is a specific,
documented reason.
