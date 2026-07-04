package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/parser"
)

func TestRunPassingTest(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/pass.yml", testFileYAML("pass test", "pass-job", "ok"))

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts())
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitOK)
	}
	if result.Passed != 1 || result.Failed != 0 {
		t.Fatalf("Run() summary = %#v", result)
	}
	if len(result.Tests) != 1 || !result.Tests[0].Passed {
		t.Fatalf("Run() tests = %#v", result.Tests)
	}
}

func TestRunFailingAssertResult(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/fail.yml", testFileYAML("fail test", "fail-job", "expected"))

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts())
	if exitCode != ExitTestFailed {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitTestFailed)
	}
	if result.Failed != 1 {
		t.Fatalf("Run() failed = %d, want 1", result.Failed)
	}
	if len(result.Tests[0].Failures) == 0 {
		t.Fatalf("expected assert failures, got %#v", result.Tests[0])
	}
}

func TestRunReturnsRunnerErrorForInvalidInput(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeRawFile(t, "tests/invalid.yml", strings.TrimSpace(`
test-job:
  script:
    - echo ok
---
.glut:
  name: bad-setup
  setup:
    pipeline_source: merge_request_event
`)+"\n")

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts())
	if exitCode != ExitTestFailed {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitTestFailed)
	}
	if len(result.Tests) != 1 || result.Tests[0].Error == nil || !strings.Contains(result.Tests[0].Error.Error(), "setup.merge_request is missing") {
		t.Fatalf("Run() test error = %v, want merge_request missing context", result.Tests)
	}
}

func TestRunReturnsErrorMessageForInvalidDebugPause(t *testing.T) {
	t.Parallel()
	result, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{DebugPause: "bad"})
	if exitCode != ExitRunnerError {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitRunnerError)
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "invalid debug pause point") {
		t.Fatalf("Run() error = %v, want debug pause context", result.Error)
	}
}

func TestRunFailFastStopsAfterFirstFailure(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/01-fail.yml", testFileYAML("first fail", "job-one", "want"))
	env.writeTestFile(t, "tests/02-pass.yml", testFileYAML("second pass", "job-two", "ok"))

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts(RunOptions{FailFast: true}))
	if exitCode != ExitTestFailed {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitTestFailed)
	}
	if len(result.Tests) != 1 {
		t.Fatalf("Run() ran %d tests, want 1", len(result.Tests))
	}
	if result.Tests[0].TestName != "first fail" {
		t.Fatalf("first result = %#v", result.Tests[0])
	}
}

func TestRunMaxFailStopsAfterLimit(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/01-fail.yml", testFileYAML("first fail", "job-one", "want"))
	env.writeTestFile(t, "tests/02-fail.yml", testFileYAML("second fail", "job-three", "want"))
	env.writeTestFile(t, "tests/03-pass.yml", testFileYAML("third pass", "job-two", "ok"))

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts(RunOptions{MaxFail: 2}))
	if exitCode != ExitTestFailed {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitTestFailed)
	}
	if len(result.Tests) != 2 {
		t.Fatalf("Run() ran %d tests, want 2", len(result.Tests))
	}
}

func TestListSupportsPatternAndRecursiveDiscovery(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/a/alpha.yml", testFileYAML("alpha", "job-alpha", "ok"))
	env.writeTestFile(t, "tests/b/beta.yml", testFileYAML("beta", "job-beta", "ok"))

	tests, err := List(context.Background(), []string{"tests"}, ListOptions{RunPattern: "beta", WorkDir: env.workDir})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(tests) != 1 {
		t.Fatalf("List() = %#v, want one test", tests)
	}
	if !strings.HasSuffix(tests[0].FilePath, filepath.Join("tests", "b", "beta.yml")) {
		t.Fatalf("List() path = %q", tests[0].FilePath)
	}
}

// TestFinalizeCleanupError guards against a test being reported Passed=true
// with a non-nil Error: result.Passed is computed in runSingleTest before
// deferred cleanup (mock server stop, workspace policy) can turn up an
// error, so finalizeCleanupError must recheck it rather than trust the
// caller's value.
// TestGitOutputIgnoresStderrChatter guards against gitOutput using
// CombinedOutput, which merges stderr into the parsed value. Warnings from a
// user's gitconfig (e.g. hint: ... lines) would otherwise corrupt values
// like CI_COMMIT_SHA or the commit message.
func TestGitOutputIgnoresStderrChatter(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, dir, "git", "#!/bin/sh\necho 'hint: some advice from user gitconfig' >&2\necho abc123\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got, err := gitOutput(dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("gitOutput() error = %v", err)
	}
	if got != "abc123" {
		t.Fatalf("gitOutput() = %q, want %q — stderr chatter must not corrupt the parsed value", got, "abc123")
	}
}

func TestFinalizeCleanupError(t *testing.T) {
	primary := errors.New("assert failed")
	cleanup := errors.New("stop mock server: boom")

	tests := []struct {
		name          string
		passed        bool
		primaryErr    error
		cleanupErrors []error
		wantPassed    bool
		wantErr       error
	}{
		{name: "passed with no errors stays passed", passed: true, wantPassed: true, wantErr: nil},
		{name: "cleanup error demotes an already-passed result", passed: true, cleanupErrors: []error{cleanup}, wantPassed: false, wantErr: cleanup},
		{name: "primary error stays failing", passed: false, primaryErr: primary, wantPassed: false, wantErr: primary},
		{name: "primary error takes precedence over cleanup error", passed: false, primaryErr: primary, cleanupErrors: []error{cleanup}, wantPassed: false, wantErr: primary},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPassed, gotErr := finalizeCleanupError(tt.passed, tt.primaryErr, tt.cleanupErrors)
			if gotPassed != tt.wantPassed {
				t.Errorf("passed = %v, want %v", gotPassed, tt.wantPassed)
			}
			if !errors.Is(gotErr, tt.wantErr) {
				t.Errorf("err = %v, want %v", gotErr, tt.wantErr)
			}
			if gotErr != nil && gotPassed {
				t.Errorf("invariant violated: Passed=true with non-nil Error %v", gotErr)
			}
		})
	}
}

func TestRunPreservesOnlyLastFailedWorkspaces(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/01.yml", testFileYAML("fail one", "job-one", "want"))
	env.writeTestFile(t, "tests/02.yml", testFileYAML("fail two", "job-three", "want"))
	env.writeTestFile(t, "tests/03.yml", testFileYAML("fail three", "job-four", "want"))

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts(RunOptions{KeepLastFailed: 2}))
	if exitCode != ExitTestFailed {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitTestFailed)
	}
	if len(result.Tests) != 3 {
		t.Fatalf("Run() tests = %d, want 3", len(result.Tests))
	}

	first := result.Tests[0].WorkspacePath
	last := result.Tests[2].WorkspacePath
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("first preserved workspace still exists: %s", first)
	}
	if _, err := os.Stat(last); err != nil {
		t.Fatalf("last preserved workspace missing: %v", err)
	}
	for _, testResult := range result.Tests[1:] {
		if testResult.WorkspacePath != "" {
			_ = os.RemoveAll(testResult.WorkspacePath)
		}
	}
}

func TestRunProgressSinkReceivesEventsInOrder(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/pass.yml", testFileYAML("pass test", "pass-job", "ok"))

	sink := &recordingSink{}
	_, exitCode := Run(context.Background(), []string{"tests"}, env.opts(RunOptions{
		Progress: []ProgressSink{sink},
	}))
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitOK)
	}

	want := []string{"start:1", "test:pass test", "summary:1/0"}
	if strings.Join(sink.events, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %#v, want %#v", sink.events, want)
	}
}

