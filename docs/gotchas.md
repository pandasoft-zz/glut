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
