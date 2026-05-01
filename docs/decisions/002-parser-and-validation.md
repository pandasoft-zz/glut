# 002. Parser And Validation Strategy

## Status

Accepted

## Decision

GLUT should use mature libraries for YAML parsing and JSON Schema validation.
It should not implement a custom YAML grammar.

The parser should operate on a YAML syntax tree, extract the top-level `glut:`
section, decode that section into typed Go config, and preserve the remaining
pipeline YAML for `gitlab-ci-local`.

JSON Schema is the authoritative structural validator for the `glut:` section.
Go lint rules remain responsible for semantic checks that require cross-field or
cross-document knowledge.

The schema must be embedded into the compiled binary.

## Rationale

YAML is a complex format. GitLab CI files often use anchors, aliases, includes,
extends, and other patterns that are easy to damage with simplistic parsing.

JSON Schema is well suited for:

- Object structure.
- Types.
- Required fields.
- Enums.
- Additional property rules.
- Local constraints.

It is less suited for checks such as whether `assert.job` references an existing
pipeline job. Those checks belong in Go lint rules.

Embedding the schema preserves GLUT's single-binary distribution model.

## Consequences

- `schema/glut.schema.json` must be maintained as a real product artifact.
- Parser tests should include YAML syntax that could be broken by map-based
  re-encoding.
- Runtime validation must not depend on schema files existing next to the binary.
- Adding schema validation may introduce a dependency on a maintained JSON Schema
  library.