func TestRunDebugPauseBeforePipelineContinues(t *testing.T) {
	env := newRunnerTestEnv(t) // no t.Parallel — withStdinLine mutates os.Stdin (global state)
	env.writeTestFile(t, "tests/pass.yml", testFileYAML("pass test", "pass-job", "ok"))

	withStdinLine(t, func() {
		result, exitCode := Run(context.Background(), []string{"tests"}, env.opts(RunOptions{DebugPause: "before-pipeline"}))
		if exitCode != ExitOK {
			t.Fatalf("Run() exit = %d, want %d; result = %#v", exitCode, ExitOK, result)
		}
		if len(result.Tests) != 1 || !result.Tests[0].Passed {
			t.Fatalf("Run() tests = %#v", result.Tests)
		}
	})
}

func TestRunDebugKeepsFailureDiagnostics(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/fail.yml", testFileYAML("fail test", "fail-job", "expected"))

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts(RunOptions{Debug: true}))
	if exitCode != ExitTestFailed {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitTestFailed)
	}
	if len(result.Tests) != 1 {
		t.Fatalf("Run() tests = %d, want 1", len(result.Tests))
	}
	debug := result.Tests[0].Debug
	if debug == nil {
		t.Fatal("expected debug data for failed test")
	}
	if debug.RawStdout == "" {
		t.Fatalf("expected raw stdout in debug data: %#v", debug)
	}
	if debug.WorkspaceGitLog == "" || debug.OriginGitLog == "" {
		t.Fatalf("expected git logs in debug data: %#v", debug)
	}
	if _, ok := debug.PhaseTimings["pipeline"]; !ok {
		t.Fatalf("expected pipeline timing: %#v", debug.PhaseTimings)
	}
	_ = os.RemoveAll(result.Tests[0].WorkspacePath)
}

// TestRunDebugSurvivesWorkspaceCreationFailure guards against a panic in the
// debug defer: workspace.New returns a nil *Workspace on error, and the
// debug-data block used to dereference work.WorkspaceDir unconditionally.
func TestRunDebugSurvivesWorkspaceCreationFailure(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeRawFile(t, "tests/bad-origin.yml", strings.TrimSpace(`
stages: [test]

test-job:
  stage: test
  script:
    - echo ok
---
.glut:
  name: workspace creation fails
  setup:
    git:
      origin:
        files:
          "../escape.txt": "nope"
  assert:
    job:
      test-job:
        present: true
`)+"\n")

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts(RunOptions{Debug: true}))
	if exitCode != ExitTestFailed {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitTestFailed)
	}
	if len(result.Tests) != 1 {
		t.Fatalf("Run() tests = %#v", result.Tests)
	}
	tr := result.Tests[0]
	if tr.Error == nil || !strings.Contains(tr.Error.Error(), "create workspace") {
		t.Fatalf("Run() test error = %v, want workspace creation failure", tr.Error)
	}
	if tr.Debug == nil {
		t.Fatal("expected debug data even when workspace creation fails")
	}
	if tr.Debug.WorkspaceGitLog != "" || tr.Debug.OriginGitLog != "" {
		t.Fatalf("expected empty git logs when no workspace was created: %#v", tr.Debug)
	}
}

// TestRunAbortsRemainingTestsOnContextCancellation guards against the loop
// silently continuing to spin up workspaces/mock servers for every
// remaining test after the run context has been cancelled (e.g. SIGINT).
func TestRunAbortsRemainingTestsOnContextCancellation(t *testing.T) {
	counterDir := t.TempDir()
	counterFile := filepath.Join(counterDir, "counter")

	env := newRunnerTestEnvWithScript(t, counterScript(counterFile))
	for i := 1; i <= 3; i++ {
		env.writeRawFile(t, fmt.Sprintf("tests/t%d.yml", i), strings.TrimSpace(fmt.Sprintf(`
stages: [test]

job:
  stage: test
  script:
    - echo ok
---
.glut:
  name: test %d
  assert:
    job:
      job:
        exit-status: 0
`, i))+"\n")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			data, _ := os.ReadFile(counterFile)
			if strings.TrimSpace(string(data)) == "1" {
				cancel()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	result, _ := Run(ctx, []string{"tests"}, env.opts())
	if len(result.Tests) != 1 {
		t.Fatalf("expected the loop to stop after the in-flight test, got %d results: %#v", len(result.Tests), result.Tests)
	}
	if result.Tests[0].Passed {
		t.Fatalf("expected the interrupted test to be reported as failed, got %#v", result.Tests[0])
	}
}

// counterScript increments a file-backed counter on each invocation and
// sleeps on the first call only, giving a test a deterministic window to
// cancel the run context mid-execution without relying on wall-clock races.
func counterScript(counterFile string) string {
	return fmt.Sprintf(`#!/bin/sh
n=$(($(cat %q 2>/dev/null || echo 0) + 1))
echo "$n" > %q
if [ "$n" = "1" ]; then
  sleep 5
fi
job_name="$(grep '^[A-Za-z0-9_-]\+:' .gitlab-ci.yml | cut -d: -f1 | grep -v '^stages$' | head -n1)"
printf 'GLUT_JOB|name=%%s|exit=0|stdout=ok|stderr=\n' "$job_name"
`, counterFile, counterFile)
}

// TestRunRetriesDaemonFailureEvenWithPresentAssertion guards against the
// infra-retry heuristic never firing for tests with present:/when:
// assertions. Those pre-populate JobOutputs from executor.ListJobs before
// the pipeline runs, so len(JobOutputs) == 0 is never true even when the
// actual run fails at the daemon level before any job executes — exactly
// the class of DinD flake the retry exists to smooth over.
func TestRunRetriesDaemonFailureEvenWithPresentAssertion(t *testing.T) {
	orig := dockerTestRetryPause
	dockerTestRetryPause = time.Millisecond
	defer func() { dockerTestRetryPause = orig }()

	env := newRunnerTestEnvWithScript(t, `#!/bin/sh
if [ "$1" = "--list-json" ]; then
  echo '[{"name":"job","when":"on_success"}]'
  exit 0
fi
echo "daemon unreachable" >&2
exit 1
`)
	env.writeRawFile(t, "tests/present.yml", strings.TrimSpace(`
stages: [test]

job:
  stage: test
  script:
    - echo ok
---
.glut:
  name: present assertion daemon failure
  assert:
    job:
      job:
        present: true
`)+"\n")

	sink := &recordingSink{}
	_, exitCode := Run(context.Background(), []string{"tests"}, env.opts(RunOptions{
		Progress: []ProgressSink{sink},
	}))
	if exitCode != ExitTestFailed {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitTestFailed)
	}

	retried := false
	for _, event := range sink.events {
		if strings.HasPrefix(event, "retry:") {
			retried = true
		}
	}
	if !retried {
		t.Fatalf("expected the daemon-level failure to trigger a retry, got events %#v", sink.events)
	}
}

// TestRunDockerWaitFailurePreservesCompletedResults guards against a
// mid-suite docker.Wait failure discarding results of tests that already
// ran: Run used to return a fresh RunResult{Error: ...} in that case,
// dropping every completed test and skipping sink.Summary entirely.
func TestRunDockerWaitFailurePreservesCompletedResults(t *testing.T) {
	env := newRunnerTestEnv(t)
	// Nothing listens here, so docker.dial fails fast instead of reaching a
	// real daemon. Set via opts.HostEnv (not the process env) to guard
	// against docker.Wait/Endpoint reading DOCKER_HOST from the process env
	// directly instead of the resolved host environment.
	env.hostEnv = append(env.hostEnv, "DOCKER_HOST=tcp://127.0.0.1:1")
	env.writeRawFile(t, "tests/a-no-docker.yml", strings.TrimSpace(`
stages: [test]

job:
  stage: test
  script:
    - echo ok
---
.glut:
  name: a no docker test
  setup:
    docker: false
  assert:
    job:
      job:
        exit-status: 0
`)+"\n")
	env.writeRawFile(t, "tests/b-needs-docker.yml", strings.TrimSpace(`
stages: [test]

job:
  stage: test
  script:
    - echo ok
---
.glut:
  name: b needs docker test
  assert:
    job:
      job:
        exit-status: 0
`)+"\n")

	sink := &recordingSink{}
	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts(RunOptions{
		WaitTimeout: 10 * time.Millisecond,
		Progress:    []ProgressSink{sink},
	}))
	if exitCode != ExitRunnerError {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitRunnerError)
	}
	if result.Error == nil {
		t.Fatal("expected result.Error to be set")
	}
	if len(result.Tests) != 1 || result.Tests[0].TestName != "a no docker test" {
		t.Fatalf("expected the already-completed test result to be preserved, got %#v", result.Tests)
	}

	summaryCalled := false
	for _, event := range sink.events {
		if strings.HasPrefix(event, "summary:") {
			summaryCalled = true
		}
	}
	if !summaryCalled {
		t.Fatal("expected sink.Summary to still be called")
	}
}

