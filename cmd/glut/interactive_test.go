package main

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/pandasoft-zz/glut/internal/runner"
)

// TestToRunnerOptionsMapsAllFields guards against the ~15-field translation
// between CLI RunOptions and runner.RunOptions drifting out of sync between
// the normal and interactive run paths — both now call this single function.
func TestToRunnerOptionsMapsAllFields(t *testing.T) {
	t.Setenv("GLUT_WORK_DIR", "/repo/.glut-tmp")
	opts := RunOptions{
		Pattern:              "pattern",
		FailFast:             true,
		MaxFail:              3,
		Verbose:              true,
		Quiet:                true,
		Timeout:              5 * time.Second,
		WaitTimeout:          10 * time.Second,
		Debug:                true,
		KeepWorkspace:        true,
		DebugPause:           "before-pipeline",
		KeepLastFailed:       2,
		CopyStrategy:         "rsync",
		DockerVolumeStrategy: "bind",
		Include:              []string{"a", "b"},
	}
	sinks := []runner.ProgressSink{}
	dockerOut := io.Discard

	got := toRunnerOptions(opts, sinks, dockerOut)

	want := runner.RunOptions{
		RunPattern:           "pattern",
		FailFast:             true,
		MaxFail:              3,
		Verbose:              true,
		Quiet:                true,
		Timeout:              5 * time.Second,
		WaitTimeout:          10 * time.Second,
		Debug:                true,
		KeepWorkspace:        true,
		DebugPause:           "before-pipeline",
		KeepLastFailed:       2,
		CopyStrategy:         "rsync",
		DockerVolumeStrategy: "bind",
		Include:              []string{"a", "b"},
		Progress:             sinks,
		DockerWaitOutput:     dockerOut,
		WorkspaceTempDir:     "/repo/.glut-tmp",
	}

	if got.RunPattern != want.RunPattern || got.FailFast != want.FailFast || got.MaxFail != want.MaxFail ||
		got.Verbose != want.Verbose || got.Quiet != want.Quiet || got.Timeout != want.Timeout ||
		got.WaitTimeout != want.WaitTimeout || got.Debug != want.Debug || got.KeepWorkspace != want.KeepWorkspace ||
		got.DebugPause != want.DebugPause || got.KeepLastFailed != want.KeepLastFailed ||
		got.CopyStrategy != want.CopyStrategy || got.DockerVolumeStrategy != want.DockerVolumeStrategy ||
		len(got.Include) != len(want.Include) || got.WorkspaceTempDir != want.WorkspaceTempDir {
		t.Fatalf("toRunnerOptions() = %#v, want %#v", got, want)
	}
}

// TestRunSelectedTestsReportsFileWriteError guards against writeFileReports
// failure being swallowed: the interactive path used to return
// ExitRunnerError with no message and no result.Error set, unlike the
// non-interactive path which prints the error and sets it on the result.
func TestRunSelectedTestsReportsFileWriteError(t *testing.T) {
	emptyDir := t.TempDir()
	// "junit:<dir>" points the report at a directory, not a file path, so
	// reporter.FileReport.WriteFile must fail.
	opts := RunOptions{
		Reports: []string{"junit:" + emptyDir},
	}

	// An existing, empty directory makes discoverTests succeed with zero
	// tests, so runner.Run returns cleanly (ExitOK, Error: nil) — isolating
	// the assertion to the report-write failure that follows.
	result, code := runSelectedTests(context.Background(), opts, []string{emptyDir})

	if code != runner.ExitRunnerError {
		t.Fatalf("code = %v, want ExitRunnerError", code)
	}
	if result.Error == nil {
		t.Fatal("expected result.Error to be set when the file report write fails")
	}
}

func TestRunSelectedTestsSucceedsWithNoReports(t *testing.T) {
	emptyDir := t.TempDir()
	opts := RunOptions{}

	result, code := runSelectedTests(context.Background(), opts, []string{emptyDir})
	if code != runner.ExitOK {
		t.Fatalf("code = %v, want ExitOK", code)
	}
	if result.Error != nil {
		t.Fatalf("result.Error = %v, want nil", result.Error)
	}
}

func TestRunSelectedTestsInvalidReportTarget(t *testing.T) {
	dir := t.TempDir()
	opts := RunOptions{
		Reports: []string{"not-a-valid-target"},
	}

	result, code := runSelectedTests(context.Background(), opts, []string{filepath.Join(dir, "x.yml")})
	if code != runner.ExitRunnerError {
		t.Fatalf("code = %v, want ExitRunnerError", code)
	}
	if result.Error == nil {
		t.Fatal("expected result.Error to be set for an invalid report target")
	}
}
