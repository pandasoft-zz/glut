# Doctor

`glut doctor` analyses test files and returns lint issues, authoring hints,
and a job coverage summary. It is designed for use during test authoring and
for AI coding assistants that read structured feedback.

`doctor` does not run tests. It does not need Docker or `gitlab-ci-local`.

## Usage

```bash
glut doctor ./tests
glut doctor -k release ./tests
glut doctor --format=json ./tests/release.yml
```

## Flags

| Flag | Short | Meaning |
| --- | --- | --- |
| `--run <pattern>` | `-k` | Analyse only tests whose name contains the substring. |
| `--format <fmt>` | | Output format: `text` (default) or `json`. |

## Output Sections

### Lint Issues

`doctor` runs the same checks as `glut lint`. Issues are marked `ERROR` or
`WARNING`. Parse errors go to `stderr`. All other output goes to `stdout`.

### Hints

Hints point out authoring problems that are technically valid but produce
weak test coverage. Each hint has a path that names the relevant part of the
`.glut:` metadata document.

| Hint path | Trigger condition |
| --- | --- |
| `.glut.assert` | Assert block is entirely empty. |
| `.glut.assert` | More than half of job asserts check only exit status with no artifact, git, API, or binary asserts. |
| `.glut.assert` | `setup.tag` is set with no `assert.binary`, `assert.api`, or `assert.git`. |
| `.glut.assert.api` | `setup.pipeline_source` is `merge_request_event` but `assert.api` is missing. |
| `.glut.assert.api` | `setup.api` mock is configured but `assert.api` is missing. |
| `.glut.assert.api` | `setup.api.seed` has data but `assert.api` is missing. The hint names the seeded entity counts. |
| `.glut.assert.binary` | `setup.mocks.binaries` is configured but `assert.binary` is missing. The hint names the mock binaries. |
| `.glut.assert.git` | `setup.git` is configured but `assert.git` is missing. |
| `.glut.assert.job` | `setup.upstream` is configured but no job asserts exist. |
| `.glut.setup.schedule` | `setup.schedule` is configured. Reminder to cover scheduled-only behavior. |

### Coverage

Coverage shows how many pipeline jobs have at least one entry in `assert.job`.

Text output adds a `[COVERAGE]` line per file:

```
[COVERAGE] tests/release.yml: 1/2 jobs asserted
```

JSON output adds a `coverage` object to each file entry:

```json
"coverage": {
  "jobs_total": 2,
  "jobs_asserted": 1
}
```

## Text Output Format

```
[ERROR]    <file>: <message>
[WARNING]  <file>: <message>
[HINT]     <file>: <path>: <message>
[COVERAGE] <file>: <asserted>/<total> jobs asserted
```

Parse errors go to `stderr`. Everything else goes to `stdout`.

## JSON Output Format

```json
{
  "files": [
    {
      "file": "tests/release.yml",
      "issues": [
        {
          "file": "tests/release.yml",
          "level": "warning",
          "category": "semantic",
          "path": ".glut.assert",
          "message": ".glut.assert is empty"
        }
      ],
      "hints": [
        {
          "file": "tests/release.yml",
          "path": ".glut.assert.binary",
          "message": "Mock binaries are configured (release-cli), but no binary asserts exist. Add assert.binary checks for called tools and arguments."
        }
      ],
      "coverage": {
        "jobs_total": 2,
        "jobs_asserted": 1
      }
    }
  ],
  "has_errors": false
}
```

## Exit Codes

| Code | Meaning |
| --- | --- |
| `0` | No errors found. |
| `1` | At least one lint error found. |
| `2` | Doctor could not run (invalid flag, unreadable path). |

Hints and coverage do not affect the exit code.

## Example Output

Running `glut doctor ./tests` on a test suite with one release test and two
authoring gaps:

```
[WARNING]  tests/release.yml: .glut.setup.pipeline_source: unknown value "push-tag"
[HINT]     tests/release.yml: .glut.assert: More than half of job asserts check only exit-status. Add artifact, git, API, or binary asserts for stronger coverage.
[HINT]     tests/release.yml: .glut.assert.binary: Mock binaries are configured (release-cli), but no binary asserts exist. Add assert.binary checks for called tools and arguments.
[COVERAGE] tests/release.yml: 1/2 jobs asserted
```

Exit code is `0` because there are no errors — only warnings and hints.

## Acting on Hints

Hints do not block the pipeline and do not affect the exit code. Use this table
to decide how much to act on each one.

| Hint path | Act on it when... | Safe to ignore when... |
| --- | --- | --- |
| `.glut.assert` (empty) | Always. An empty assert block proves nothing. | Never. |
| `.glut.assert` (exit-status only) | Your pipeline has side effects: releases, pushes, API calls. | Your job only needs to pass — e.g. a compile-only job with no artifacts. |
| `.glut.assert` (tag, no binary/api/git) | Your tag pipeline creates a release or pushes a manifest. | Your tag pipeline only builds an image with no external effects. |
| `.glut.assert.api` (MR, no api assert) | Your pipeline calls the GitLab API (fetches MR labels, posts a comment). | Your pipeline reads `CI_MERGE_REQUEST_*` variables but makes no API calls. |
| `.glut.assert.api` (mock configured, no assert) | Always. You set up a mock but never checked whether it was called. | Never. |
| `.glut.assert.binary` | Always. You mocked a binary so you could assert on it. | Never. |
| `.glut.assert.git` | Your pipeline commits or pushes. | Your pipeline only reads git variables like `CI_COMMIT_SHA`. |
| `.glut.assert.job` (upstream, no job asserts) | Always. An upstream context is useful only if you verify the triggered job. | Never. |
| `.glut.setup.schedule` | You want a reminder to cover scheduled-only behavior. | You already have a separate test for scheduled behavior. |

Coverage below 100 % means some pipeline jobs have no asserts at all. Add at
least an `exit-status` check for any job that should run.

## Use With AI Tools

Run `glut doctor --format=json ./tests` and pass the output to a coding
assistant. The JSON structure gives the assistant file paths, issue categories,
hint paths, and coverage numbers so it can suggest targeted improvements.

Use `-k` to focus on a single test by name when a large test suite produces
too much output.
