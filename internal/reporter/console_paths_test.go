package reporter

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pandasoft-zz/glut/internal/asserter"
	"github.com/pandasoft-zz/glut/internal/executor"
	"github.com/pandasoft-zz/glut/internal/mockserver"
	"github.com/pandasoft-zz/glut/internal/mockwrapper"
	"github.com/pandasoft-zz/glut/internal/runner"
)

func failedDebugResult() runner.TestResult {
	return runner.TestResult{
		FilePath: "tests/release.yml",
		TestName: "release test",
		Passed:   false,
		Duration: 1200 * time.Millisecond,
		Failures: []asserter.AssertResult{
			{Path: "job.release.exit-status", Passed: false, Expected: "0", Actual: "1"},
		},
		Error: errors.New("run pipeline: boom"),
		JobOutputs: map[string]executor.JobOutput{
			"release": {Name: "release", Present: true, Executed: true, ExitStatus: 1, Stdout: "line1\nline2", StatusKnown: true},
		},
		WorkspacePath:      "/tmp/glut-x",
		PreservedWorkspace: true,
		Debug: &runner.DebugData{
			RawStdout:       "raw out",
			RawStderr:       "raw err",
			BinaryLogs:      map[string][]mockwrapper.BinaryCall{"release-cli": {{Name: "release-cli", Args: []string{"create"}}}},
			APICalls:        []mockserver.APICall{{Method: "POST", Path: "/api/v4/projects/1/releases"}},
			WorkspaceGitLog: "* abc123 commit",
			OriginGitLog:    "* abc123 commit",
			PhaseTimings:    map[string]time.Duration{"pipeline": time.Second},
			CleanupErrors:   []string{"stop mock server: boom"},
		},
	}
}

func passedResult() runner.TestResult {
	return runner.TestResult{
		FilePath: "tests/ok.yml",
		TestName: "ok test",
		Passed:   true,
		Duration: 40 * time.Millisecond,
		JobOutputs: map[string]executor.JobOutput{
			"build": {Name: "build", Present: true, Executed: true, Stdout: "done", StatusKnown: true},
		},
		PreservedWorkspace: true,
		WorkspacePath:      "/tmp/glut-keep",
	}
}

// TestConsoleSinksRenderFailureAndDebugDetail drives each console format
// through a failed test with full debug data and a passed verbose test, so
// the failure/debug rendering paths are exercised end to end.
func TestConsoleSinksRenderFailureAndDebugDetail(t *testing.T) {
	t.Parallel()

	summary := runner.RunResult{
		Tests:    []runner.TestResult{failedDebugResult(), passedResult()},
		Passed:   1,
		Failed:   1,
		Duration: 2 * time.Second,
	}

	for _, format := range []string{"pretty", "dots", "json"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			sink, err := NewConsole(ConsoleOptions{Format: format, Verbose: true, Debug: true, Writer: &out})
			if err != nil {
				t.Fatalf("NewConsole(%s) error = %v", format, err)
			}
			sink.Start(2)
			sink.TestDone(failedDebugResult())
			sink.TestDone(passedResult())
			sink.Summary(summary)

			got := out.String()
			if !strings.Contains(got, "release") {
				t.Fatalf("%s output missing the failed test:\n%s", format, got)
			}
			if format != "json" && !strings.Contains(got, "workspace kept") {
				t.Fatalf("%s output missing the preserved-workspace note:\n%s", format, got)
			}
		})
	}
}

// TestJSONConsoleQuietSkipsPassedTests pins the quiet JSON contract: passed
// tests are omitted, failed tests and the summary are still emitted.
func TestJSONConsoleQuietSkipsPassedTests(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	sink, err := NewConsole(ConsoleOptions{Format: "json", Quiet: true, Writer: &out})
	if err != nil {
		t.Fatal(err)
	}
	sink.TestDone(passedResult())
	if out.Len() != 0 {
		t.Fatalf("quiet json must skip passed tests, got %q", out.String())
	}
	sink.TestDone(failedDebugResult())
	if !strings.Contains(out.String(), "release") {
		t.Fatalf("quiet json must still emit failures, got %q", out.String())
	}
	sink.Summary(runner.RunResult{Failed: 1, Duration: time.Second})
	if !strings.Contains(out.String(), "\"failed\":1") {
		t.Fatalf("summary missing, got %q", out.String())
	}
}

func TestTestDisplayNameFallsBackToFilePath(t *testing.T) {
	t.Parallel()
	if got := testDisplayName(runner.TestResult{FilePath: "a.yml"}); got != "a.yml" {
		t.Fatalf("testDisplayName() = %q, want the file path fallback", got)
	}
	if got := testDisplayName(runner.TestResult{FilePath: "a.yml", TestName: "named"}); got != "named" {
		t.Fatalf("testDisplayName() = %q, want the test name", got)
	}
}