// TestRunExercisesRealGitLabOutputFormatEndToEnd guards against a gap where
// every runner-level test used the synthetic GLUT_JOB| marker protocol, so
// the real parseGitLabOutput path (regex-parsed "job > line" / "job finished
// in ... PASS" gcl output) was never exercised end-to-end through the full
// runner → executor → asserter stack.
func TestRunExercisesRealGitLabOutputFormatEndToEnd(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnvWithScript(t, "#!/bin/sh\n"+
		"echo 'build-job > Compiling project...'\n"+
		"echo 'build-job > Build complete'\n"+
		"echo 'build-job finished in 120 ms  PASS'\n")

	env.writeRawFile(t, "tests/realistic.yml", strings.TrimSpace(`
stages: [test]

build-job:
  stage: test
  script:
    - echo Compiling project...
    - echo Build complete
---
.glut:
  name: realistic gcl output format
  assert:
    job:
      build-job:
        exit-status: 0
        stdout:
          - "Compiling project..."
          - "Build complete"
`)+"\n")

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts())
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d; tests = %#v", exitCode, ExitOK, result.Tests)
	}
	if len(result.Tests) != 1 || !result.Tests[0].Passed {
		t.Fatalf("Run() tests = %#v", result.Tests)
	}
}

func TestRunRecordsMockBinaryCalls(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	logger := filepath.Join(env.repoDir, "mock-logger")
	writeExecutable(t, env.repoDir, "mock-logger", `#!/bin/sh
name="$(basename "$0")"
mkdir -p "$GLUT_MOCK_LOG_DIR"
printf '{"name":"%s","args":["--flag"],"cwd":"%s","stdin":""}\n' "$name" "$(pwd)" >> "$GLUT_MOCK_LOG_DIR/$name.jsonl"
`)
	env.writeRawFile(t, "tests/mock.yml", strings.TrimSpace(`
stages: [test]

mock-job:
  stage: test
  script:
    - mock-tool --flag
---
.glut:
  name: mock binary test
  setup:
    docker: false
    mocks:
      binaries:
        mock-tool:
          executable: "echo mocked"
  assert:
    job:
      mock-job:
        present: true
        exit-status: 0
    binary:
      mock-tool:
        called: true
        times: 1
        calls:
          - args:
              contain-element: "--flag"
`)+"\n")

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts(RunOptions{GlutBinPath: logger}))
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d; result = %#v", exitCode, ExitOK, result)
	}
	if len(result.Tests) != 1 || !result.Tests[0].Passed {
		t.Fatalf("Run() tests = %#v", result.Tests)
	}
}

func TestRunChecksArtifactsAndAPICalls(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeRawFile(t, "tests/api-artifact.yml", strings.TrimSpace(`
stages: [test]

call-api:
  stage: test
  script:
    - curl --header "PRIVATE-TOKEN: $CI_JOB_TOKEN" "$CI_API_V4_URL/projects/1"
    - echo artifact > artifact.txt
---
.glut:
  name: api and artifact test
  setup:
    docker: false
  assert:
    job:
      call-api:
        present: true
        exit-status: 0
    artifacts:
      artifact.txt:
        exists: true
        contents:
          contain-substring: artifact
    api:
      "GET /api/v4/projects/1":
        called: true
        times: 1
`)+"\n")

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts())
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d; result = %#v", exitCode, ExitOK, result)
	}
	if len(result.Tests) != 1 || !result.Tests[0].Passed {
		t.Fatalf("Run() tests = %#v", result.Tests)
	}
}

func TestRunReportsMockBinarySetupError(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeRawFile(t, "tests/mock-setup-error.yml", strings.TrimSpace(`
stages: [test]

mock-job:
  stage: test
  script:
    - mock-tool
---
.glut:
  name: mock setup error
  setup:
    docker: false
    mocks:
      binaries:
        mock-tool:
          executable: "echo mocked"
  assert:
    job:
      mock-job:
        present: true
`)+"\n")

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts(RunOptions{GlutBinPath: filepath.Join(env.repoDir, "missing-glut")}))
	if exitCode != ExitTestFailed {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitTestFailed)
	}
	if len(result.Tests) != 1 || result.Tests[0].Error == nil {
		t.Fatalf("Run() result = %#v", result)
	}
	if !strings.Contains(result.Tests[0].Error.Error(), "setup mock binaries") {
		t.Fatalf("Run() error = %v", result.Tests[0].Error)
	}
}

func TestRunReportsListJobsError(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnvWithScript(t, fakeGitLabCILocalListErrorScript())
	env.writeRawFile(t, "tests/list-error.yml", strings.TrimSpace(`
stages: [test]

list-job:
  stage: test
  script:
    - echo ok
---
.glut:
  name: list error
  assert:
    job:
      list-job:
        present: false
`)+"\n")

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts())
	if exitCode != ExitTestFailed {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitTestFailed)
	}
	if len(result.Tests) != 1 || result.Tests[0].Error == nil {
		t.Fatalf("Run() result = %#v", result)
	}
	if !strings.Contains(result.Tests[0].Error.Error(), "list jobs") {
		t.Fatalf("Run() error = %v", result.Tests[0].Error)
	}
}

func TestRunReportsPipelineErrorWithoutJobOutput(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnvWithScript(t, fakeGitLabCILocalPipelineErrorScript())
	env.writeRawFile(t, "tests/pipeline-error.yml", strings.TrimSpace(`
stages: [test]

pipeline-job:
  stage: test
  script:
    - exit 1
---
.glut:
  name: pipeline error
  assert:
    artifacts:
      missing.txt:
        exists: false
`)+"\n")

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts())
	if exitCode != ExitTestFailed {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitTestFailed)
	}
	if len(result.Tests) != 1 || result.Tests[0].Error == nil {
		t.Fatalf("Run() result = %#v", result)
	}
	if !strings.Contains(result.Tests[0].Error.Error(), "run pipeline") {
		t.Fatalf("Run() error = %v", result.Tests[0].Error)
	}
}

