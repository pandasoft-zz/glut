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
