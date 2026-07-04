// Enforces the Conventional Commits rules documented in
// docs/conventions.md#commit-messages. Keep the type list here in sync with
// the table there.
//
// subject-case uses config-conventional's default (forbid only fully
// sentence/start/pascal/upper-cased subjects) instead of a hard lower-case
// rule: real descriptions routinely contain identifiers and acronyms such as
// JUnit, Docker, or GLUT_WORK_DIR, and a strict lower-case rule would reject
// every one of them. header-max-length is 100 (the community default). The
// documented "under 72 characters" guidance applies to the description; when
// a series of long commits predates this gate, squash-merge the PR so only the
// final subject is linted.
module.exports = {
  extends: ['@commitlint/config-conventional'],
  rules: {
    'type-enum': [
      2,
      'always',
      ['feat', 'fix', 'docs', 'refactor', 'test', 'chore', 'perf', 'build', 'ci'],
    ],
    'type-case': [2, 'always', 'lower-case'],
    'subject-case': [
      2,
      'never',
      ['sentence-case', 'start-case', 'pascal-case', 'upper-case'],
    ],
    'subject-full-stop': [2, 'never', '.'],
    'header-max-length': [2, 'always', 100],
  },
};