func TestRunKeepWorkspacePreservesPassingWorkspace(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/pass.yml", testFileYAML("pass test", "pass-job", "ok"))

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts(RunOptions{KeepWorkspace: true}))
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitOK)
	}
	if len(result.Tests) != 1 || !result.Tests[0].PreservedWorkspace {
		t.Fatalf("Run() tests = %#v", result.Tests)
	}
	if _, err := os.Stat(result.Tests[0].WorkspacePath); err != nil {
		t.Fatalf("workspace was not preserved: %v", err)
	}
	_ = os.RemoveAll(result.Tests[0].WorkspacePath)
}

// TestRunWorkspaceTempDirControlsWorkspaceLocation guards against
// opts.WorkspaceTempDir (wired from GLUT_WORK_DIR) being dead plumbing: when
// GLUT itself runs inside a container that only bind-mounts part of its
// filesystem, temp workspaces must land under a directory the host can see
// instead of the container's own ephemeral /tmp.
func TestRunWorkspaceTempDirControlsWorkspaceLocation(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/pass.yml", testFileYAML("pass test", "pass-job", "ok"))

	tempRoot := t.TempDir()
	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts(RunOptions{
		KeepWorkspace:    true,
		WorkspaceTempDir: tempRoot,
	}))
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitOK)
	}
	if len(result.Tests) != 1 {
		t.Fatalf("Run() tests = %#v", result.Tests)
	}
	defer func() { _ = os.RemoveAll(result.Tests[0].WorkspacePath) }()

	workspacePath := result.Tests[0].WorkspacePath
	rel, err := filepath.Rel(tempRoot, workspacePath)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("workspace path %q is not under WorkspaceTempDir %q", workspacePath, tempRoot)
	}
}

func TestRunnerHelperBranches(t *testing.T) {
	if got := normalizePaths(nil); len(got) != 1 || got[0] != "." {
		t.Fatalf("normalizePaths(nil) = %#v", got)
	}
	if got := normalizePaths([]string{"tests"}); len(got) != 1 || got[0] != "tests" {
		t.Fatalf("normalizePaths(paths) = %#v", got)
	}

	regexMatcher := compilePattern("pass.*test")
	if !regexMatcher(testFileForPattern("pass my test", "tests/a.yml")) {
		t.Fatal("regex matcher did not match test name")
	}
	substringMatcher := compilePattern("[")
	if !substringMatcher(testFileForPattern("literal [ value", "tests/a.yml")) {
		t.Fatal("substring fallback did not match test name")
	}

	if err := maybePause("", "before-pipeline", "workspace"); err != nil {
		t.Fatalf("maybePause inactive error = %v", err)
	}
	withStdinLine(t, func() {
		if err := maybePause("after-pipeline", "before-asserts", "workspace"); err != nil {
			t.Fatalf("maybePause active error = %v", err)
		}
	})
	if err := maybePauseOnFail("on-fail", true, "workspace"); err != nil {
		t.Fatalf("maybePauseOnFail passed error = %v", err)
	}
	withStdinLine(t, func() {
		if err := maybePauseOnFail("on-fail", false, "workspace"); err != nil {
			t.Fatalf("maybePauseOnFail active error = %v", err)
		}
	})

	api := parser.APISetupConfig{
		Project: &parser.ProjectConfig{Path: "group/project"},
	}
	apiTestFile := parser.TestFile{Glut: parser.GlutSection{Setup: parser.SetupConfig{API: &api}}}
	if got := apiConfig(apiTestFile); got.Project == nil || got.Project.Path != "group/project" {
		t.Fatalf("apiConfig() = %#v", got)
	}
	if got := apiConfig(parser.TestFile{}); got.Project != nil {
		t.Fatalf("empty apiConfig() = %#v", got)
	}

	if got := resolveGlutBinPath("custom-glut"); got != "custom-glut" {
		t.Fatalf("resolveGlutBinPath() = %q", got)
	}

	present := true
	if !needsJobList(parser.TestFile{Glut: parser.GlutSection{Assert: config.AssertConfig{Job: map[string]config.JobAssert{
		"build": {Present: &present},
	}}}}) {
		t.Fatal("needsJobList should be true when a present assert exists")
	}
	if needsJobList(parser.TestFile{Glut: parser.GlutSection{Assert: config.AssertConfig{Job: map[string]config.JobAssert{
		"build": {},
	}}}}) {
		t.Fatal("needsJobList should be false without present asserts")
	}

	copied := copyPhaseTimings(map[string]time.Duration{"one": time.Second})
	if copied["one"] != time.Second {
		t.Fatalf("copyPhaseTimings() = %#v", copied)
	}
	messages := errorsToStrings([]error{errors.New("one"), errors.New("two")})
	if strings.Join(messages, ",") != "one,two" {
		t.Fatalf("errorsToStrings() = %#v", messages)
	}
	if errorsToStrings(nil) != nil {
		t.Fatal("errorsToStrings(nil) should be nil")
	}
}

func TestSharedExecutorDeadline(t *testing.T) {
	t.Run("timeout <= 0 means unlimited", func(t *testing.T) {
		parent := context.Background()
		ctx, cancel := sharedExecutorDeadline(parent, 0)
		defer cancel()
		if _, ok := ctx.Deadline(); ok {
			t.Fatal("expected no deadline when timeout <= 0")
		}

		ctxNeg, cancelNeg := sharedExecutorDeadline(parent, -time.Second)
		defer cancelNeg()
		if _, ok := ctxNeg.Deadline(); ok {
			t.Fatal("expected no deadline when timeout is negative")
		}
	})

	t.Run("positive timeout sets one shared deadline", func(t *testing.T) {
		start := time.Now()
		ctx, cancel := sharedExecutorDeadline(context.Background(), 100*time.Millisecond)
		defer cancel()
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("expected a deadline")
		}

		// Simulate a first executor call ("list-jobs") consuming most of the
		// budget, then confirm a second call sees the SAME deadline rather
		// than a fresh 100ms window — this is the fix for ListJobs and Run
		// each getting a full, independent --timeout budget.
		time.Sleep(60 * time.Millisecond)
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > 40*time.Millisecond {
			t.Fatalf("remaining budget = %v, want a shrinking window bounded by the original 100ms deadline", remaining)
		}

		<-ctx.Done()
		elapsed := time.Since(start)
		if elapsed < 90*time.Millisecond || elapsed > 500*time.Millisecond {
			t.Fatalf("context expired after %v, want ~100ms from the original deadline", elapsed)
		}
	})
}

func TestDiscoverAndLoadErrorBranches(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeRawFile(t, "tests/schema-error.yml", strings.TrimSpace(`
job:
  script: echo ok
---
.glut:
  name: schema error
  unknown_key: true
`)+"\n")

	_, err := loadTestFile(filepath.Join(env.workDir, "tests", "schema-error.yml"))
	if err == nil || !strings.Contains(err.Error(), "validate schema") {
		t.Fatalf("loadTestFile() error = %v", err)
	}

	_, err = discoverTests([]string{filepath.Join(env.workDir, "tests", "missing.yml")}, "")
	if err == nil || !strings.Contains(err.Error(), "stat path") {
		t.Fatalf("discoverTests() missing path error = %v", err)
	}

	env.writeRawFile(t, "skip/no-glut.yml", "job:\n  script: echo ok\n")
	tests, err := discoverTests([]string{filepath.Join(env.workDir, "skip")}, "does-not-match")
	if err != nil {
		t.Fatalf("discoverTests() filtered error = %v", err)
	}
	if len(tests) != 0 {
		t.Fatalf("discoverTests() filtered tests = %#v", tests)
	}
}

