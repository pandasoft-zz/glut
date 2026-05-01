# 001. Repository Language And Style

## Status

Accepted

## Decision

All committed repository content must be written in English.

The English should be simple technical English, roughly at A2 level. Prefer
short sentences, common words, and direct structure.

This includes:

- Code identifiers.
- Code comments.
- Documentation.
- Test fixtures and test names.
- Commit messages.
- Pull request descriptions.

Avoid complex idioms, literary phrasing, and long nested sentences. Documentation
should be easy to read for contributors who do not use English as their first
language.

## Rationale

GLUT is intended to be an open-source project. English repository content makes
the project easier to review, search, document, and share with contributors.

Simple technical English improves maintainability. It also makes AI-assisted
work safer because prompts, docs, and code comments use clear words with less
room for interpretation.

## Consequences

- Documentation added to `docs/` must be English.
- Code review can treat non-English committed text as a style issue unless it is
  a deliberate test fixture.
- Code review can also ask for simpler wording when documentation or comments
  become too complex.
