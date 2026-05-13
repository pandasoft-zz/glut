package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/parser"
)

func TestRunPassingTest(t *testing.T) {
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/pass.yml", testFileYAML("pass test", "pass-job", "ok"))

	result, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{})
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
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/fail.yml", testFileYAML("fail test", "fail-job", "expected"))

	result, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{})
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

	result, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{})
	if exitCode != ExitRunnerError {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitRunnerError)
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "setup.merge_request is missing") {
		t.Fatalf("Run() error = %v, want merge_request missing context", result.Error)
	}
}

func TestRunReturnsErrorMessageForInvalidDebugPause(t *testing.T) {
	result, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{DebugPause: "bad"})
	if exitCode != ExitRunnerError {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitRunnerError)
	}
	if result.Error == nil || !strings.Contains(result.Error.Error(), "invalid debug pause point") {
		t.Fatalf("Run() error = %v, want debug pause context", result.Error)
	}
}

func TestRunFailFastStopsAfterFirstFailure(t *testing.T) {
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/01-fail.yml", testFileYAML("first fail", "job-one", "want"))
	env.writeTestFile(t, "tests/02-pass.yml", testFileYAML("second pass", "job-two", "ok"))

	result, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{FailFast: true})
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
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/01-fail.yml", testFileYAML("first fail", "job-one", "want"))
	env.writeTestFile(t, "tests/02-fail.yml", testFileYAML("second fail", "job-three", "want"))
	env.writeTestFile(t, "tests/03-pass.yml", testFileYAML("third pass", "job-two", "ok"))

	result, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{MaxFail: 2})
	if exitCode != ExitTestFailed {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitTestFailed)
	}
	if len(result.Tests) != 2 {
		t.Fatalf("Run() ran %d tests, want 2", len(result.Tests))
	}
}

func TestListSupportsPatternAndRecursiveDiscovery(t *testing.T) {
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/a/alpha.yml", testFileYAML("alpha", "job-alpha", "ok"))
	env.writeTestFile(t, "tests/b/beta.yml", testFileYAML("beta", "job-beta", "ok"))

	tests, err := List(context.Background(), []string{"tests"}, ListOptions{RunPattern: "beta"})
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

func TestRunPreservesOnlyLastFailedWorkspaces(t *testing.T) {
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/01.yml", testFileYAML("fail one", "job-one", "want"))
	env.writeTestFile(t, "tests/02.yml", testFileYAML("fail two", "job-three", "want"))
	env.writeTestFile(t, "tests/03.yml", testFileYAML("fail three", "job-four", "want"))

	result, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{KeepLastFailed: 2})
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
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/pass.yml", testFileYAML("pass test", "pass-job", "ok"))

	sink := &recordingSink{}
	_, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{
		Progress: []ProgressSink{sink},
	})
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitOK)
	}

	want := []string{"start:1", "test:pass test", "summary:1/0"}
	if strings.Join(sink.events, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %#v, want %#v", sink.events, want)
	}
}

func TestRunDebugPauseBeforePipelineContinues(t *testing.T) {
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/pass.yml", testFileYAML("pass test", "pass-job", "ok"))

	withStdinLine(t, func() {
		result, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{DebugPause: "before-pipeline"})
		if exitCode != ExitOK {
			t.Fatalf("Run() exit = %d, want %d; result = %#v", exitCode, ExitOK, result)
		}
		if len(result.Tests) != 1 || !result.Tests[0].Passed {
			t.Fatalf("Run() tests = %#v", result.Tests)
		}
	})
}

func TestRunDebugKeepsFailureDiagnostics(t *testing.T) {
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/fail.yml", testFileYAML("fail test", "fail-job", "expected"))

	result, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{Debug: true})
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

func TestRunRecordsMockBinaryCalls(t *testing.T) {
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

	result, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{GlutBinPath: logger})
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d; result = %#v", exitCode, ExitOK, result)
	}
	if len(result.Tests) != 1 || !result.Tests[0].Passed {
		t.Fatalf("Run() tests = %#v", result.Tests)
	}
}