// TestDiscoverTestsSkipsGitAndGlutTmpDirs guards against `glut run .`
// descending into .git or GLUT's own stale .glut-tmp* workspace copies,
// which would otherwise produce phantom results.
func TestDiscoverTestsSkipsGitAndGlutTmpDirs(t *testing.T) {
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/real.yml", testFileYAML("real test", "job", "ok"))
	env.writeTestFile(t, "tests/.git/phantom.yml", testFileYAML("phantom test", "job", "ok"))
	env.writeTestFile(t, "tests/.glut-tmp-abc123/phantom.yml", testFileYAML("phantom test", "job", "ok"))

	tests, err := discoverTests([]string{filepath.Join(env.workDir, "tests")}, "")
	if err != nil {
		t.Fatalf("discoverTests() error = %v", err)
	}
	if len(tests) != 1 || tests[0].Glut.Name != "real test" {
		t.Fatalf("discoverTests() = %#v, want only the real test", tests)
	}
}

func withStdinLine(t *testing.T, fn func()) {
	t.Helper()

	original := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdin = reader
	defer func() {
		os.Stdin = original
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
	}()

	fn()
}

func testFileForPattern(name string, path string) *parser.TestFile {
	return &parser.TestFile{
		FilePath: path,
		Glut: parser.GlutSection{
			Name: name,
		},
	}
}

type runnerTestEnv struct {
	repoDir string
	hostEnv []string
	workDir string
}

func (env runnerTestEnv) opts(extra ...RunOptions) RunOptions {
	base := RunOptions{HostEnv: env.hostEnv, WorkDir: env.workDir}
	if len(extra) > 0 {
		o := extra[0]
		o.HostEnv = base.HostEnv
		o.WorkDir = base.WorkDir
		return o
	}
	return base
}

func newRunnerTestEnv(t *testing.T) runnerTestEnv {
	t.Helper()
	return newRunnerTestEnvWithScript(t, fakeGitLabCILocalScript())
}

func newRunnerTestEnvWithScript(t *testing.T, gitlabCILocalScript string) runnerTestEnv {
	t.Helper()
	root := t.TempDir()
	repoDir := filepath.Join(root, "repo")
	binDir := filepath.Join(root, "bin")

	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("create repo dir: %v", err)
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}

	writeExecutable(t, binDir, "gitlab-ci-local", gitlabCILocalScript)
	writeExecutable(t, binDir, "glut", "#!/bin/sh\nexit 0\n")

	hostPath := os.Getenv("PATH")
	return runnerTestEnv{
		repoDir: repoDir,
		workDir: repoDir,
		hostEnv: []string{
			"PATH=" + binDir + string(os.PathListSeparator) + hostPath,
			"HOME=" + root,
			"TMPDIR=" + root,
		},
	}
}

func (env runnerTestEnv) writeTestFile(t *testing.T, relativePath string, content string) {
	t.Helper()
	env.writeRawFile(t, relativePath, content)
}

func (env runnerTestEnv) writeRawFile(t *testing.T, relativePath string, content string) {
	t.Helper()

	fullPath := filepath.Join(env.repoDir, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatalf("create parent dir: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("write file %s: %v", fullPath, err)
	}
}

func writeExecutable(t *testing.T, dir string, name string, content string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, name+".cmd")
		if err := os.WriteFile(path, []byte(content), 0755); err != nil {
			t.Fatalf("write executable %s: %v", path, err)
		}
		return
	}
	// Write via temp+rename to avoid ETXTBSY on overlayfs in Docker CI.
	tmp, err := os.CreateTemp(dir, ".tmp-exec-*")
	if err != nil {
		t.Fatalf("create temp executable %s: %v", name, err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		t.Fatalf("write temp executable %s: %v", name, err)
	}
	if err := tmp.Chmod(0755); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		t.Fatalf("chmod temp executable %s: %v", name, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		t.Fatalf("sync temp executable %s: %v", name, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		t.Fatalf("close temp executable %s: %v", name, err)
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, name)); err != nil {
		t.Fatalf("rename temp executable %s: %v", name, err)
	}
}

func testFileYAML(testName string, jobName string, expectedStdout string) string {
	return fmt.Sprintf(`stages: [test]

%s:
  stage: test
  script:
    - echo ok
---
.glut:
  name: %s
  assert:
    job:
      %s:
        present: true
        exit-status: 0
        stdout: %s
`, jobName, testName, jobName, expectedStdout)
}

func fakeGitLabCILocalScript() string {
	return `#!/bin/sh
if [ "$1" = "--list-json" ]; then
  grep '^[A-Za-z0-9_-]\+:' .gitlab-ci.yml | cut -d: -f1 | grep -v '^stages$' | awk 'BEGIN{printf "["} {if (NR>1) printf ","; printf "{\"name\":\"%s\",\"when\":\"on_success\"}", $0} END{print "]"}'
  exit 0
fi

if grep -q 'mock-tool' .gitlab-ci.yml; then
  mock-tool --flag >/dev/null
fi
if grep -q 'CI_API_V4_URL' .gitlab-ci.yml; then
  curl --silent --header "PRIVATE-TOKEN: $CI_JOB_TOKEN" "$CI_API_V4_URL/projects/1" >/dev/null
fi
if grep -q 'artifact.txt' .gitlab-ci.yml; then
  echo artifact > artifact.txt
fi

job_name="$(grep '^[A-Za-z0-9_-]\+:' .gitlab-ci.yml | cut -d: -f1 | grep -v '^stages$' | head -n1)"
stdout="ok"
case "$job_name" in
  fail-job|job-one|job-three|job-four)
    stdout="wrong"
    ;;
esac
printf 'GLUT_JOB|name=%s|exit=0|stdout=%s|stderr=\n' "$job_name" "$stdout"
`
}

// TestRunSetupDefaultBranch verifies that setup.default_branch sets CI_DEFAULT_BRANCH
// correctly and that git.origin.branch does not interfere.
func TestRunSetupDefaultBranch(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnvWithScript(t, fakeGitLabCILocalEnvEchoScript("CI_DEFAULT_BRANCH"))

	env.writeRawFile(t, "tests/default-branch.yml", strings.TrimSpace(`
stages: [test]

check-branch:
  stage: test
  script:
    - echo "$CI_DEFAULT_BRANCH"
---
.glut:
  name: setup.default_branch sets CI_DEFAULT_BRANCH
  setup:
    branch: "feature/my-thing"
    default_branch: "release"
    git:
      origin:
        branch: "feature/my-thing"
  assert:
    job:
      check-branch:
        present: true
        exit-status: 0
        stdout: "release"
`)+"\n")

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts())
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d; result = %#v", exitCode, ExitOK, result)
	}
	if len(result.Tests) != 1 || !result.Tests[0].Passed {
		t.Fatalf("Run() tests = %#v", result.Tests)
	}
}

// TestRunDefaultBranchFallback verifies that CI_DEFAULT_BRANCH defaults to "main"
// when setup.default_branch is not set and the source repo has no origin/HEAD.
func TestRunDefaultBranchFallback(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnvWithScript(t, fakeGitLabCILocalEnvEchoScript("CI_DEFAULT_BRANCH"))

	env.writeRawFile(t, "tests/default-branch-fallback.yml", strings.TrimSpace(`
stages: [test]

check-branch:
  stage: test
  script:
    - echo "$CI_DEFAULT_BRANCH"
---
.glut:
  name: CI_DEFAULT_BRANCH defaults to main
  setup:
    branch: "feature/xyz"
    git:
      origin:
        branch: "feature/xyz"
  assert:
    job:
      check-branch:
        present: true
        exit-status: 0
        stdout: "main"
`)+"\n")

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts())
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d; result = %#v", exitCode, ExitOK, result)
	}
	if len(result.Tests) != 1 || !result.Tests[0].Passed {
		t.Fatalf("Run() tests = %#v", result.Tests)
	}
}

