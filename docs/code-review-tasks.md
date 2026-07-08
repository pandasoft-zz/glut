# Code Review Tasks

Full-repository code review performed on 2026-07-02 (branch `fix/dind-volume-strategy-autodetect`).
Findings are grouped by severity. Every critical and high finding was verified
against the code by a second pass. Line numbers refer to the state of the
working tree at review time.

Areas: `[cli]` cmd/glut + parser + config + schema, `[workspace]` internal/workspace,
`[runner]` internal/runner + executor + docker, `[mock]` internal/mockserver + mockwrapper,
`[assert]` internal/asserter + reporter, `[build]` build/CI/docs/tests.

## Critical

- [x] **[runner] Kill the whole process tree on timeout and bound the pipe-drain wait** (`internal/executor/executor.go:228`)
  `exec.CommandContext` + `cmd.Run()` kills only the `gitlab-ci-local` process on deadline, not its children (shell jobs, `docker` clients). Orphans survive the "timeout", and because `cmd.Stdout`/`cmd.Stderr` are `bytes.Buffer`s, `cmd.Wait()` blocks until all pipe writers close — a grandchild holding the pipe makes `Run` hang forever, so the configured timeout is silently ineffective. The existing timeout test only covers a direct child.
  Fix: set `cmd.WaitDelay`, and use `SysProcAttr{Setpgid: true}` with a custom `cmd.Cancel` that signals the negative pgid (with a Windows fallback).

## High

- [x] **[runner] Fix nil-pointer panic in debug defer when workspace creation fails** (`internal/runner/runner.go:448`)
  The deferred debug block calls `safeGitLog(work.WorkspaceDir)` unconditionally. `workspace.New` returns `nil` on error, so `--debug` plus a workspace setup failure panics inside the defer and crashes the run. The block at line 428 guards `work != nil`; this one does not.
  Fix: guard the `WorkspaceGitLog`/`OriginGitLog` fields with `if work != nil`.

- [x] **[runner] Do not treat SIGINT/context cancellation as a successful run** (`internal/executor/executor.go:118`)
  The error path checks only `context.DeadlineExceeded`. On Ctrl-C the context is `Canceled`, and if any job output was already parsed, `if len(result.Jobs) > 0 { return result, nil }` swallows the interruption. Assertions then run against a half-executed pipeline and the test can report PASSED after a user abort.
  Fix: check `runCtx.Err() != nil` (both Canceled and DeadlineExceeded) before the early-nil return and propagate a distinct "interrupted" error.

- [x] **[runner] Abort the test loop when the run context is cancelled** (`internal/runner/runner.go:179`)
  `Run` never checks `ctx.Err()` between tests (verified: no `ctx.Err()` call exists in runner.go). After SIGINT every remaining test still builds a workspace, git origin, and mock server, then fails with a confusing per-test error. The retry-pause `select` even falls through on `<-ctx.Done()` and proceeds with the retry on a dead context.
  Fix: check `ctx.Err()` at the top of the loop and after the retry pause; skip the retry when the context is done.

- [x] **[mock] Fix dead auto-increment — every created resource gets id/iid 0** (`internal/mockserver/store.go:93`)
  `Create` merges into `defaultObject(resource)`, which pre-seeds `"id": 0` / `"iid": 0` for every resource. `setDefaultIdentifierLocked` returns early when the key already exists, so the auto-increment branch is unreachable. Every MR/issue/pipeline/job created via POST gets id 0; duplicates make GET/PUT/DELETE by id ambiguous (`findLocked` always returns the first match).
  Fix: treat identifier value `0` as unset in `setDefaultIdentifierLocked`, or stop pre-seeding ids in `defaultObject`. Add a test asserting incrementing ids across two POSTs.

- [x] **[cli] Advertised lint rule "assert.job references missing pipeline jobs" is not implemented** (`cmd/glut/root.go:152`, `internal/parser/lint.go:13`)
  The `lint` help promises detection of assert.job references to missing pipeline jobs. No such check exists; `Lint` discards the parsed pipeline document. A typo in an assert.job name passes lint and only surfaces as a confusing run-time failure. conventions.md lists this check as a Go lint responsibility.
  Fix: implement the check using the already-parsed `pipelineRoot` (skip files with dynamic jobs from `include:`/inputs), or remove the claim from the help text.

- [x] **[build] Coverage gate in CI is masked by `tee` and never enforced** (`.github/workflows/ci.yml:54`)
  `make test-cover-check | tee ...` runs under the default `bash -e {0}` shell, which has no `pipefail`. If `check-coverage.sh` exits non-zero, `tee` exits 0 and the step passes. The 90% threshold is not enforced in CI at all.
  Fix: add `shell: bash` to the step (enables pipefail) or `set -o pipefail` as the first line.

- [x] **[build] skill/SKILL.md documents the opposite of the actual Docker default** (`skill/SKILL.md:560`)
  SKILL.md says GLUT runs without Docker by default and `docker: true` enables it. The code (`internal/runner/runner.go:1088` `resolveDockerMode`), the schema, and test-format.md all say the default is Docker ON. AI assistants consuming the skill generate wrong tests.
  Fix: rewrite the "Docker Executor" section to match reality: default is Docker mode; `docker: false` forces shell.

- [x] **[assert] Check bufio.Scanner errors and raise the line limit in scanLines** (`internal/asserter/patterns.go:52`)
  `scanLines` uses the default 64 KB token limit and never checks `scanner.Err()`. One long log line silently drops all following lines: positive patterns fail wrongly, and negated patterns (`!/panic/`) pass wrongly — a silent false pass of a safety assertion. `parseDotenv` (`internal/asserter/report.go:275`) has the same pattern.
  Fix: raise the buffer via `scanner.Buffer` (or split on `\n`) and surface `scanner.Err()` as an assertion failure.

- [x] **[assert] Fail size/md5/sha256/report asserts on git origin files instead of ignoring them** (`internal/asserter/git.go:152`)
  `runBareGitFileAssert` evaluates only `exists`, `contents`, `mode`, and `filetype`. `size`, `md5`, `sha256`, and `report` are silently dropped, so those assertions pass vacuously — while assert-syntax.md documents them for git `file` entries.
  Fix: implement the fields for bare repos (hash/measure the blob from `git show`), or emit an explicit "unsupported field" failure.

## Medium

### Runner and executor

- [x] **[runner] Infra-retry heuristic never fires for tests with `present`/`when` assertions** (`internal/runner/runner.go:205`)
  The retry condition requires `len(testResult.JobOutputs) == 0`, but when `needsJobList` is true the outputs are pre-populated from `executor.ListJobs` before the pipeline runs, so a daemon-level failure never retries — exactly the DinD flakes the heuristic targets.
  Fix: track "executor produced job output" with a dedicated flag instead of inferring it from `result.JobOutputs`.

- [x] **[runner] Docker-wait failure discards already-completed test results** (`internal/runner/runner.go:185`)
  When `docker.Wait` fails mid-suite, `Run` returns a fresh `RunResult{Error: ...}` — results of tests that already ran are thrown away and `sink.Summary` is never called.
  Fix: set `result.Error`, break the loop, and fall through to the normal summary/exit-code path.

- [x] **[runner] Volume pruning matches by substring and can delete foreign or concurrent volumes** (`internal/docker/wait.go:163`)
  `docker volume ls --filter name=glut-` is a substring match, so a user volume named `my-glut-data` is deleted; no `dangling=true` filter is applied, so volumes of a concurrently running glut process can be removed between populate and job start.
  Fix: add `--filter dangling=true` and match the exact `glut-...` naming pattern against listed names.

- [x] **[runner] Cleanup errors produce Passed=true tests with non-nil Error** (`internal/runner/runner.go:437`)
  `result.Passed` is computed before the defer promotes a cleanup error into `result.Error`, so the suite counts the test as passed and exits 0 while the result carries an error.
  Fix: recompute `result.Passed` in the defer, or decide (and document) that cleanup errors do not fail tests and keep them out of `Error`.

