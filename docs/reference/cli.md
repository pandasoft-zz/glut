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
| `--interactive` | `-i` | | Select which tests to run in an interactive terminal prompt. |
| `--verbose` | `-v` | `GLUT_VERBOSE` | Print job output for passing tests, with styled output blocks. |
| `--quiet` | `-q` | | Print less progress output. |
| `--format <fmt>` | | `GLUT_FORMAT` | Console format. |
| `--report <fmt:path>` | | `GLUT_REPORT` | Write a report file. Can be repeated. |
| `--timeout <duration>` | | `GLUT_TIMEOUT` | Timeout for one test. Default is `10m`. |
| `--debug` | | `GLUT_DEBUG` | Keep more debug data. |
| `--keep-workspace` | | `GLUT_KEEP_WORKSPACE` | Keep the workspace after the run. |
| `--debug-pause <point>` | | | Pause at a debug point. Valid values: `before-pipeline`, `before-asserts`, `after-pipeline` (alias for `before-asserts`), `on-fail`. |
| `--keep-last-failed <n>` | | | Keep the last N failed workspaces. |
| `--include <dir>` | | | Copy only this subdirectory into the workspace. Can be repeated. |
| `--copy-strategy <strategy>` | | | Workspace copy strategy: `auto` (default), `rsync`, or `native`. |
| `--wait-timeout <duration>` | | `GLUT_WAIT_TIMEOUT` | Max time to wait for the Docker daemon to become ready. Default is `120s`. |
| `--docker-volume-strategy <strategy>` | | | Docker volume strategy: `auto` (default, detect), `bind` (native Linux), or `volume` (Docker Desktop/WSL2). |

Examples:

```bash
glut run -k release ./tests
glut run --fail-fast ./tests
glut run --interactive ./tests
glut run --report=junit:report.xml ./tests
glut run --report=junit:report.xml --report=tap:report.tap ./tests
glut run --debug --keep-workspace ./tests/release.yml
glut run --include tests ./tests/
glut run --include component --include shared ./tests/
```

### Interactive Mode

`--interactive` opens a terminal selection prompt before running any tests.

```bash
glut run --interactive ./tests
glut run -i ./tests
```

**Step 1 — choose a directory** (shown only when the path contains subdirectories):

```
Select test directory
> All directories
  artifact
  docker
  git
```

**Step 2 — choose a test** from the selected directory:

```
Select test to run
> All tests
  build image on feature branch
  build image with tag
  build image on merge request
```

Select a directory or test with the arrow keys and press Enter. Choosing
"All directories" or "All tests" runs every test in the current scope.

`--interactive` requires an interactive terminal. It returns an error when
run from a script or pipe.

You can combine `--interactive` with other flags:

```bash
glut run --interactive --verbose ./tests
glut run --interactive -k release ./tests
```

Supported report formats are `junit` and `tap`.

`GLUT_REPORT` can hold more than one report, separated by commas.

```bash
export GLUT_REPORT="junit:report.xml,tap:report.tap"
glut run ./tests
```

### Workspace Copy Performance

By default GLUT copies the entire current directory into an isolated workspace
before each test. For large repositories this can be the dominant cost of a
test run.

**`--include` — copy only what the pipeline needs**

```bash
glut run --include tests ./tests/
glut run --include component --include shared ./tests/
```

Use `--include` to restrict the copy to one or more subdirectories. The flag
can be repeated. Only those subdirectories are present in the workspace; all
other repository files are excluded. This is safe whenever your pipeline does
not reference files outside the listed directories.

**`--copy-strategy` — choose how files are copied**

| Strategy | Behaviour |
| --- | --- |
| `auto` | Try `rsync` first; fall back to native Go copy if rsync is not installed. This is the default. |
| `rsync` | Always use `rsync`. Fails if rsync is not installed. |
| `native` | Always use the built-in Go file copy. |

```bash
glut run --copy-strategy=rsync ./tests/
glut run --copy-strategy=native ./tests/
```

`rsync` is typically the fastest option on Linux and in Docker because it
transfers only the data and skips `.git`. Use `native` when rsync is not
available or on filesystems where rsync performs poorly.

### Debugging a Failing Test

**Inspect the workspace after failure:**

```bash
glut run --keep-last-failed 1 ./tests/release.yml
```

GLUT keeps the workspace of the most recent failure. When the workspace is
preserved, GLUT prints the path and useful inspection commands. The workspace
layout:

```
/tmp/glut-XXXXXXXX/
  workspace/          ← cloned repo where the pipeline ran
  .glut-origin.git    ← fake origin (bare repo)
  bin/                ← mock binary symlinks
  mock-logs/          ← one JSON file per mock binary with recorded calls
```

```bash
ls -la mock-logs/
git --git-dir=.glut-origin.git log --all --oneline
```

**Get more output during the run:**

```bash
glut run --debug --verbose ./tests/release.yml
```

`--debug` adds to the failure output: raw stdout/stderr from `gitlab-ci-local`,
recorded API calls (method, path, request body, status), binary call logs, git
history for both repos, and phase timings.

`--verbose` prints per-job stdout to the terminal during the run.

**Pause at a specific point:**

```bash
glut run --debug-pause on-fail ./tests/release.yml
```

| Pause value | When GLUT pauses |
| --- | --- |
| `before-pipeline` | Workspace is ready, pipeline has not started. |
| `before-asserts` | Pipeline finished, assertions not yet run. |
| `after-pipeline` | Alias for `before-asserts`. |
| `on-fail` | After assertions, only if the test failed. |

GLUT prints the workspace path and waits for Enter. Open a second terminal to
inspect the workspace while it is paused.

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

When no path is given, `glut lint` defaults to `./tests/` (not the current
directory, unlike `glut run` and `glut list`).

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

### JSON Output Format

```json
{
  "files": [
    {
      "file": "tests/release.yml",
      "issues": [
        {
          "file": "tests/release.yml",
          "line": 12,
          "level": "error",
          "category": "schema",
          "path": ".glut.setup.pipeline_source",
          "message": "glut schema: setup.pipeline_source: must be one of ..."
        }
      ]
    }
  ],
  "has_errors": true
}
```

| Field | Type | Notes |
| --- | --- | --- |
| `files[].file` | string | File path. |
| `files[].issues` | array | Empty array when no issues in this file. |
| `issues[].line` | integer | Source line number. Omitted when not applicable. |
| `issues[].level` | string | `"error"` or `"warning"`. |
| `issues[].category` | string | `"schema"`, `"semantic"`, or `"parse"`. |
| `issues[].path` | string | Dotted path to the offending field. Omitted for parse errors. |
| `issues[].message` | string | Human-readable description. |
| `has_errors` | boolean | `true` when any issue has `level: "error"`. |

`category` meanings:
- `schema`: fix key names, value types, or enum values in `.glut:`.
- `semantic`: fix cross-document errors, such as an `assert.job` key that names a job not in the pipeline.
- `parse`: fix YAML syntax, file structure, or unreadable files.

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