// TestRunDockerModeJobReceivesMockResources verifies BUG-2, BUG-3, and BUG-4 together:
// when a pipeline job uses an image:, GLUT must pass --volume (BUG-2: mock binaries and
// git origin are inside work.Dir) and --extra-host glut-mock:<ip> (BUG-3: so containers
// can reach the API server) and must set CI_API_V4_URL to use glut-mock (BUG-3).
func TestRunDockerModeJobReceivesMockResources(t *testing.T) {
	env := newRunnerTestEnvWithScript(t, dockerAwareFakeGCLScript())

	glutStub := filepath.Join(env.repoDir, "glut-stub")
	writeExecutable(t, env.repoDir, "glut-stub", "#!/bin/sh\nexit 0\n")

	env.writeRawFile(t, "tests/docker-mode.yml", strings.TrimSpace(`
stages: [test]

docker-job:
  image: alpine:latest
  stage: test
  script:
    - mock-tool --flag
    - curl --header "PRIVATE-TOKEN: $CI_JOB_TOKEN" "$CI_API_V4_URL/projects/1"
---
.glut:
  name: docker mode resources
  setup:
    docker: true
    mocks:
      binaries:
        mock-tool:
          executable: "echo mocked"
  assert:
    job:
      docker-job:
        present: true
        exit-status: 0
`)+"\n")

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts(RunOptions{GlutBinPath: glutStub}))
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d; tests = %#v", exitCode, ExitOK, result.Tests)
	}
	if len(result.Tests) != 1 || !result.Tests[0].Passed {
		t.Fatalf("Run() tests = %#v", result.Tests)
	}
}

// TestRunDockerFalseUsesForceShellExecutor verifies that setting docker: false in a test
// causes GLUT to pass --force-shell-executor so all jobs run in the shell regardless of image:.

func TestRunPipelineKeepsRootAndRootlessImageDefinitions(t *testing.T) {
	env := newRunnerTestEnvWithScript(t, fakeGitLabCILocalDockerUserModelScript())
	env.writeRawFile(t, "tests/docker-user-model.yml", strings.TrimSpace(`
stages: [test]

root-job:
  image: alpine:latest
  stage: test
  script:
    - echo "root image"

rootless-job:
  image:
    name: moby/buildkit:rootless
  stage: test
  script:
    - echo "rootless image"
---
.glut:
  name: docker user model
  setup:
    docker: false
  assert:
    job:
      root-job:
        present: true
        exit-status: 0
      rootless-job:
        present: true
        exit-status: 0
	`)+"\n")

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts())
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d; tests = %#v; err = %v", exitCode, ExitOK, result.Tests, result.Tests[0].Error)
	}
	if len(result.Tests) != 1 || !result.Tests[0].Passed {
		t.Fatalf("Run() tests = %#v", result.Tests)
	}
}

func TestRunPipelineKeepsEntrypointVariants(t *testing.T) {
	env := newRunnerTestEnvWithScript(t, fakeGitLabCILocalEntrypointModelScript())
	env.writeRawFile(t, "tests/docker-entrypoint.yml", strings.TrimSpace(`
stages: [test]

default-entrypoint:
  image: moby/buildkit:rootless
  stage: test
  script:
    - echo "default"

override-entrypoint:
  image:
    name: moby/buildkit:rootless
    entrypoint: [""]
  stage: test
  script:
    - echo "override"
---
.glut:
  name: docker entrypoint model
  setup:
    docker: false
  assert:
    job:
      default-entrypoint:
        present: true
        exit-status: 0
      override-entrypoint:
        present: true
        exit-status: 0
	`)+"\n")

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts())
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d; tests = %#v; err = %v", exitCode, ExitOK, result.Tests, result.Tests[0].Error)
	}
	if len(result.Tests) != 1 || !result.Tests[0].Passed {
		t.Fatalf("Run() tests = %#v", result.Tests)
	}
}

func TestRunDockerFalseUsesForceShellExecutor(t *testing.T) {
	env := newRunnerTestEnvWithScript(t, fakeGitLabCILocalForceShellScript())
	env.writeRawFile(t, "tests/docker-false.yml", strings.TrimSpace(`
stages: [test]

shell-job:
  image: alpine:latest
  stage: test
  script:
    - echo ok
---
.glut:
  name: docker false forces shell
  setup:
    docker: false
  assert:
    job:
      shell-job:
        present: true
        exit-status: 0
`)+"\n")

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts())
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d; tests = %#v", exitCode, ExitOK, result.Tests)
	}
	if len(result.Tests) != 1 || !result.Tests[0].Passed {
		t.Fatalf("Run() tests = %#v", result.Tests)
	}
}

// TestRunNilDockerModeUsesNonLocalhostAPIURL verifies that when docker: is absent (nil),
// CI_API_V4_URL uses a non-localhost address (bridge IP) so Docker executor jobs can reach
// the mock API server. Regression test for issue #46.
func TestRunNilDockerModeUsesNonLocalhostAPIURL(t *testing.T) {
	env := newRunnerTestEnvWithScript(t, fakeGitLabCILocalEnvEchoScript("CI_API_V4_URL"))
	env.writeRawFile(t, "tests/nil-docker.yml", strings.TrimSpace(`
stages: [test]

image-job:
  image: alpine:latest
  stage: test
  script:
    - echo "$CI_API_V4_URL"
---
.glut:
  name: nil docker url
  setup:
    branch: main
  assert:
    job:
      image-job:
        present: true
        exit-status: 0
`)+"\n")

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts())
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d; tests = %#v", exitCode, ExitOK, result.Tests)
	}
	if len(result.Tests) != 1 || !result.Tests[0].Passed {
		t.Fatalf("Run() tests = %#v", result.Tests)
	}
	url := result.Tests[0].JobOutputs["image-job"].Stdout
	if strings.Contains(url, "127.0.0.1") || strings.Contains(url, "localhost") {
		t.Errorf("nil docker: CI_API_V4_URL = %q uses localhost; want bridge IP so Docker jobs can reach the mock server", url)
	}
}

func TestApplyDockerCompatibilityEnv(t *testing.T) {
	t.Run("sets_umask_false_in_docker_mode", func(t *testing.T) {
		envVars := map[string]string{}
		applyDockerCompatibilityEnv(envVars, true)
		if envVars["GCL_UMASK"] != "false" {
			t.Fatalf("GCL_UMASK = %q, want false", envVars["GCL_UMASK"])
		}
	})

	t.Run("does_not_set_umask_in_shell_mode", func(t *testing.T) {
		envVars := map[string]string{}
		applyDockerCompatibilityEnv(envVars, false)
		if _, ok := envVars["GCL_UMASK"]; ok {
			t.Fatalf("GCL_UMASK must be unset in shell mode")
		}
	})
}