func TestRunChecksArtifactsAndAPICalls(t *testing.T) {
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

	result, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{})
	if exitCode != ExitOK {
		t.Fatalf("Run() exit = %d, want %d; result = %#v", exitCode, ExitOK, result)
	}
	if len(result.Tests) != 1 || !result.Tests[0].Passed {
		t.Fatalf("Run() tests = %#v", result.Tests)
	}
}

func TestRunReportsMockBinarySetupError(t *testing.T) {
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
    mocks:
      binaries:
        mock-tool:
          executable: "echo mocked"
  assert:
    job:
      mock-job:
        present: true
`)+"\n")

	result, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{GlutBinPath: filepath.Join(env.repoDir, "missing-glut")})
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

	result, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{})
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

	result, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{})
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
	env := newRunnerTestEnv(t)
	env.writeTestFile(t, "tests/pass.yml", testFileYAML("pass test", "pass-job", "ok"))

	result, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{KeepWorkspace: true})
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

func TestRunnerHelperBranches(t *testing.T) {
	if got := normalizePaths(nil); len(got) != 1 || got[0] != "." {
		t.Fatalf("normalizePaths(nil) = %#v", got)
	}
	if got := normalizePaths([]string{"tests"}); len(got) != 1 || got[0] != "tests" {
		t.Fatalf("normalizePaths(paths) = %#v", got)
	}

	regexMatcher, err := compilePattern("pass.*test")
	if err != nil {
		t.Fatal(err)
	}
	if !regexMatcher(testFileForPattern("pass my test", "tests/a.yml")) {
		t.Fatal("regex matcher did not match test name")
	}
	substringMatcher, err := compilePattern("[")
	if err != nil {
		t.Fatal(err)
	}
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

func TestDiscoverAndLoadErrorBranches(t *testing.T) {
	env := newRunnerTestEnv(t)
	env.writeRawFile(t, "tests/schema-error.yml", strings.TrimSpace(`
job:
  script: echo ok
---
.glut:
  name: schema error
  unknown_key: true
`)+"\n")

	_, err := loadTestFile(filepath.Join("tests", "schema-error.yml"))
	if err == nil || !strings.Contains(err.Error(), "validate schema") {
		t.Fatalf("loadTestFile() error = %v", err)
	}

	_, err = discoverTests([]string{filepath.Join("tests", "missing.yml")}, "")
	if err == nil || !strings.Contains(err.Error(), "stat path") {
		t.Fatalf("discoverTests() missing path error = %v", err)
	}

	env.writeRawFile(t, "skip/no-glut.yml", "job:\n  script: echo ok\n")
	tests, err := discoverTests([]string{"skip"}, "does-not-match")
	if err != nil {
		t.Fatalf("discoverTests() filtered error = %v", err)
	}
	if len(tests) != 0 {
		t.Fatalf("discoverTests() filtered tests = %#v", tests)
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

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", root)
	t.Setenv("TMPDIR", root)
	t.Chdir(repoDir)

	return runnerTestEnv{repoDir: repoDir}
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

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
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
if [ "$1" = "--list" ]; then
  grep '^[A-Za-z0-9_-]\+:' .gitlab-ci.yml | cut -d: -f1 | grep -v '^stages$'
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

func fakeGitLabCILocalListErrorScript() string {
	return `#!/bin/sh
if [ "$1" = "--list" ]; then
  echo "list failed" >&2
  exit 2
fi
printf 'GLUT_JOB|name=list-job|exit=0|stdout=ok|stderr=\n'
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

func (s *recordingSink) TestDone(result TestResult) {
	s.events = append(s.events, "test:"+result.TestName)
}

func (s *recordingSink) Summary(result RunResult) {
	s.events = append(s.events, "summary:"+strconv(result.Passed)+"/"+strconv(result.Failed))
}

func strconv(value int) string {
	return fmt.Sprintf("%d", value)
}
