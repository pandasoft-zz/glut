# AI Agent Instructions

This file is the main entry point for AI coding agents that work on this
repository.

## Repository Language

All committed repository content must be written in simple technical English.
Use short sentences, common words, and direct structure. Aim for A2-level
English when possible.

## Read First

Before implementation work, read these documents:

- [Architecture](docs/architecture.md)
- [Conventions](docs/conventions.md)
- [Gotchas](docs/gotchas.md)
- [Refactoring Guardrails](docs/requirements/refactoring-guardrails.md)

Also read the decision records:

- [001. Repository Language And Style](docs/decisions/001-repository-language.md)
- [002. Parser And Validation Strategy](docs/decisions/002-parser-and-validation.md)
- [003. Package Boundaries](docs/decisions/003-package-boundaries.md)

## Implementation Rules

- Follow the package boundaries from `docs/architecture.md`.
- Keep Cobra handlers thin. Use typed options and delegate real work.
- Keep parsing, schema validation, linting, orchestration, and execution
  separate.
- Use JSON Schema as the structural validator for the `glut:` section.
- Embed runtime schema assets into the final binary.
- Do not rewrite GitLab CI YAML through a generic map marshal/unmarshal cycle.
- Check and wrap errors from shell, git, filesystem, and process operations.
- Do not add empty passing tests.
- Prefer mature libraries for YAML AST work and JSON Schema validation.

## Current Project State

This repository is still in an early implementation phase. Some packages are
scaffolds. Treat the documentation above as guardrails for the next
implementation tasks.