func TestResolveDockerMode(t *testing.T) {
	trueVal := true
	falseVal := false

	useDocker, forceShell := resolveDockerMode(nil)
	if !useDocker || forceShell {
		t.Fatalf("nil docker: useDocker=%v forceShell=%v, want useDocker=true forceShell=false", useDocker, forceShell)
	}

	useDocker, forceShell = resolveDockerMode(&trueVal)
	if !useDocker || forceShell {
		t.Fatalf("true docker: useDocker=%v forceShell=%v, want useDocker=true forceShell=false", useDocker, forceShell)
	}

	useDocker, forceShell = resolveDockerMode(&falseVal)
	if useDocker || !forceShell {
		t.Fatalf("false docker: useDocker=%v forceShell=%v, want useDocker=false forceShell=true", useDocker, forceShell)
	}
}

// TestSetupPrivilegedPassedToExecutor asserts setup.privileged through
// behavior — a fake gitlab-ci-local script reports whether --privileged was
// in its argv — rather than re-evaluating the production expression inline
// against itself (which can never fail regardless of what the code does).
func TestSetupPrivilegedPassedToExecutor(t *testing.T) {
	env := newRunnerTestEnvWithScript(t, `#!/bin/sh
found=false
for arg in "$@"; do
  if [ "$arg" = "--privileged" ]; then
    found=true
  fi
done
job_name="$(grep '^[A-Za-z0-9_-]\+:' .gitlab-ci.yml | cut -d: -f1 | grep -v '^stages$' | head -n1)"
printf 'GLUT_JOB|name=%s|exit=0|stdout=privileged=%s|stderr=\n' "$job_name" "$found"
`)

	cases := []struct {
		name       string
		setupBlock string
		want       string
	}{
		{name: "privileged true passes --privileged", setupBlock: "  setup:\n    privileged: true\n", want: "privileged=true"},
		{name: "privileged false omits --privileged", setupBlock: "  setup:\n    privileged: false\n", want: "privileged=false"},
		{name: "privileged omitted omits --privileged", setupBlock: "", want: "privileged=false"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fileName := "tests/" + strings.ReplaceAll(tc.name, " ", "-") + ".yml"
			yaml := "stages: [test]\n\n" +
				"job:\n" +
				"  stage: test\n" +
				"  script:\n" +
				"    - echo ok\n" +
				"---\n" +
				".glut:\n" +
				"  name: " + tc.name + "\n" +
				tc.setupBlock +
				"  assert:\n" +
				"    job:\n" +
				"      job:\n" +
				fmt.Sprintf("        stdout: %q\n", tc.want)
			env.writeRawFile(t, fileName, yaml)

			result, exitCode := Run(context.Background(), []string{fileName}, env.opts())
			if exitCode != ExitOK {
				t.Fatalf("Run() exit = %d, want %d; tests = %#v", exitCode, ExitOK, result.Tests)
			}
			if len(result.Tests) != 1 || !result.Tests[0].Passed {
				t.Fatalf("Run() tests = %#v", result.Tests)
			}
		})
	}
}

func fakeGitLabCILocalListErrorScript() string {
	return `#!/bin/sh
if [ "$1" = "--list-json" ]; then
  echo "list failed" >&2
  exit 2
fi
printf 'GLUT_JOB|name=list-job|exit=0|stdout=ok|stderr=\n'
`
}

// dockerAwareFakeGCLScript returns a fake gitlab-ci-local script that simulates Docker
// isolation failures when the required flags are absent. If the pipeline declares image:
// and --volume / --extra-host are missing, or CI_API_V4_URL uses 127.0.0.1 (unreachable
// from containers), the script emits a job marker with exit=127.
func dockerAwareFakeGCLScript() string {
	return `#!/bin/sh
HAS_VOLUME=0
HAS_EXTRA_HOST=0
IS_LIST=0
for arg in "$@"; do
  case "$arg" in
    --volume) HAS_VOLUME=1 ;;
    --extra-host) HAS_EXTRA_HOST=1 ;;
    --list-json) IS_LIST=1 ;;
  esac
done

if [ "$IS_LIST" = "1" ]; then
  grep '^[A-Za-z0-9_-]\+:' .gitlab-ci.yml | cut -d: -f1 | grep -v '^stages$' | awk 'BEGIN{printf "["} {if (NR>1) printf ","; printf "{\"name\":\"%s\",\"when\":\"on_success\"}", $0} END{print "]"}'
  exit 0
fi

job_name="$(grep '^[A-Za-z0-9_-]\+:' .gitlab-ci.yml | cut -d: -f1 | grep -v '^stages$' | head -n1)"

if grep -qE '^  image:' .gitlab-ci.yml 2>/dev/null; then
  if [ "$HAS_VOLUME" = "0" ]; then
    printf 'GLUT_JOB|name=%s|exit=127|stdout=|stderr=docker: mock binaries not mounted (missing --volume)\n' "$job_name"
    exit 0
  fi
  if [ "$HAS_EXTRA_HOST" = "0" ]; then
    printf 'GLUT_JOB|name=%s|exit=127|stdout=|stderr=docker: host unreachable (missing --extra-host)\n' "$job_name"
    exit 0
  fi
  if echo "$CI_API_V4_URL" | grep -q '127.0.0.1'; then
    printf 'GLUT_JOB|name=%s|exit=127|stdout=|stderr=docker: CI_API_V4_URL uses 127.0.0.1 which is unreachable from containers (BUG-3)\n' "$job_name"
    exit 0
  fi
fi

if grep -q 'mock-tool' .gitlab-ci.yml; then
  mock-tool --flag >/dev/null
fi
if grep -q 'CI_API_V4_URL' .gitlab-ci.yml; then
  curl --silent --header "PRIVATE-TOKEN: $CI_JOB_TOKEN" "$CI_API_V4_URL/projects/1" >/dev/null
fi
printf 'GLUT_JOB|name=%s|exit=0|stdout=ok|stderr=\n' "$job_name"
`
}

func fakeGitLabCILocalDockerUserModelScript() string {
	return `#!/bin/sh
if [ "$1" = "--list-json" ]; then
  printf '[{"name":"root-job","when":"on_success"},{"name":"rootless-job","when":"on_success"}]\n'
  exit 0
fi

if ! grep -q '^root-job:' .gitlab-ci.yml; then
  printf 'GLUT_JOB|name=root-job|exit=127|stdout=|stderr=root job missing in pipeline\n'
  exit 0
fi
if ! grep -q '^rootless-job:' .gitlab-ci.yml; then
  printf 'GLUT_JOB|name=rootless-job|exit=127|stdout=|stderr=rootless job missing in pipeline\n'
  exit 0
fi

printf 'GLUT_JOB|name=root-job|exit=0|stdout=ok|stderr=\n'
printf 'GLUT_JOB|name=rootless-job|exit=0|stdout=ok|stderr=\n'
`
}

func fakeGitLabCILocalEntrypointModelScript() string {
	return `#!/bin/sh
if [ "$1" = "--list-json" ]; then
  printf '[{"name":"default-entrypoint","when":"on_success"},{"name":"override-entrypoint","when":"on_success"}]\n'
  exit 0
fi

if ! grep -q '^default-entrypoint:' .gitlab-ci.yml; then
  printf 'GLUT_JOB|name=default-entrypoint|exit=127|stdout=|stderr=default job missing\n'
  exit 0
fi
if ! grep -Fq 'entrypoint: [""]' .gitlab-ci.yml; then
  printf 'GLUT_JOB|name=override-entrypoint|exit=127|stdout=|stderr=entrypoint override missing\n'
  exit 0
fi

printf 'GLUT_JOB|name=default-entrypoint|exit=0|stdout=ok|stderr=\n'
printf 'GLUT_JOB|name=override-entrypoint|exit=0|stdout=ok|stderr=\n'
`
}

