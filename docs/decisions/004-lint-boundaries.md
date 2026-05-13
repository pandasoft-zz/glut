# 004. Lint Boundaries

## Status

Accepted

## Context

The linter checks test files for errors before test execution. It reads raw
YAML but cannot render a GitLab CI pipeline. Pipelines can use include
directives, remote components, and input interpolation. These features make
cross-checking against pipeline content impossible without running
gitlab-ci-local.

Two approaches were considered for rendering-based validation:

- Run gitlab-ci-local during `glut run` and pass resolved job names to the
  semantic linter. This would work but creates a split: `glut lint` and
  `glut run` would behave differently for the same check.

- Run gitlab-ci-local during `glut lint` as well. This makes the lint command
  depend on external tooling and infrastructure, which breaks the expectation
  that lint is a fast, offline static check.

## Decision

The linter validates only the structure and semantics of the `.glut:` metadata
section. It does not cross-check values in `.glut:` against the pipeline YAML.

Checks that stay:

- JSON schema validation of the `.glut:` section.
- Unknown keys in `.glut:`.
- Missing `.glut.name`.
- Mutually exclusive `.glut.setup` fields (tag + branch).
- Missing `.glut.setup.merge_request` when source is merge_request_event.

Checks that are out of scope for the linter:

- Whether a job referenced in `.glut.assert.job` exists in the pipeline.
- Whether a job stage is defined in the pipeline stages block.
- Any other check that requires interpreting or rendering the pipeline YAML.

## Consequences

False positives from pipelines that use input interpolation, remote components,
or include directives are eliminated. The linter covers less ground but stays
within what it can reliably validate.

Future option: the runner already calls gitlab-ci-local and could compare
resolved job names against `.glut.assert.job` entries at runtime. This would
give accurate coverage without changing the lint command contract.