- [x] **[runner] gitOutput uses CombinedOutput as the parsed value** (`internal/runner/runner.go:911`)
  Trimmed stdout+stderr becomes `CI_COMMIT_SHA`, the commit message, and the timestamp. Any git stderr chatter (warnings from user gitconfig) corrupts CI variables.
  Fix: use `cmd.Output()` and keep stderr only for error messages.

- [x] **[runner] monitor.stop() races log capture — cancel can truncate the last job's logs** (`internal/executor/executor.go:105`, `internal/executor/dockerlogs.go:106`)
  `stop()` cancels the shared `watchCtx` before `collectLogs()`; a `docker logs --follow` capture still draining the final job is killed and the job's output silently disappears.
  Fix: split the contexts — stop only the events watcher in `stop()`, cancel capture goroutines after `collectLogs` returns.

- [x] **[runner] Fragile, version-coupled parsing of gitlab-ci-local output** (`internal/executor/executor.go:31,490,564`)
  Exit codes and job outputs are recovered by regex over human-oriented gcl text ("finished in", PASS/FAIL summary, "still running..."). A gcl upgrade that rewords these lines silently degrades exit-status mapping. `parseJobListJSON` also fails if any diagnostic line follows the JSON array (`json.Unmarshal` rejects trailing data).
  Fix: use `json.NewDecoder(...).Decode` for the list (tolerates trailing data); verify the gcl version at startup and fail loudly when the finished-line regex matches nothing.
  Done: switched `parseJobListJSON` to `json.NewDecoder(...).Decode`, which tolerates a diagnostic line after the array (regression test added).
  Follow-up done (2026-07-04): investigated gitlab-ci-local 4.72.0's CLI — it has
  **no machine-readable run output** (only `--list-json` for the job list), so text
  parsing cannot be replaced outright. Instead the parsing now fails loud instead
  of silently degrading: `JobOutput.StatusKnown` tracks whether an exit status was
  actually recovered from a status line (or GLUT_JOB marker), and `executor.Run`
  errors out when job output was captured but no status line matched — the exact
  scenario that would otherwise default every ExitStatus to 0 and produce false
  PASSes. The error message includes the installed gitlab-ci-local version next to
  `config.TestedGCLVersion` (kept in sync with the Dockerfile's GCL_VERSION). The
  non-zero-exit path was tightened the same way: output lines alone no longer count
  as "a job failed on its own merits". Job stdout already has a robust file-based
  channel (`.gitlab-ci-local/output/*.log` via mergeJobLogs); exit codes have no
  such channel in gcl 4.72.0.

- [x] **[runner] Split runSingleTest (~360 LOC) into phase functions** (`internal/runner/runner.go:374`)
  One function handles 10+ phases coordinated through a mutable named return and a 40-line defer — the direct cause of the nil-guard bug above. Conventions require small single-responsibility functions.
  Fix: extract `setupTestEnv`, `buildExecConfig`, `collectDockerResults`, and a phase-timer helper; keep one thin orchestrating function owning a single defer.
  Done: the single-test execution moved to `internal/runner/testrun.go`. A `testRun`
  struct carries per-test state; `runSingleTest` is now a thin orchestrator that
  runs named phase methods (`createWorkspace`, `startMockServer`,
  `setupDockerVolumeAndMocks`, `buildExecConfig`, `listJobs`, `runPipeline`,
  `collectMockLogs`, `fetchGCLArtifacts`, `resolveOriginSource`, `runAsserts`)
  and owns the single cleanup defer (`finalize`). A `timePhase` helper replaces
  the repeated stopwatch code, and `recordErr` centralizes the
  `if err != nil && primaryErr == nil` first-error pattern. The suite-level
  state (volume strategy, pending volume cleanup, preserved workspaces) moved
  into a `suiteRun` struct; the Docker-readiness wait (`ensureDockerReady`) and
  the infra-retry policy (`runTestWithRetry` + `shouldRetryInfraFailure`) were
  extracted from `Run` as well. Behavior-preserving: error wrapping, phase
  timings, defer ordering, and retry semantics are unchanged, and the full
  runner test suite passes.

- [x] **[runner] Delete or rewrite tautological TestResolvePrivileged** (`internal/runner/runner_test.go:1044`)
  The test re-implements the production expression inline (`got := c.input != nil && *c.input`) and calls no production code — it can never fail. Conventions forbid tests that do not assert behavior.
  Fix: assert through behavior (fake gcl script checking for `--privileged` in `$@`) or drop the test.

- [x] **[runner] Critical paths untested: dockerlogs.go, retry, interrupt, realistic gcl output** (`internal/executor/dockerlogs.go`, `internal/runner/runner_test.go`)
  (a) dockerlogs.go (199 LOC of concurrency) has zero tests. (b) No test triggers the Docker infra-retry path. (c) No test covers context cancellation mid-suite or a timeout with a grandchild holding the pipe. (d) All runner-level tests use a synthetic `GLUT_JOB|` marker protocol, so the real `parseGitLabOutput` path is never exercised end-to-end.
  Fix: add unit tests for `collectLogs`/`containerInfo` with a fake docker CLI on PATH; add a retry-path test; add one end-to-end test with realistic gcl-formatted output.

### Mock infrastructure

- [x] **[mock] Stop coercing numeric identifiers to strings on PUT** (`internal/mockserver/store.go:64`)
  `Update` overwrites the identifier with the raw URL path string, so after `PUT .../pipelines/10` the object has `"id": "10"` (string) instead of a number. Clients decoding into an int fail.
  Fix: do not overwrite the identifier on update, or preserve the stored value's type.

- [x] **[mock] Fix check-then-act race in note and commit-status ID generation** (`internal/mockserver/server.go:452,516,591`)
  POST handlers compute `id := len(existing)+1` from an RLock snapshot, then insert under a separate Lock. Two jobs posting concurrently get duplicate ids.
  Fix: assign the id inside the locked region (e.g. `addNote` returns the id computed under `notesMu`).

- [x] **[mock] Record the query string as CONTRIBUTING.md requires** (`internal/mockserver/recorder.go:9`, `internal/mockserver/server.go:161`)
  `APICall` has no query field and `record()` stores only the path, so assertions cannot distinguish `GET /pipelines?ref=main` from `?ref=dev`. CONTRIBUTING.md says method, path, query, and body must be recorded.
  Fix: add a `Query` field populated from `r.URL.RawQuery` and expose it to the asserter.

- [x] **[mock] Bound request-body buffering; keep git pack payloads out of the recorder** (`internal/mockserver/server.go:143`)
  The recording middleware does `io.ReadAll(r.Body)` with no limit for every request, including git smart HTTP — a packfile (potentially hundreds of MB) is buffered twice and retained for the whole run. No `http.MaxBytesReader` anywhere.
  Fix: route git HTTP before the recorder (or skip body capture for `*.git/` paths) and cap API body capture.

- [x] **[mock] Mitigate network exposure of the 0.0.0.0-bound mock server** (`internal/mockserver/server.go:69`, `internal/mockserver/githttp.go:34`)
  The server deliberately binds 0.0.0.0 (Docker siblings must reach it), but `authorized()` accepts any non-empty token, `ExpiresAt` is never checked, and git HTTP auth uses the hard-coded constant `mock-job-token`. On a shared network any host can clone/push the code under test and mutate mock state during a run.
  Fix: bind to the Docker bridge interface (default to loopback when Docker is unused) and/or generate a per-run random job token.
  Done: `Server.Start(allInterfaces bool)` binds loopback-only whenever `setup.docker: false` (no job can ever need container reachability), keeping the prior universal 0.0.0.0 bind for the default/Docker case. Deferred: tightening `authorized()`/git-token semantics is intentionally permissive by design (tests use arbitrary token literals with no secret coordination) — turning that into a validated-secret model is a larger, more invasive redesign and was left out of this pass.

- [x] **[mock] Raise the bufio.Scanner limit in ReadBinaryLogs to match the unbounded writer** (`internal/mockwrapper/wrapper.go:213`)
  `appendBinaryCall` writes JSONL lines containing full captured stdin (unbounded); `ReadBinaryLogs` reads with the default 64 KB scanner limit. Any mocked binary receiving >64 KB on stdin makes the whole log unreadable and errors the test.
  Fix: call `scanner.Buffer` with a limit at least as large as the writer can produce, or truncate captured stdin at write time.

- [x] **[mock] Avoid draining stdin before exec — wrapper can hang where the real binary would not** (`internal/mockwrapper/wrapper.go:266`)
  `readStdin` does `io.ReadAll` on pipes before launching the real binary. A producer that keeps the pipe open (e.g. `tail -f x | tool`) blocks the wrapper forever; the unmocked binary would run fine.
  Fix: tee stdin into a capped buffer while feeding `cmd.Stdin` directly, letting the real binary drive consumption.

### CLI and parsing

- [x] **[cli] Report-write error is silently swallowed in interactive mode** (`cmd/glut/interactive.go:63`)
  `writeFileReports` failure returns `ExitRunnerError` without printing the error or setting `result.Error` — `glut run -i` exits non-zero with no message.
  Fix: set `result.Error = werr` (or call `writeError`) before returning, matching the non-interactive path.

- [x] **[cli] extractPipelineJobNames breaks on job names containing a colon, corrupting doctor coverage** (`cmd/glut/lint_report.go:288`)
  Job names are extracted by cutting at the first colon, so `build:image` becomes `build`. Combined with unioning assert.job names into the set, coverage numbers are wrong and a typo'd assert name inflates both numerator and denominator.
  Fix: derive job names from the parsed pipeline mapping instead of line-scanning re-rendered YAML.

- [x] **[cli] Duplicate test-file discovery logic in parser.ParseDir and runner.discoverTests** (`internal/parser/parse_dir.go:33`, `internal/runner/runner.go:277`)
  Two independent walkers already diverge (walk-error handling, sorting), and neither skips `.git` or `.glut-tmp*`, so `glut lint .` descends into stale workspace copies and produces phantom results.
  Fix: consolidate discovery into one parser function with an explicit skip list; use it from both lint and runner.
  Done: added `parser.SkipDiscoveryDir` (matches `.git` and `.glut-tmp*`) and applied it in both walkers, closing the phantom-results bug. Deferred: fully merging the two walkers into one function is a larger change given their differing feature sets (schema validation + pattern matching in `discoverTests` vs. plain parse-and-collect in `ParseDir`) and was left out of this pass.

- [x] **[cli] RunOptions → runner.RunOptions field mapping duplicated in two places** (`cmd/glut/root.go:96`, `cmd/glut/interactive.go:45`)
  The ~15-field translation is copy-pasted in the normal and interactive paths; a flag wired into one and forgotten in the other compiles fine and silently drops the option in one mode.
  Fix: extract a single `toRunnerOptions` helper used by both call sites.

### Workspace

- [x] **[workspace] Honor the port embedded in GLUT_COMPONENTS_SERVER over CI_SERVER_PORT** (`internal/workspace/components.go:54`)
  Inside a real CI job `CI_SERVER_PORT` is always set, so `GLUT_COMPONENTS_SERVER=host:8443` silently produces port 443 URLs — the documented host:port override is broken exactly where overrides are used. The unit test masks this by omitting `CI_SERVER_PORT`.
  Fix: let an explicit port in `GLUT_COMPONENTS_SERVER` override `CI_SERVER_PORT`; only `GLUT_COMPONENTS_PORT` ranks higher. Add `CI_SERVER_PORT` to the test env.

- [x] **[workspace] Record stdin in the Docker shell mock wrapper** (`internal/workspace/dockervolume.go:85`)
  The shell wrapper baked into the Docker volume hardcodes `"stdin":""` while the native Go wrapper captures real stdin. The same stdin assertion behaves differently between `docker: false` and `docker: true`.
  Fix: capture stdin in the shell wrapper (JSON-escaped, piped on to the real binary), or document and lint-reject stdin assertions for Docker jobs.

### Assertions and reporting

- [x] **[assert] Do not ignore xml.Unmarshal error in parseJUnit** (`internal/asserter/report.go:165`)
  The error is discarded; a truncated JUnit file leaves `doc.Suites` partially filled and counts are computed from the truncated document, so a corrupt report can satisfy `tests:`/`failures:` assertions.
  Fix: capture the error and return it as a "valid JUnit XML" failure unless it is the wrong-root-element case.

- [x] **[assert] Unify regex/pattern semantics between scalar values and pattern lists** (`internal/asserter/matcher.go:42`)
  A `/regexp/` in scalar position matches the whole multi-line text as one line, while the same pattern in a list matches per line — `contents: "/^a$/"` fails where `contents: ["/^a$/"]` passes. The bare `!text` negation is also undocumented, so literal text starting with `!` silently inverts.
  Fix: run scalar special-string matching through the same per-line scan as lists (or document the difference); document `!text`.

- [x] **[assert] Document or fix bare-list subset matching and duplicate reuse** (`internal/asserter/matcher.go:61,157`)
  A bare YAML list matches as a subset, not equality (`args: ["--version"]` passes against `["--version", "--force"]`), and duplicated expected items match a single actual element. Docs call bare values "exact match". `contain-elements` has the same reuse flaw.
  Fix: make bare lists deep-equal (or document subset semantics) and use a used-flags bipartite match as `consist-of` already does.

- [x] **[assert] Prevent `not` from passing on matcher configuration errors** (`internal/asserter/matcher.go:278`)
  An invalid regex or semver constraint returns a failed state, which `not` inverts into a pass: `not: {match-regexp: "("}` always passes and masks a broken test file.
  Fix: distinguish matcher errors from mismatches in `matchState` and propagate errors through `not`/`or`/`and` as failures.

- [x] **[assert] Count failed testcases, not failure elements, in JUnit report attributes** (`internal/reporter/junit.go:133`)
  `suite.Failures++` runs once per assertion failure, so one test with 5 failed assertions yields `tests="1" failures="5"` — inconsistent totals in GitLab's test UI.
  Fix: increment `suite.Failures` at most once per testcase; optionally merge assertion details into one `<failure>` element.

- [x] **[assert] Escape TAP descriptions and YAML diagnostic messages** (`internal/reporter/tap.go:52`)
  `#` in a test name starts an unintended TAP directive; unquoted `message: %s` with `: `, quotes, or `\r` produces invalid YAML diagnostics. Failure messages embed raw job output, so these characters are realistic.
  Fix: escape `#` and strip CR/LF in descriptions; YAML-quote the message.

### Build, CI, and docs

- [x] **[build] Enable conventions-relevant linters** (`.golangci.yml:3`)
  Only errcheck, govet, staticcheck, unused are enabled. The project's own conventions mandate wrapped contextual errors (`errorlint` enforces this), the tool shells out and serves HTTP (`gosec`, `bodyclose`), and the repo mandates simple English (`misspell`).
  Fix: enable at least `errorlint`, `gosec`, `bodyclose`, `nilerr`, `misspell`, `revive`; triage findings once.

- [x] **[build] Run tests with the race detector** (`Makefile:9`, `.github/workflows/ci.yml:39`)
  No `-race` anywhere, although `covermode=atomic` (the race-compatible mode) signals the intent. The runner/executor/mockserver code is goroutine-heavy — races are the realistic failure mode.
  Fix: add `-race` to `make test` and to the CI gotestsum invocation.

- [x] **[build] CI hard-fails on forked PRs because build-image pushes to ghcr** (`.github/workflows/ci.yml:79`)
  `build-image` runs on `pull_request` and pushes with `GITHUB_TOKEN`; forked-PR tokens are read-only, so the push and the dependent `integration-test` job fail. Only `gitlab-integration` has a fork guard.
  Fix: guard `build-image`/`integration-test` for forks, or build with `push: false` + `load: true` on fork PRs.

- [x] **[build] cli.md missing `--wait-timeout` and `--docker-volume-strategy` flags** (`docs/reference/cli.md:24`)
  Both flags exist in `cmd/glut/root.go` (the latter added by the current DinD work and used by .gitlab-ci.yml) but are absent from the flag table, so the override that gotchas.md recommends is undiscoverable.
  Fix: add both flags with env vars and value enums (`auto|bind|volume`).

- [x] **[build] gotchas.md contradicts itself on bind-mounts** (`docs/gotchas.md:133` vs `docs/gotchas.md:236`)
  The older section says GLUT "never" uses bind-mounts for Docker jobs; the newer auto-detection section says bind is used on native Linux. The older section is stale.
  Fix: update the older section (bind-mounts are unreliable *inside containers*) and cross-reference the auto-detection section.

- [x] **[build] Published GitLab CI examples omit `entrypoint: [""]` and will fail on real runners** (`docs/getting-started/installation.md`)
  The image entrypoint is `glut`, so the documented `image: ghcr.io/...` jobs pass the runner's shell script to the glut binary. The repo's own .gitlab-ci.yml sets `entrypoint: [""]`; the user docs do not.
  Fix: add `image: { name: ..., entrypoint: [""] }` to both documented variants.

- [x] **[build] Docs toolchain inconsistent — mermaid never renders, wrong theme/package names** (`.github/workflows/docs.yml:33`, `mkdocs.yml:4`, `README.md:72`)
  docs.yml installs `mkdocs-terminal` (unused — theme is readthedocs) and `mkdocs-mermaid2-plugin` (not enabled in mkdocs.yml plugins, so the mermaid block in index.md renders as plain code). README tells contributors to install `mkdocs-readthedocs`, which is not a real package.
  Fix: add `mermaid2` to mkdocs plugins, drop the unused theme package, pin mkdocs, fix the README install line.

- [x] **[build] Add .dockerignore and a dependency-layer cache to the builder** (`Dockerfile:10`)
  `COPY . .` ships the whole worktree (`.git`, local `glut` binary, `.glut-tmp*`) as build context and busts the module cache on every source change.
  Fix: add a `.dockerignore` and split the builder into a `COPY go.mod go.sum` + `go mod download` layer before `COPY . .`.

- [x] **[build] skill/SKILL.md stale on assert fields and API seed resources** (`skill/SKILL.md:251,350,372`)
  The seed list omits milestones, issues, variables, hooks, tags, branches, environments, deployments, pipelines, jobs; the job assert list omits `when`; the artifact assert list omits `report` (the structured-report feature).
  Fix: regenerate the cheat-sheet lists from the schema; add short `when:` and `report:` examples.

## Low

### Runner and executor

- [x] **[runner] Docker networking/plumbing logic lives in runner instead of docker/executor** (`internal/runner/runner.go:1004`)
  Bridge-IP discovery, `--extra-host` construction, and volume-mount building are Docker-domain concerns per architecture.md; `executor/dockerlogs.go` also imports `internal/workspace` just for a shared name constant.
  Fix: move the helpers into `internal/docker` and the GCL volume-name vocabulary into a shared package.
  Done: `dockerVolumes`/`dockerExtraHosts`/`outboundIP` moved to
  `internal/docker/jobnet.go` as `VolumeMounts`/`ExtraHosts`/`OutboundIP` (with
  unit tests), and the GCL volume-name codec (`GCLJobName`) moved to
  `internal/docker/gclvolume.go`. `executor/dockerlogs.go` now imports
  `internal/docker` instead of `internal/workspace`, and workspace's artifact
  fetch uses the same shared codec — one source of truth, no cross-boundary
  import for vocabulary.

- [x] **[runner] docker CLI helpers ignore HostEnv / DOCKER_HOST abstraction** (`internal/docker/wait.go:82,163`, `internal/executor/dockerlogs.go:61`)
  gcl gets `DOCKER_*` forwarded from `cfg.HostEnv`, but `docker.Endpoint()`, the readiness check, log monitor, and volume prune use the raw process env — with a custom HostEnv they talk to a different daemon than gcl.
  Fix: thread the resolved DOCKER_* values into `docker.Wait`, `PruneOrphanedVolumes`, and the dockerlogs commands.

- [x] **[runner] Docker output monitor runs uselessly in bind-mount strategy** (`internal/executor/executor.go:98`, `internal/executor/dockerlogs.go:177`)
  The monitor starts whenever `DockerVolumes` is non-empty; in bind strategy the "volume name" is a host path that can never match, so a `docker events` subprocess plus per-container inspects run for the whole pipeline and capture nothing.
  Fix: pass an explicit `MonitorVolume` field set only for the volume strategy instead of inferring from the mount spec.

- [x] **[runner] ListJobs and Run each consume a full Timeout budget** (`internal/executor/executor.go:91,136`)
  Tests with `present:`/`when:` assertions run gcl twice, each with its own `cfg.Timeout`, so `--timeout 60s` can take ~120s per test.
  Fix: document per-invocation semantics, or derive one deadline in `runSingleTest` and pass the remaining budget.
  Done: `sharedExecutorDeadline` derives one deadline from `opts.Timeout` in `runSingleTest`, shared by both the `ListJobs` and `Run` executor calls via a composed context.

### Mock infrastructure

- [x] **[mock] Implement file locking on Windows instead of returning unsupported** (`internal/mockwrapper/flock_windows.go:12`)
  `lockFile` always errors on Windows and `appendBinaryCall` silently proceeds unlocked; concurrent mock invocations rely on undocumented NTFS append atomicity.
  Fix: implement with `golang.org/x/sys/windows.LockFileEx`, or document the best-effort fallback at the call site per conventions.
  Done: documented the best-effort fallback (no Windows CI to validate a real LockFileEx implementation against).

- [x] **[mock] Handle git command failures in serveGitPack / surface stderr context** (`internal/mockserver/githttp.go:71,111`)
  `_ = cmd.Run()` ignores upload/receive-pack failures with no comment (conventions violation); clients get a truncated 200 body and nothing is recorded server-side. `serveGitInfoRefs` discards the error carrying stderr.
  Fix: capture stderr, log/record pack failures (headers already sent), include stderr in the info/refs 500.

- [x] **[mock] Make user/group handlers respect the requested ID and sub-paths** (`internal/mockserver/server.go:206,250`)
  Any `/api/v4/users/...` or `/api/v4/groups/...` path returns the single configured object — `GET /users/999` returns a user with a different id (real GitLab: 404) and `GET /groups/1/projects` returns an object where GitLab returns an array.
  Fix: parse the ID segment, 404 on mismatch, 404 (or implement) sub-paths.

- [x] **[mock] Surface or drop the write-only serveErr field** (`internal/mockserver/server.go:41,86`)
  A fatal serve error is stored under lock but never read — jobs get connection-refused with no diagnostic.
  Fix: return it from `Stop()` (or an accessor the runner checks), or delete the field.

- [x] **[mock] Deep-copy stored objects — shallow copyObject shares nested maps** (`internal/mockserver/store.go:111`)
  Nested maps/slices (`assets`, `commit`, `author`) are shared between the store and every copy handed out; the advertised copy-isolation guarantee is false and any future nested mutation creates cross-request races.
  Fix: recurse into `map[string]any` and `[]any`, or document the shallow contract.

- [x] **[mock] Harden argv[0] dispatch for Windows casing and .exe suffix** (`internal/mockwrapper/wrapper.go:66,197`)
  `Glut.exe` is compared case-sensitively (misrouted into the mock wrapper, exit 127), and a mock invoked as `release-cli.exe` logs to `release-cli.exe.jsonl`, which the `only` filter of `ReadBinaryLogs` skips — assertions see zero calls.
  Fix: normalize the basename on Windows (lowercase, strip `.exe`) before both comparisons.

- [x] **[mock] Improve coverage of known gaps; remove assertion-free test** (`internal/mockwrapper/wrapper_test.go:619`, `internal/mockserver/endpoints_test.go:906`)
  `TestWriteErrorIgnoresWriterFailure` asserts nothing; `git-receive-pack` (push — the mutating path) has no test; no test asserts the id of a created resource (why the id-0 bug survived); a custom `contains` closure reimplements `strings.Contains`.
  Fix: add the push round-trip and created-id tests, fix or drop the empty test, use the stdlib.

### CLI and parsing

- [x] **[cli] lintPath produces wrong JSON path for job-assert semantic errors** (`cmd/glut/lint_report.go:202`)
  For lintJobAsserts messages the heuristic doubles the prefix (`.glut.glut.assert.job.release`) and truncates job names containing a colon. The `path` field in `lint --format=json` is wrong for exactly the error it exists to locate.
  Fix: skip the `.glut.` prefix when the field already starts with it; add this message shape to `TestLintPathCoverage`.

- [x] **[cli] Lint parses every file twice from disk** (`cmd/glut/lint_report.go:55,88`)
  `collectLintFiles` fully parses each file, then `parser.Lint` re-reads and re-parses it; a file modified between reads yields inconsistent results.
  Fix: add a `LintParsed(*TestFile)` entry point reusing the parsed documents.

- [x] **[cli] parser.Lint on a file without .glut emits a misleading schema error** (`internal/parser/lint.go:12`)
  `readLintInput` returns `(nil, nil, nil)` for missing `.glut:`; `Lint` then validates a nil map against the schema, producing "Expected: object, given: null" plus spurious warnings. Currently unreachable from the CLI, but `Lint` is the exported entry point.
  Fix: return an explicit "no .glut document" result and have `Lint` exit early.

- [x] **[cli] Use errors.Is instead of == for errMissingGlut comparison** (`internal/parser/lint.go:59`)
  The package already defines `IsMissingGlut` with `errors.Is`; the raw `==` breaks silently if the sentinel is ever wrapped with context.
  Fix: replace with `errors.Is(err, errMissingGlut)`.
  Done: already fixed as a byproduct of the earlier High-severity lint work (`readLintInput` already uses `errors.Is`).

- [x] **[cli] Parse returns raw os.ReadFile error without phase context** (`internal/parser/parser.go:14`)
  Every other error in the function is wrapped; the file-read error relies solely on the OS text, against the conventions' boundary rule.
  Fix: `fmt.Errorf("failed to read test file %s: %w", filePath, err)`.
  Done: already fixed as a byproduct of the earlier High-severity parser work.

- [x] **[cli] envDuration silently ignores invalid GLUT_TIMEOUT / GLUT_WAIT_TIMEOUT values** (`cmd/glut/root.go:257`)
  `GLUT_TIMEOUT=10minutes` (or a unit-less `30`) silently falls back to the default with no warning and no documenting comment.
  Fix: print a one-line stderr warning on parse failure, or document the intentional fallback at the call site.

- [x] **[cli] Package-level mutable flag state in cmd/glut contradicts stated conventions** (`cmd/glut/root.go:25,214`)
  20 package-level mutable variables hold flag values and `init()` mutates two from the environment; options tests need save/restore boilerplate and cannot run in parallel.
  Fix: bind flags to per-command option structs captured by the RunE closures.
  Done: every command is now built by a constructor (`newRootCmd`, `newRunCmd`,
  `newListCmd`, `newLintCmd`, `newDoctorCmd`, `newVersionCmd`) that binds flags
  to a local `runFlags` struct (or local variables) captured by the Run
  closures. The `init()` functions in root.go and help.go are gone; the only
  remaining package vars are `version`/`commit`, which the linker writes via
  ldflags. Option tests construct flag structs directly — the save/restore
  boilerplate was deleted and the tests run in parallel.

- [x] **[cli] lint/doctor default path "./tests/" errors out when the directory does not exist** (`cmd/glut/options.go:77`)
  `glut lint` with no args fails with a raw stat error in any repo that keeps tests elsewhere; `run`/`list` default to `.` instead.
  Fix: default to `.` like the other commands, or special-case the missing default directory with a clear message.
  Done: kept the documented `./tests/` default (cli.md explicitly says lint/doctor differ from run/list here) and added `checkDefaultTestsDirExists` to fail with an actionable message instead of a raw stat error.

- [x] **[cli] Dead alias buildDoctorReportResult and production-unused buildDoctorReport** (`cmd/glut/lint_report.go:73`)
  The type alias is pointless and `buildDoctorReport` is called only from tests. lint_report.go (563 LOC) also mixes report building, hint heuristics, YAML scraping, and printing.
  Fix: delete the alias, call `buildDoctorReportFiltered` in tests, consider splitting the file.
  Done: deleted the alias and `buildDoctorReport`; tests call `buildDoctorReportFiltered` directly. Deferred: splitting the file (disproportionate scope for this pass, same as the runSingleTest split).

- [x] **[cli] Garbled comment on pipelineJobNames** (`cmd/glut/lint_report.go:241`)
  The comment is not grammatical English and does not describe the code, against the simple-English convention.
  Fix: rewrite to state that job names are the union of assert.job names and scraped top-level keys.
  Done: already fixed as a byproduct of the earlier High-severity `pipelineJobNames` rewrite (the current comment is accurate and grammatical).

- [x] **[cli] Hardcoded "merge_request_event" literals instead of config.PipelineSourceMR** (`internal/parser/lint.go:170`, `cmd/glut/lint_report.go:343`)
  The constant exists in internal/config exactly for this; two literals can desynchronize lint from runtime.
  Fix: use `config.PipelineSourceMR` in both places.

- [x] **[cli] LintError.Line is never populated** (`internal/parser/types.go:49`)
  The JSON `line` field always omits; the parser has `yaml.Node` positions available and conventions ask for line information where possible.
  Fix: thread node line numbers into `LintError`, or remove the dead field.
  Done: `TestFile` now carries the `.glut` `yaml.Node`; `fieldLines` walks it to build a dotted-path→line map, and `lintSchema` looks up `validationErr.Field` in it for both `Lint` and `LintParsed`. Deferred: threading lines into the other (non-schema) semantic lint messages, since their free-text paths don't map onto the yaml.Node walk as directly.

- [x] **[cli] mostly-exit-status doctor hint misclassifies `{}` and `output:`-only asserts** (`cmd/glut/lint_report.go:417`)
  `jobname: {}` (weakest assert) counts as "rich" and `output:`-only asserts count as exit-only, so the hint is suppressed exactly where asserts are weakest.
  Fix: treat all-nil asserts as weak and include `Output` in the rich-assert check.

- [x] **[cli] Schema/Go drift: access_level string casing** (`schema/glut.schema.json:330`, `internal/config/types.go:178`)
  `UnmarshalYAML` lowercases input so `Maintainer` runs fine, but the schema enum is lowercase-only, so lint rejects a file that run accepts.
  Fix: make the Go unmarshal case-sensitive (schema is authoritative) or loosen the schema pattern.

- [x] **[cli] branch+tag conflict reported twice (schema `not` + semantic lint)** (`schema/glut.schema.json:92`, `internal/parser/lint.go:164`)
  Users see a cryptic schema error and a clear semantic error for one mistake.
  Fix: drop the schema `not` block and keep the Go rule.

- [x] **[cli] Interactive TTY detection misses Cygwin/MSYS terminals on Windows** (`cmd/glut/interactive.go:17`)
  `isatty.IsTerminal` returns false under Git Bash/MinTTY, so `glut run -i` refuses on the platform this repo is developed on.
  Fix: also accept `isatty.IsCygwinTerminal(fd)`.

### Workspace

- [x] **[workspace] Exclude GLUT temp dirs in the rsync copy path; fix the stale prefix constant** (`internal/workspace/workspace.go:17,434,525`)
  rsync excludes only `.git` while native copy skips `tmpDirPrefix` dirs — and `tmpDirPrefix` is `".glut-tmp-"` (trailing dash), which does not match the repo-standard `.glut-tmp/`, so even native copy misses it. Snapshot bloat/stale content in staging.
  Fix: add `--exclude=.glut-tmp*` to rsync and match `.glut-tmp` with and without suffixes from one shared constant.

- [x] **[workspace] Remove or wire up dead workspace-location plumbing** (`internal/workspace/workspace.go:30`, `Makefile:50`)
  `Options.TempDir` is set only by tests, and no Go code reads the Makefile's `GLUT_WORK_DIR`/`GLUT_HOST_WORK_DIR` exports — the intended host-visible-workdir behavior does not exist.
  Fix: wire `GLUT_WORK_DIR` through to `Options.TempDir` in the runner, or delete the dead env vars and field.
  Done: added `RunOptions.WorkspaceTempDir`, read from `GLUT_WORK_DIR` in `toRunnerOptions` and threaded to `workspace.Options.TempDir`. Removed `GLUT_HOST_WORK_DIR` from the Makefile — it has no Go consumer and, since GLUT running inside that container always auto-detects the named-volume Docker strategy (never bind-mount), the host-path-translation concern it implied does not actually arise.

- [x] **[workspace] Escape the binary name in the shell wrapper's JSON log line** (`internal/workspace/dockervolume.go:85`, `internal/workspace/mockbinaries.go:106`)
  `cwd` and args go through `json_str` but `"name":"%s"` interpolates raw; `validateMockBinaryName` allows quotes, so a name like `foo"bar` produces malformed JSONL.
  Fix: escape `$name` like the other fields, or tighten validation to `[A-Za-z0-9._-]+`.

- [x] **[workspace] Make getDefaultBranchFromRepo honor Options.HostEnv** (`internal/workspace/workspace.go:352`)
  Every other git call resolves the binary via `resolveExecutable(..., hostEnv)`; this one uses bare `exec.Command("git", ...)` with the process env, so custom-PATH callers get a different git or a silent fallback to "main".
  Fix: thread `opts.HostEnv` through and use `resolveExecutable` + `cmd.Env` like the rest of the file.

- [x] **[workspace] Pass git config overrides through to origin.Commands** (`internal/workspace/workspace.go:241`)
  Commands run with env stripped to `GLUT_ORIGIN_REPO`, `HOME`, `PATH`; `GIT_CONFIG_*` suppression from hostEnv is dropped, so e.g. a global `tag.gpgSign=true` makes `git tag -a` in a command fail or hang.
  Fix: copy `GIT_*` keys from the effective host env into the command env.

- [x] **[workspace] Set CI_COMMIT_BEFORE_SHA in merge-request pipelines** (`internal/workspace/env_vars.go:200`)
  Real GitLab defines `CI_COMMIT_BEFORE_SHA` (all zeros) in MR pipelines too; GLUT leaves it unset there.
  Fix: set the zero SHA in `applyMergeRequestEnv` as well.

- [x] **[workspace] Truncate CI_COMMIT_REF_SLUG/CI_PROJECT_PATH_SLUG to 63 bytes** (`internal/workspace/slugify.go:10`)
  GitLab shortens `*_SLUG` values to 63 bytes; `slugify` implements everything except the truncation, so long branch names diverge from real GitLab.
  Fix: truncate to 63 bytes before trimming trailing dashes; add a >63-char test case.

- [x] **[workspace] Add phase context to file-write errors in setupGitOrigin** (`internal/workspace/workspace.go:214`)
  `os.MkdirAll`/`os.WriteFile` errors for `setup.git.origin.files` are returned bare, against the boundary-error convention.
  Fix: wrap both with the file name and phase.

- [x] **[workspace] Give forced-rsync strategy the same transient-failure retry as auto** (`internal/workspace/workspace.go:453`)
  Auto mode retries a failed rsync once (transient WSL2 I/O errors); `--copy-strategy=rsync` returns the first error before the retry block — exactly the users who pinned rsync lose the mitigation.
  Fix: move the early-return after the retry attempt; fall back to native only in auto mode.

- [x] **[workspace] Non-zero tar exit with empty stderr is silently treated as success when reading mock logs** (`internal/workspace/dockervolume.go:188`)
  The nil branch mainly masks abnormal container terminations (OOM-kill, daemon race) as "no logs", turning `binary.called` assertions into false negatives; `tar -cC <dir> .` on an empty dir actually exits 0.
  Fix: return the error (wrapped as an infra error so retry applies) and detect the genuinely-empty case explicitly.

- [x] **[workspace] Strengthen TestGitOriginFilesAndCommands** (`internal/workspace/workspace_test.go:406`)
  The file check is conditional (a regression passes if the file still reaches origin), git verification errors are discarded, and lines 408-413 contain leftover free-form reasoning comments.
  Fix: assert the workspace file directly, check git command errors, delete the stray comments.

### Assertions and reporting

- [x] **[assert] Resolve symlinks before reading artifact contents** (`internal/asserter/helpers.go:170`)
  `joinWorkspacePath` validates only lexically; a pipeline-created symlink pointing outside the workspace is followed for `contents`/`md5`/`sha256`, contradicting the "path escapes workspace" guarantee the errors imply.
  Fix: apply `filepath.EvalSymlinks` and re-check containment, or document that symlinks are followed.

- [x] **[assert] Allow gjson-only body asserts on non-object JSON bodies** (`internal/asserter/api.go:91`)
  The body is unconditionally unmarshalled into `map[string]any`, so a JSON array body can never match even a pure `gjson:` assertion.
  Fix: skip the top-level map unmarshal when only `gjson` keys are expected.

- [x] **[assert] Make `have-len: 0` pass for nil values** (`internal/asserter/matcher.go:289`)
  `lengthOf(nil)` returns `(0, false)`, so `have-len: 0` fails against a JSON `null` field.
  Fix: return `(0, true)` for nil in `lengthOf`.

- [x] **[assert] Compare checksum digests case-insensitively** (`internal/asserter/artifacts.go:104`)
  An uppercase MD5/SHA256 pasted from another tool never matches the lowercase `%x` output, with no hint why.
  Fix: use `strings.EqualFold`.

- [x] **[assert] Replace weakly-typed junitSeconds and string re-parsing in sumSuiteDuration** (`internal/reporter/junit.go:161`)
  An unchecked type assertion can panic, and durations are summed by re-parsing the `"%.3f"` strings the code itself formatted.
  Fix: keep per-case durations as `time.Duration`, sum directly, format once.

- [x] **[assert] Sanitize the git environment in the asserter's runGit** (`internal/asserter/helpers.go:194`)
  `runGit` inherits the parent env; with `GIT_DIR`/`GIT_WORK_TREE` set, git ignores `cmd.Dir` and asserts against the wrong repository. The test helper filters exactly these variables; production does not.
  Fix: filter `GIT_DIR`/`GIT_WORK_TREE`/`GIT_INDEX_FILE` from `cmd.Env` like the test helper.

- [x] **[assert] Align bare git file missing-file behavior with the artifact path** (`internal/asserter/git.go:161`)
  A missing file with `exists: true` emits both the `.exists` failure and a second generic failure — double reporting the artifact path avoids.
  Fix: mirror `runArtifactAssert`'s per-field handling without the duplicate generic failure.

- [x] **[assert] Add negative and edge-case coverage to matcher tests** (`internal/asserter/matcher_test.go:5`)
  All 17 advanced-matcher cases assert `wantPass: true`; untested: per-operator mismatches, empty patterns, unicode, multiline vs `/re/` anchoring, type mismatches, `consist-of` duplicates, `not` around invalid regex — the two semantics bugs above survive precisely because of this.
  Fix: extend the table with failing rows per operator and the listed edge cases.

- [x] **[assert] Make matcher/report lookup tables immutable per conventions** (`internal/asserter/matcher.go:14`, `internal/asserter/report.go:58`)
  `matcherKeys` and `reportAllowedFields` are package-level mutable maps; conventions discourage package-level mutable state.
  Fix: return them from functions or document them as read-only lookup tables.

### Build, CI, and docs

- [x] **[build] Docs quickstart command is wrong (`glut glut --help`)** (`docs/getting-started/installation.md:6`)
  The image entrypoint is already `glut`, so the documented command executes `glut glut --help` and fails — the first command a new user tries.
  Fix: drop the extra `glut` from the `docker run` example.

- [x] **[build] Runtime image runs as root and installs the full Docker engine** (`Dockerfile:15`)
  `docker.io` (daemon + CLI) is installed where only the CLI is needed; no non-root `USER`; Node floats on a major tag.
  Fix: install only the Docker CLI, consider pinning node minor, document why the image stays root.
  Done: replaced `docker.io` with the static Docker CLI binary from download.docker.com (verified: no `dockerd` present, `docker ps`/`volume ls` work over the mounted socket); pinned `NODE_VERSION` to `22.23`; added a comment documenting why the image stays root (socket GID varies by host, so a fixed non-root UID cannot reliably be granted access).

- [x] **[build] Pin gotestsum, goreleaser, and DinD versions** (`.github/workflows/ci.yml:36`, `.github/workflows/build.yml:44`, `.gitlab-ci.yml:36`)
  `gotestsum@latest` and goreleaser `version: latest` make releases non-reproducible; `docker:27-dind` floats on a major tag; actions are tag-pinned only.
  Fix: pin exact versions; optionally SHA-pin actions per supply-chain policy.
  Done: pinned to gotestsum v1.13.0, goreleaser v2.16.0, `docker:27.5.1-dind` (all verified against the currently-resolved versions). Deferred SHA-pinning actions (marked optional in the finding; a repo-wide policy change out of scope for this pass).

- [x] **[build] Conventional Commits not enforced by CI; no CHANGELOG or SECURITY.md** (`.github/workflows/`)
  CLAUDE.md/AGENTS.md mandate Conventional Commits and semantic-release derives versions from them, but nothing validates them — a typo'd type silently produces no release.
  Fix: add a commitlint or PR-title check; optionally add `@semantic-release/changelog` and a SECURITY.md.
  Done: added `.github/workflows/commitlint.yml` (runs on every PR via `wagoid/commitlint-github-action`) and `commitlint.config.mjs`, restricted to the exact type list in docs/conventions.md. CHANGELOG.md is intentionally omitted by maintainer decision: GitHub Releases (generated by semantic-release from Conventional Commits) are the changelog. SECURITY.md remains an optional future addition.

- [x] **[build] Makefile has no .PHONY declarations and unquoted $(PWD)** (`Makefile:6,51`)
  A file named `test` or `build` in the repo root silently no-ops the target; `-e GLUT_HOST_WORK_DIR=$(PWD)/.glut-tmp` breaks on paths with spaces.
  Fix: add a `.PHONY` line for all targets and quote the path.
  Done: added `.PHONY` for all 9 targets. The unquoted `$(PWD)` instance was already removed when `GLUT_HOST_WORK_DIR` was deleted (see the dead-workspace-plumbing fix above); the remaining `$(PWD)` uses were already quoted.

- [x] **[build] failing/ suite has no negative fixtures for git, api, or binary asserts** (`tests/failing/`)
  The negative regression suite covers job/artifact/report failures only; false-pass regressions in the git, api, and binary asserters would go undetected at the integration level.
  Fix: add one minimal failing fixture per assert family.
  Done: added `api-not-called.yml`, `binary-not-called.yml`, and `git-commit-count-mismatch.yml`; verified locally that each fails for the intended reason and the full `tests/failing/` suite still exits non-zero overall.

- [x] **[build] goreleaser `before: go mod tidy` can silently release modified sources** (`.goreleaser.yaml:5`)
  The dirty-state check happens before hooks; if tidy changes go.mod/go.sum, published binaries differ from the tagged commit.
  Fix: drop the hook or replace it with `go mod verify` / `git diff --exit-code` after tidy.
  Done: replaced with `go mod tidy && git diff --exit-code -- go.mod go.sum`, failing the release instead of silently proceeding. This immediately caught real drift (go.mod had `spf13/pflag` misclassified as indirect); fixed by committing the tidied go.mod.

## Review notes — verified clean areas

- `scripts/check-coverage.sh` logic is correct (the problem is only the masked exit code in CI).
- Go/GCL version pinning is consistent across go.mod, Dockerfile, devcontainer, and workflows.
- Secrets handling in workflows and `.gitlab-ci.yml` is sound (env-scoped token, fork guard on gitlab-integration, cleanup in `always()`).
- `schema/glut.schema.json` matches `docs/reference/assert-syntax.md` and the Go types (except the `access_level` casing item above).
- JUnit XML escaping, ANSI handling (per-writer lipgloss renderer), and RE2 regex use in the asserter are safe.
- Workspace isolation: per-test unique temp dirs/volume names, argv-vector command construction, token kept out of args/remotes/errors, cleanup on setup failure — all verified with tests.
- Mockserver mutex partitioning, Start/Stop lifecycle, and recorder copy semantics are correct.

## Fix verification (2026-07-04)

A second review pass verified all 99 applied fixes on branch `fix/code-review-findings`
(merge-base `525aaa6`) against the tasks above. Result: **96 confirmed fixed as
described, 2 fixed with a follow-up issue, 1 partial** (mock stdin tee). The 3
deferred refactoring tasks are correctly annotated and unchecked. Verification
was static (code + diff review); the full test suite runs in CI.

The follow-up issues found during verification are listed below as new tasks.
All of them have since been fixed and re-tested: `go build`, `go vet`, the
affected package tests, and `golangci-lint v2.11.4` (`config verify` + full run,
0 issues) all pass. The gosec exclude IDs were confirmed load-bearing, and the
mock stdin-capture semantics turned out narrower than first reported (see the
individual items below for details).

### Medium

- [x] **[build] commitlint rules reject this branch's own commits** (`commitlint.config.mjs:13,15`)
  `subject-case: [2, 'always', 'lower-case']` fails any subject containing an
  uppercase letter (identifiers like `JUnit`, `GLUT_WORK_DIR`, `Docker` appear
  throughout this branch), and `header-max-length: 72` is exceeded by 30 of the
  branch's ~63 commits (up to 125 chars). The new commitlint gate lints every PR
  commit, so it turns CI red on the very PR that introduces it.
  Fix: relax `subject-case` to config-conventional's default (`never [sentence-case, start-case, pascal-case, upper-case]`)
  and either raise `header-max-length` (e.g. 100) or accept it only for future commits (squash-merge the PR).

- [x] **[cli] `pages` is treated as a reserved keyword, causing a false lint error** (`internal/parser/lint.go:224`)
  `gitlabReservedTopLevelKeys` lists `pages`, but in GitLab CI `pages` is a real,
  common job name. A valid test asserting on a `pages:` job now gets a blocking
  error from the new missing-job lint (`references a job that is not defined in the pipeline`)
  and `glut lint` exits non-zero.
  Fix: remove `pages` from the reserved map (in both copies — see the duplication task below); add a `pages` job test case.

### Low

- [x] **[mock] Set cmd.WaitDelay in the mock wrapper to close the residual stdin hang** (`internal/mockwrapper/wrapper.go:97`)
  The stdin tee fix leaves one narrow hang: `Stdin` is a non-`*os.File` reader, so
  os/exec spawns a copy goroutine and `Wait` blocks until it finishes. If the real
  binary exits without consuming stdin while the producer keeps the pipe open and
  silent, the wrapper hangs after the child exited.
  Fix: set `cmd.WaitDelay` (e.g. 1s) in `RunWithOptions`.

- [x] **[mock] Stdin capture semantics changed silently; call record lost on kill** (`internal/mockwrapper/wrapper.go:96-113`)
  Captured stdin now reflects only what the real binary actually read — a stub that
  ignores stdin logs `""` where the old code logged the full piped input, which can
  silently change `assert.binary` stdin assertions. The JSONL record is also written
  after the child runs, so a wrapper killed mid-run (job timeout) loses the record.
  Fix: verify generated stubs in `internal/workspace/mockbinaries.go` against a stdin
  assertion test; document the semantics in assert-syntax.md.
  Done: added `TestRunWithOptionsStdinCaptureReflectsWhatMockReads`, which revealed
  the concern was narrower than stated — os/exec buffers stdin into the OS pipe, so a
  typical (small) input is captured in full even when the mock never reads it; only a
  payload larger than the pipe buffer (~64 KiB) that the mock never drains can be
  captured in part. Documented this in `docs/reference/assert-syntax.md` under Binary
  Asserts (recommend `cat >/dev/null` in the mock when asserting on large stdin). The
  "record lost on kill" note is already covered by the barrier-file mechanism in
  `appendBinaryCall` (`CheckMockLogBarriers` detects interrupted writes).

- [x] **[assert] Scan-error matchStates omit IsError, so `not` inverts them into a pass** (`internal/asserter/matcher.go:62`, `internal/asserter/patterns.go:25`)
  The new scanner-error path returns a failed state without `IsError: true`; `not:`
  around a scalar/list pattern turns a >10 MiB-line scan failure into a pass — the
  inversion class the `IsError` mechanism was built to stop.
  Fix: set `IsError: true` on scan-failure states.

- [x] **[assert] `or` drops IsError from non-final branches; slash-syntax regex errors stay silent** (`internal/asserter/matcher.go:302`, `internal/asserter/patterns.go:44`)
  `or` keeps only the last branch's state, so `not: {or: [{match-regexp: "("}, "no-match"]}`
  still passes; and an invalid regex in `/re/` / `!/re/` syntax compiles to a plain
  `false`, so `not: "/(/"` always passes — only `match-regexp` got the IsError treatment.
  Fix: propagate IsError through all `or` branches and return an IsError state from
  `matchSinglePattern` on regex compile failure.

- [x] **[assert] assert-syntax.md overstates scalar/list pattern parity** (`docs/reference/assert-syntax.md` scalar section)
  The new text says scalars use "these same per-line rules", but only `/re/`, `!/re/`,
  and `\!` forms are routed per-line; bare `text` and `!text` scalars remain exact
  deep-equality, diverging from list semantics.
  Fix: scope the sentence to the three special-string forms.

- [x] **[assert] yamlQuoteString leaves control characters unescaped** (`internal/reporter/tap.go:86-107`)
  Only `\n \r \t` are escaped inside the double-quoted YAML scalar; other control
  chars (e.g. ANSI ESC, realistic in raw job output) pass through and strict YAML
  parsers may reject the diagnostic block.
  Fix: escape remaining C0 control characters (e.g. `\xNN` form).

- [x] **[cli] lintPath truncates the new missing-job message for colon job names** (`cmd/glut/lint_report.go:211-218`)
  For `.glut.assert.job.deploy:prod references a job that is not defined...`, lintPath
  splits at the first colon and returns a truncated path; normal names fall to the
  generic `.glut.assert` case. The new message shape reopened the exact blind spot the
  lintPath fix closed, and TestLintPathCoverage has no row for it.
  Fix: add a special case for the missing-job message (like the `: "when"` one) and a coverage row.

- [x] **[cli] Reserved-keyword map duplicated in parser and cmd/glut** (`internal/parser/lint.go:218`, `cmd/glut/lint_report.go:284`)
  `gitlabReservedTopLevelKeys` and `gitlabTopLevelKeywords` are identical maps in two
  packages that can silently desynchronize.
  Fix: export one copy (internal/config or parser) and use it in both places.

- [x] **[cli] `(?i)` inline regex flag is not portable JSON Schema** (`schema/glut.schema.json:330`)
  JSON Schema `pattern` is ECMA-262, which rejects `(?i)`. Works with the Go validator
  (RE2), but external consumers (editors, yaml-language-server) fail to compile it.
  Fix: enumerate the case variants or document the Go-only assumption.

- [x] **[runner] Map ESRCH to os.ErrProcessDone in cmd.Cancel** (`internal/executor/procattrs_unix.go:17`)
  When the whole group already exited as the deadline fires, `syscall.Kill` returns raw
  `ESRCH`, which os/exec does not suppress — Wait can surface a spurious "canceling Cmd" error.
  Fix: return `os.ErrProcessDone` when the kill fails with `ESRCH`.

- [x] **[runner] Document the Ctrl-C semantics change from Setpgid** (`internal/executor/procattrs_unix.go:15`, `docs/gotchas.md`)
  gcl no longer receives the terminal's SIGINT (it is in its own process group); glut
  now SIGKILLs the tree via ctx cancel. Daemon-managed containers are not in the
  process tree and may be left running where a graceful gcl SIGINT would have cleaned up.
  Fix: document in gotchas.md; consider a graceful SIGINT-then-SIGKILL escalation in cmd.Cancel.
  Done: documented at the source in `setProcessGroup`'s doc comment. A graceful
  SIGINT-then-SIGKILL escalation is left as a possible future improvement.

- [x] **[runner] Volume-prune comment overclaims the concurrency guarantee** (`internal/docker/wait.go:211-216`)
  `dangling=true` narrows but does not close the race: a concurrent glut's volume is
  dangling between its populate container exiting and its job container starting.
  Fix: correct the comment; optionally embed the PID in volume names and skip live-PID volumes.

- [x] **[build] Verify the gosec exclude IDs exist** (`.golangci.yml:47-51`)
  Excludes G702/G703/G705/G122 could not be confirmed against the pinned gosec version;
  if they are typos, the corresponding findings resurface as CI lint failures (loud, not silent).
  Fix: run `golangci-lint` once locally/CI and confirm each ID suppresses a real finding.
  Done: ran `golangci-lint v2.11.4` (`config verify` passes, full run reports 0
  issues). Removing the four IDs surfaces exactly the findings they suppress —
  G702 (`internal/mockserver/githttp.go:74,113`), G705 (`githttp.go:93`), G703
  (`internal/mockwrapper/wrapper_test.go:892`), G122 (`internal/workspace/dockervolume.go:518`,
  `workspace.go:548`) — so all four are real, load-bearing rules, not typos.

- [x] **[mock] Trivia: stale comment, over-broad git bypass, unwrapped serveErr** (`internal/mockwrapper/wrapper.go:214`, `internal/mockserver/server.go:207,113`)
  (a) Comment still says stdin is captured "with no size cap" (now 10 MiB). (b) `isGitHTTPPath`
  matches any path containing `.git/`, so a hypothetical API path embedding it would skip
  recording. (c) `Stop()`'s early branch returns `serveErr` bare while the main path wraps it.
  Fix: update the comment, anchor the git-path check to the repo route, wrap consistently.
