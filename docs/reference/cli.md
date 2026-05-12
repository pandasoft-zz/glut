# CLI

GLUT is a single binary named `glut`.

```bash
glut <command> [flags] [path...]
```

`path` can be a directory or a YAML file. If no path is given, GLUT uses the
current directory.

## `glut run`

Run tests.

```bash
glut run
glut run ./tests
glut run ./tests/release.yml
```

Flags:

| Flag | Short | Environment | Meaning |
| --- | --- | --- | --- |
| `--run <pattern>` | `-k` | | Run tests with names that match a substring or regex. |
| `--fail-fast` | `-x` | `GLUT_FAIL_FAST` | Stop after the first failed test. |
| `--maxfail <n>` | | | Stop after N failed tests. |
| `--verbose` | `-v` | `GLUT_VERBOSE` | Print more job output. |
| `--quiet` | `-q` | | Print less progress output. |
| `--format <fmt>` | | `GLUT_FORMAT` | Console format. |
| `--report <fmt:path>` | | `GLUT_REPORT` | Write a report file. Can be repeated. |
| `--timeout <duration>` | | `GLUT_TIMEOUT` | Timeout for one test. Default is `10m`. |
| `--debug` | | `GLUT_DEBUG` | Keep more debug data. |
| `--keep-workspace` | | `GLUT_KEEP_WORKSPACE` | Keep the workspace after the run. |
| `--debug-pause <point>` | | | Pause at a debug point. |
| `--keep-last-failed <n>` | | | Keep the last N failed workspaces. |

Examples:

```bash
glut run -k release ./tests
glut run --fail-fast ./tests
glut run --report=junit:report.xml ./tests
glut run --report=junit:report.xml --report=tap:report.tap ./tests
glut run --debug --keep-workspace ./tests/release.yml
```

Supported report formats are `junit` and `tap`.

`GLUT_REPORT` can hold more than one report, separated by commas.

```bash
export GLUT_REPORT="junit:report.xml,tap:report.tap"
glut run ./tests
```

## `glut list`

List tests without running them.

```bash
glut list
glut list ./tests
glut list -k release ./tests
```

Flags:

| Flag | Short | Meaning |
| --- | --- | --- |
| `--run <pattern>` | `-k` | List tests with names that match a substring or regex. |

## `glut lint`

Run static checks.

```bash
glut lint
glut lint ./tests
glut lint ./tests/release.yml
glut lint --format=json ./tests/release.yml
```

Lint checks include:

- YAML syntax.
- `.glut:` schema validation.
- Missing `.glut.name`.
- Empty `assert:`.
- `assert.job` references to missing jobs.
- Pipeline jobs with stages that are not in `stages:`.
- Invalid setup combinations, such as `branch` and `tag` together.

`glut lint` exits with code `1` when it finds an error.

Use `--format=json` when another tool or AI assistant needs structured output.
The JSON output groups issues by file and marks each issue as `schema`,
`semantic`, or `parse`.

## `glut doctor`

Explain tests for AI tools.

```bash
glut doctor ./tests
glut doctor -k release ./tests
glut doctor --format=json ./tests/release.yml
```

Flags:

| Flag | Short | Meaning |
| --- | --- | --- |
| `--run <pattern>` | `-k` | Analyse only tests with names that match a substring. |
| `--format <fmt>` | | Output format: `text` (default) or `json`. |

`doctor` returns the same lint issues as `lint`. It also returns authoring
hints and a job coverage summary for each file.

**Hints** point out weak or incomplete tests. Examples:

- Most job asserts check only exit status — add artifact, git, API, or binary asserts.
- A tag pipeline test has no release API or binary assert.
- Mock binaries are configured but `assert.binary` is missing — the hint lists the binary names.
- A git setup is present but `assert.git` is missing.
- A scheduled or upstream-triggered pipeline has no job asserts.
- API seed data is configured but `assert.api` is missing.
- The assert block is empty entirely.

**Coverage** shows how many pipeline jobs have at least one `assert.job` entry.
Text output adds a `[COVERAGE]` line per file. JSON output adds a `coverage`
object with `jobs_total` and `jobs_asserted`.

Use `--format=json` when a coding assistant should read the result and edit the
test. Parse errors go to `stderr`. All other output goes to `stdout`.

## `glut version`

Print the GLUT version.

```bash
glut version
```

## Help

Every command has built-in help.

```bash
glut --help
glut help run
glut run --help
glut lint --help
```

## Exit Codes

| Code | Meaning |
| --- | --- |
| `0` | The command succeeded. |
| `1` | At least one test or lint check failed. |
| `2` | GLUT could not run because of an input, setup, or internal error. |
