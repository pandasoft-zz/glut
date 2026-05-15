# Debugging a Failing Test

When a test fails, GLUT gives you several ways to find out why.

## Reading the Failure Output

A failing test prints which assert failed, what was expected, and what was
found:

```
FAIL  release from tag  (14.2s)
  assert.job["release"].exit-status: expected 0, got 1
  assert.binary["release-cli"].called: expected true, got false

1 failed, 0 passed
```

The path tells you the resource type (`job`, `binary`, `api`, `git`,
`artifacts`) and the field that did not match. Start there.

## The Workspace

GLUT keeps the workspaces of the last three failed tests by default. When a
workspace is kept, GLUT prints the path and some useful commands:

```
Test workspace preserved at: /tmp/glut-abc123/
  cd /tmp/glut-abc123/
  ls -la mock-logs/                    # call logs
  git --git-dir=.glut-origin.git log --all   # origin history
```

The workspace layout:

```
/tmp/glut-abc123/
  workspace/          ← cloned repo where the pipeline ran
  .glut-origin.git    ← fake origin (bare repo)
  bin/                ← mock binary symlinks
  mock-logs/          ← one JSON file per mock binary with recorded calls
```

### Inspect the Repository

```bash
cd /tmp/glut-abc123/

# workspace commits
git -C workspace log --all --oneline

# origin commits
git --git-dir=.glut-origin.git log --all --oneline

# a file in the origin
git --git-dir=.glut-origin.git show HEAD:manifest.yaml
```

### Inspect Mock Binary Calls

```bash
cat mock-logs/release-cli.json
```

Each entry records one call:

```json
[
  {
    "args": ["create", "--tag-name", "v1.2.0", "--name", "v1.2.0"],
    "cwd": "/tmp/glut-abc123/workspace",
    "stdin": "",
    "timestamp": "2026-05-13T10:00:00Z"
  }
]
```

### Inspect Artifacts

```bash
ls -la workspace/dist/
cat workspace/dist/image.txt
```

## More Output With `--debug`

```bash
glut run --debug ./tests/release.yml
```

`--debug` appends extra data to the failure output:

- Raw stdout and stderr from `gitlab-ci-local`.
- Recorded API calls: method, path, request body, status code, timestamp.
- Binary call logs for each mock binary.
- Git log for both the workspace and the origin.
- Phase timings: workspace setup, mock server, pipeline, assertions.

Useful when an `assert.api` check fails with "call not made" but you expect it
was — the API call list shows exactly what the mock server received.

## Verbose Job Output

```bash
glut run --verbose ./tests/release.yml
```

`--verbose` prints each job's stdout to the terminal while the pipeline runs.
Use it when the job exits with status 1 and you need to see what went wrong
without inspecting the workspace manually.

## Pausing With `--debug-pause`

```bash
glut run --debug-pause on-fail ./tests/release.yml
```

GLUT prints the workspace path and waits for you to press Enter before
cleaning up. Open a second terminal and inspect the workspace while it is
paused.

| Value | When GLUT pauses |
| --- | --- |
| `before-pipeline` | Workspace is ready, pipeline has not started. Use to verify git state and mock binaries. |
| `before-asserts` | Pipeline finished, assertions not yet run. Use to inspect what the pipeline produced. |
| `after-pipeline` | Alias for `before-asserts`. |
| `on-fail` | After assertions, only if the test failed. Use during iterative debugging. |

## Narrowing With `-k`

```bash
glut run -k "release from" ./tests/
```

Runs only tests whose name contains `release from`. Avoids running the full
suite while you work on one test.

## Controlling Workspace Retention

```bash
glut run --keep-last-failed 5 ./tests/
```

Keep the last 5 failed workspaces instead of the default 3.

```bash
glut run --keep-workspace ./tests/release.yml
```

Always keep the workspace, even when the test passes. Prints the path at the
end of the run.

## Common Failure Patterns

### "job not found in output"

The `assert.job` key does not match a real job name. Run `glut lint` — it
catches job name mismatches as a semantic error.

### "API call not made"

The job did not call the expected endpoint. Possible causes:

1. The job failed before reaching the API call — check the `exit-status` assert.
2. The job called a different path — use `--debug` and read the API call list.
3. The token was invalid — check `setup.api.token`.
4. The endpoint is not in the supported list and returned `404 Not Found`.

### Unexpected origin commit count

When using `git.origin.commands`, the origin starts with two commits: one
snapshot commit and one seed commit for the files from `git.origin.files`. Your
custom commands add more on top.

Use `{ ge: 2 }` rather than an exact count unless you are explicitly testing
commit history depth.

### Job exits with status 1 unexpectedly

Use `--verbose` to see job stdout in real time, or look in
`workspace/builds/<job-name>/` inside the preserved workspace for the full
log.
