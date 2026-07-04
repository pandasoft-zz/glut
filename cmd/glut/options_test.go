package main

import (
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRunOptionsFromCommandUsesGlobalsAndDefaultPath(t *testing.T) {
	saveRunGlobals := func() {
		oldPattern := runPattern
		oldFailFast := runFailFast
		oldMaxFail := runMaxFail
		oldVerbose := runVerbose
		oldQuiet := runQuiet
		oldFormat := runFormat
		oldReports := append([]string(nil), runReports...)
		oldTimeout := runTimeout
		oldDebug := runDebug
		oldKeepWorkspace := runKeepWorkspace
		oldDebugPause := runDebugPause
		oldKeepLastFailed := runKeepLastFailed
		t.Cleanup(func() {
			runPattern = oldPattern
			runFailFast = oldFailFast
			runMaxFail = oldMaxFail
			runVerbose = oldVerbose
			runQuiet = oldQuiet
			runFormat = oldFormat
			runReports = oldReports
			runTimeout = oldTimeout
			runDebug = oldDebug
			runKeepWorkspace = oldKeepWorkspace
			runDebugPause = oldDebugPause
			runKeepLastFailed = oldKeepLastFailed
		})
	}
	saveRunGlobals()

	runPattern = "release"
	runFailFast = true
	runMaxFail = 2
	runVerbose = true
	runQuiet = true
	runFormat = "json"
	runReports = []string{"junit:report.xml"}
	runTimeout = 3 * time.Minute
	runDebug = true
	runKeepWorkspace = true
	runDebugPause = "on-fail"
	runKeepLastFailed = 4

	opts := runOptionsFromCommand(nil)
	if !reflect.DeepEqual(opts.Paths, []string{"."}) {
		t.Fatalf("Paths = %#v", opts.Paths)
	}
	if opts.Pattern != "release" || !opts.FailFast || opts.MaxFail != 2 || !opts.Verbose || !opts.Quiet {
		t.Fatalf("basic options = %+v", opts)
	}
	if opts.Format != "json" || opts.Timeout != 3*time.Minute || !opts.Debug || !opts.KeepWorkspace {
		t.Fatalf("format/debug options = %+v", opts)
	}
	if opts.DebugPause != "on-fail" || opts.KeepLastFailed != 4 {
		t.Fatalf("debug pause options = %+v", opts)
	}
	if !reflect.DeepEqual(opts.Reports, []string{"junit:report.xml"}) {
		t.Fatalf("Reports = %#v", opts.Reports)
	}

	runReports[0] = "tap:report.tap"
	if opts.Reports[0] != "junit:report.xml" {
		t.Fatalf("Reports must be copied, got %#v", opts.Reports)
	}
}

func TestListAndLintOptionsUseDefaultPaths(t *testing.T) {
	oldPattern := listPattern
	oldLintFormat := lintFormat
	listPattern = "smoke"
	lintFormat = "json"
	t.Cleanup(func() {
		listPattern = oldPattern
		lintFormat = oldLintFormat
	})

	listOpts := listOptionsFromCommand(nil)
	if !reflect.DeepEqual(listOpts.Paths, []string{"."}) || listOpts.Pattern != "smoke" {
		t.Fatalf("list options = %+v", listOpts)
	}

	lintOpts := lintOptionsFromCommand(nil)
	if !reflect.DeepEqual(lintOpts.Paths, []string{"./tests/"}) || lintOpts.Format != "json" {
		t.Fatalf("lint options = %+v", lintOpts)
	}

	custom := lintOptionsFromCommand([]string{"custom"})
	if !reflect.DeepEqual(custom.Paths, []string{"custom"}) {
		t.Fatalf("custom lint paths = %#v", custom.Paths)
	}
}

func TestCheckDefaultTestsDirExists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	missing := dir + "/does-not-exist/"

	if err := checkDefaultTestsDirExists([]string{missing}, true); err == nil {
		t.Fatal("expected an error when the default test directory is missing")
	} else if !strings.Contains(err.Error(), missing) {
		t.Fatalf("error should name the missing directory, got: %v", err)
	}

	if err := checkDefaultTestsDirExists([]string{missing}, false); err != nil {
		t.Fatalf("an explicit (non-default) path should never be checked, got: %v", err)
	}

	if err := checkDefaultTestsDirExists([]string{dir}, true); err != nil {
		t.Fatalf("an existing default directory should not error, got: %v", err)
	}
}

func TestEnvHelpers(t *testing.T) {
	t.Parallel()
	mkEnv := func(val string) func(string) string {
		return func(string) string { return val }
	}

	if !envBool(mkEnv("yes"), "ANY") {
		t.Fatal("envBool should accept yes")
	}
	if !envBool(mkEnv("1"), "ANY") {
		t.Fatal("envBool should accept 1")
	}
	if !envBool(mkEnv("true"), "ANY") {
		t.Fatal("envBool should accept true")
	}
	if envBool(mkEnv("no"), "ANY") {
		t.Fatal("envBool should reject no")
	}
	if envBool(mkEnv(""), "ANY") {
		t.Fatal("envBool should reject empty")
	}

	if got := envDuration(mkEnv("2m"), "ANY", time.Second); got != 2*time.Minute {
		t.Fatalf("duration = %v", got)
	}
	if got := envDuration(mkEnv("bad"), "ANY", time.Second); got != time.Second {
		t.Fatalf("fallback duration = %v", got)
	}
	if got := envDuration(mkEnv(""), "ANY", defaultRunTimeout); got != defaultRunTimeout {
		t.Fatalf("empty duration = %v", got)
	}

	if got := envList(mkEnv(" junit:a.xml, ,tap:b.tap "), "ANY"); !reflect.DeepEqual(got, []string{"junit:a.xml", "tap:b.tap"}) {
		t.Fatalf("envList = %#v", got)
	}
	if got := envList(mkEnv(""), "ANY"); got != nil {
		t.Fatalf("empty envList = %#v", got)
	}
}

// TestEnvDurationWarnsOnInvalidValue is intentionally not parallel: it
// temporarily swaps the process-wide os.Stderr to capture the warning.
func TestEnvDurationWarnsOnInvalidValue(t *testing.T) {
	mkEnv := func(val string) func(string) string {
		return func(string) string { return val }
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	oldStderr := os.Stderr
	os.Stderr = w
	got := envDuration(mkEnv("10minutes"), "GLUT_TIMEOUT", time.Minute)
	os.Stderr = oldStderr
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read captured stderr: %v", err)
	}

	if got != time.Minute {
		t.Fatalf("envDuration() = %v, want fallback %v", got, time.Minute)
	}
	if !strings.Contains(string(out), "GLUT_TIMEOUT") || !strings.Contains(string(out), "10minutes") {
		t.Fatalf("expected a warning naming the env var and invalid value, got: %q", out)
	}
}
