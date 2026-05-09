package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
stages: [test]

bad-assert:
  stage: test
  script:
    - echo ok
---
.glut:
  name: bad-assert
  assert:
    job:
      missing-job: {}
`)+"\n")

	_, exitCode := Run(context.Background(), []string{"tests"}, RunOptions{})
	if exitCode != ExitRunnerError {
		t.Fatalf("Run() exit = %d, want %d", exitCode, ExitRunnerError)
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

type runnerTestEnv struct {
	repoDir string
}

func newRunnerTestEnv(t *testing.T) runnerTestEnv {
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

	writeExecutable(t, binDir, "gitlab-ci-local", fakeGitLabCILocalScript())
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