// fakeGitLabCILocalForceShellScript returns a fake gitlab-ci-local that emits a
// successful job only when --force-shell-executor appears in the argument list.
func fakeGitLabCILocalForceShellScript() string {
	return `#!/bin/sh
HAS_FORCE_SHELL=0
for arg in "$@"; do
  [ "$arg" = "--force-shell-executor" ] && HAS_FORCE_SHELL=1
done

if [ "$1" = "--list-json" ]; then
  grep '^[A-Za-z0-9_-]\+:' .gitlab-ci.yml | cut -d: -f1 | grep -v '^stages$' | awk 'BEGIN{printf "["} {if (NR>1) printf ","; printf "{\"name\":\"%s\",\"when\":\"on_success\"}", $0} END{print "]"}'
  exit 0
fi

job_name="$(grep '^[A-Za-z0-9_-]\+:' .gitlab-ci.yml | cut -d: -f1 | grep -v '^stages$' | head -n1)"
if [ "$HAS_FORCE_SHELL" = "0" ]; then
  printf 'GLUT_JOB|name=%s|exit=127|stdout=|stderr=expected --force-shell-executor but it was absent\n' "$job_name"
else
  printf 'GLUT_JOB|name=%s|exit=0|stdout=ok|stderr=\n' "$job_name"
fi
`
}

func fakeGitLabCILocalPipelineErrorScript() string {
	return `#!/bin/sh
echo "pipeline failed before jobs" >&2
exit 1
`
}

type recordingSink struct {
	events []string
}

func (s *recordingSink) Start(totalTests int) {
	s.events = append(s.events, "start:"+strconv(totalTests))
}

func (s *recordingSink) TestRetry(testName string, _ error) {
	s.events = append(s.events, "retry:"+testName)
}

func (s *recordingSink) TestDone(result TestResult) {
	s.events = append(s.events, "test:"+result.TestName)
}

func (s *recordingSink) Summary(result RunResult) {
	s.events = append(s.events, "summary:"+strconv(result.Passed)+"/"+strconv(result.Failed))
}

func strconv(value int) string {
	return fmt.Sprintf("%d", value)
}

func TestRunUsesGetWdWhenWorkDirEmpty(t *testing.T) {
	t.Parallel()
	// WorkDir="" causes Run to call os.Getwd(). Non-existent path forces
	// discoverTests to fail, exercising the empty-workdir branch.
	result, exitCode := Run(context.Background(), []string{"/nonexistent-path-xyz-9999"}, RunOptions{})
	if exitCode != ExitRunnerError {
		t.Errorf("expected ExitRunnerError, got %d", exitCode)
	}
	if result.Error == nil {
		t.Error("expected an error when path does not exist")
	}
}

func TestListSkipsParseErrorFiles(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeRawFile(t, "tests/broken.yml", "job: [broken")
	env.writeTestFile(t, "tests/ok.yml", testFileYAML("ok test", "job", "ok"))

	listed, err := List(context.Background(), []string{"tests"}, ListOptions{WorkDir: env.workDir})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	for _, item := range listed {
		if strings.Contains(item.FilePath, "broken") {
			t.Errorf("broken.yml should be skipped in listing, got %s", item.FilePath)
		}
	}
}

func TestRunFilePathIsRelativeToWorkDir(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/sub/pass.yml", testFileYAML("pass test", "pass-job", "ok"))

	result, exitCode := Run(context.Background(), []string{"tests"}, env.opts())
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitOK)
	}
	if len(result.Tests) != 1 {
		t.Fatalf("Run() tests = %d, want 1", len(result.Tests))
	}

	want := filepath.Join("tests", "sub", "pass.yml")
	if result.Tests[0].FilePath != want {
		t.Errorf("FilePath = %q, want %q (relative to work dir)", result.Tests[0].FilePath, want)
	}
}

func TestListFilePathIsRelativeToWorkDir(t *testing.T) {
	t.Parallel()
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/sub/pass.yml", testFileYAML("pass test", "pass-job", "ok"))

	tests, err := List(context.Background(), []string{"tests"}, ListOptions{WorkDir: env.workDir})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(tests) != 1 {
		t.Fatalf("List() = %d tests, want 1", len(tests))
	}

	want := filepath.Join("tests", "sub", "pass.yml")
	if tests[0].FilePath != want {
		t.Errorf("List() FilePath = %q, want %q (relative to work dir)", tests[0].FilePath, want)
	}
}

func TestRelPath(t *testing.T) {
	t.Parallel()

	t.Run("returns_relative_path", func(t *testing.T) {
		t.Parallel()
		got := relPath("/repo", "/repo/tests/a.yml")
		if got != filepath.Join("tests", "a.yml") {
			t.Errorf("relPath() = %q, want %q", got, filepath.Join("tests", "a.yml"))
		}
	})

	t.Run("empty_base_returns_path_unchanged", func(t *testing.T) {
		t.Parallel()
		got := relPath("", "/abs/path.yml")
		if got != "/abs/path.yml" {
			t.Errorf("relPath() = %q, want /abs/path.yml", got)
		}
	})
}

func TestAbsifyPaths(t *testing.T) {
	t.Parallel()

	t.Run("empty_workdir_returns_normalized", func(t *testing.T) {
		t.Parallel()
		got := absifyPaths([]string{"tests"}, "")
		if len(got) != 1 {
			t.Fatalf("expected 1 path, got %v", got)
		}
	})

	t.Run("relative_path_joined_with_workdir", func(t *testing.T) {
		t.Parallel()
		got := absifyPaths([]string{"tests"}, "/repo")
		if got[0] != "/repo/tests" {
			t.Errorf("expected /repo/tests, got %q", got[0])
		}
	})

	t.Run("absolute_path_unchanged", func(t *testing.T) {
		t.Parallel()
		got := absifyPaths([]string{"/absolute/path"}, "/repo")
		if got[0] != "/absolute/path" {
			t.Errorf("expected /absolute/path, got %q", got[0])
		}
	})
}

// fakeGitLabCILocalEnvEchoScript returns a fake gitlab-ci-local that emits the
// value of the named environment variable as the job's stdout field.
func fakeGitLabCILocalEnvEchoScript(envVar string) string {
	return `#!/bin/sh
if [ "$1" = "--list-json" ]; then
  grep '^[A-Za-z0-9_-]\+:' .gitlab-ci.yml | cut -d: -f1 | grep -v '^stages$' | awk 'BEGIN{printf "["} {if (NR>1) printf ","; printf "{\"name\":\"%s\",\"when\":\"on_success\"}", $0} END{print "]"}'
  exit 0
fi
job_name="$(grep '^[A-Za-z0-9_-]\+:' .gitlab-ci.yml | cut -d: -f1 | grep -v '^stages$' | head -n1)"
printf 'GLUT_JOB|name=%s|exit=0|stdout=%s|stderr=\n' "$job_name" "$` + envVar + `"
`
}

func TestResolveGlutBinPath(t *testing.T) {
	t.Parallel()

	t.Run("explicit_path_returned_as_is", func(t *testing.T) {
		t.Parallel()
		got := resolveGlutBinPath("/custom/glut")
		if got != "/custom/glut" {
			t.Errorf("expected /custom/glut, got %q", got)
		}
	})

	t.Run("empty_path_returns_executable_or_fallback", func(t *testing.T) {
		t.Parallel()
		got := resolveGlutBinPath("")
		if got == "" {
			t.Error("expected non-empty result")
		}
	})
}
