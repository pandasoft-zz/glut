package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pandasoft-zz/glut/internal/runner"
)

const validCmdTest = `
test_job:
  script: echo ok
---
.glut:
  name: cmd smoke test
  setup:
    docker: false
  assert:
    job:
      test_job:
        exit-status: 0
`

const lintErrorCmdTest = `
test_job:
  script: echo ok
---
.glut:
  name: broken assert
  assert:
    job:
      missing-job:
        exit-status: 0
`

func TestLintMainExitCodes(t *testing.T) {
	t.Parallel()

	t.Run("clean file passes", func(t *testing.T) {
		t.Parallel()
		var out, errOut bytes.Buffer
		path := writeTempTest(t, validCmdTest)
		if code := lintMain([]string{path}, "text", &out, &errOut); code != runner.ExitOK {
			t.Fatalf("lintMain() = %v, want ExitOK; stderr: %s", code, errOut.String())
		}
	})

	t.Run("lint error fails the command", func(t *testing.T) {
		t.Parallel()
		var out, errOut bytes.Buffer
		path := writeTempTest(t, lintErrorCmdTest)
		if code := lintMain([]string{path}, "text", &out, &errOut); code != runner.ExitTestFailed {
			t.Fatalf("lintMain() = %v, want ExitTestFailed", code)
		}
	})

	t.Run("unsupported format is a runner error", func(t *testing.T) {
		t.Parallel()
		var out, errOut bytes.Buffer
		path := writeTempTest(t, validCmdTest)
		if code := lintMain([]string{path}, "xml", &out, &errOut); code != runner.ExitRunnerError {
			t.Fatalf("lintMain() = %v, want ExitRunnerError", code)
		}
	})

	t.Run("missing default tests dir is a runner error", func(t *testing.T) {
		t.Parallel()
		var out, errOut bytes.Buffer
		if code := lintMain(nil, "text", &out, &errOut); code != runner.ExitRunnerError {
			t.Fatalf("lintMain(nil) = %v, want ExitRunnerError (no ./tests/ here)", code)
		}
		if !strings.Contains(errOut.String(), "does not exist") {
			t.Fatalf("stderr = %q, want an actionable missing-dir message", errOut.String())
		}
	})
}

func TestDoctorMainExitCodes(t *testing.T) {
	t.Parallel()

	t.Run("clean file passes with hints output", func(t *testing.T) {
		t.Parallel()
		var out, errOut bytes.Buffer
		path := writeTempTest(t, validCmdTest)
		if code := doctorMain([]string{path}, "text", "", &out, &errOut); code != runner.ExitOK {
			t.Fatalf("doctorMain() = %v, want ExitOK; stderr: %s", code, errOut.String())
		}
		if out.Len() == 0 {
			t.Fatal("doctor must produce a report on stdout")
		}
	})

	t.Run("unsupported format is a runner error", func(t *testing.T) {
		t.Parallel()
		var out, errOut bytes.Buffer
		path := writeTempTest(t, validCmdTest)
		if code := doctorMain([]string{path}, "xml", "", &out, &errOut); code != runner.ExitRunnerError {
			t.Fatalf("doctorMain() = %v, want ExitRunnerError", code)
		}
	})

	t.Run("lint errors fail doctor", func(t *testing.T) {
		t.Parallel()
		var out, errOut bytes.Buffer
		path := writeTempTest(t, lintErrorCmdTest)
		if code := doctorMain([]string{path}, "text", "", &out, &errOut); code != runner.ExitTestFailed {
			t.Fatalf("doctorMain() = %v, want ExitTestFailed", code)
		}
		// The text report (including issues) goes to stdout.
		if !strings.Contains(out.String(), "missing-job") {
			t.Fatalf("stdout = %q, want the failing assert.job reference", out.String())
		}
	})

	t.Run("json format renders the report", func(t *testing.T) {
		t.Parallel()
		var out, errOut bytes.Buffer
		path := writeTempTest(t, validCmdTest)
		if code := doctorMain([]string{path}, "json", "", &out, &errOut); code != runner.ExitOK {
			t.Fatalf("doctorMain(json) = %v, want ExitOK", code)
		}
		if !strings.Contains(out.String(), "\"files\"") && !strings.Contains(out.String(), "{") {
			t.Fatalf("json doctor output = %q", out.String())
		}
	})
}

func TestListMain(t *testing.T) {
	t.Parallel()

	t.Run("lists discovered tests", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeTestFile(t, dir, "smoke.yml", validCmdTest)
		var out, errOut bytes.Buffer
		if code := listMain([]string{dir}, "", &out, &errOut); code != runner.ExitOK {
			t.Fatalf("listMain() = %v, want ExitOK; stderr: %s", code, errOut.String())
		}
		if !strings.Contains(out.String(), "cmd smoke test") {
			t.Fatalf("list output = %q, want the test name", out.String())
		}
	})

	t.Run("nonexistent path is a runner error", func(t *testing.T) {
		t.Parallel()
		var out, errOut bytes.Buffer
		missing := filepath.Join(t.TempDir(), "nope")
		if code := listMain([]string{missing}, "", &out, &errOut); code != runner.ExitRunnerError {
			t.Fatalf("listMain() = %v, want ExitRunnerError", code)
		}
	})
}

func TestRunMainErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("invalid report target is a runner error", func(t *testing.T) {
		t.Parallel()
		var out, errOut bytes.Buffer
		flags := &runFlags{reports: []string{"bogus-format:x.out"}}
		if code := runMain(context.Background(), flags, []string{t.TempDir()}, &out, &errOut); code != runner.ExitRunnerError {
			t.Fatalf("runMain() = %v, want ExitRunnerError; stderr: %s", code, errOut.String())
		}
	})

	t.Run("nonexistent path is a runner error", func(t *testing.T) {
		t.Parallel()
		var out, errOut bytes.Buffer
		flags := &runFlags{quiet: true}
		missing := filepath.Join(t.TempDir(), "nope")
		if code := runMain(context.Background(), flags, []string{missing}, &out, &errOut); code != runner.ExitRunnerError {
			t.Fatalf("runMain() = %v, want ExitRunnerError; stderr: %s", code, errOut.String())
		}
		if errOut.Len() == 0 {
			t.Fatal("the runner error must be printed to stderr")
		}
	})

	t.Run("interactive mode without a terminal fails cleanly", func(t *testing.T) {
		t.Parallel()
		var out, errOut bytes.Buffer
		flags := &runFlags{interactive: true}
		// selectAndRun prints the TTY error to os.Stderr itself; runMain only
		// reports the exit code, so that is all this test can assert on.
		if code := runMain(context.Background(), flags, []string{t.TempDir()}, &out, &errOut); code != runner.ExitRunnerError {
			t.Fatalf("interactive runMain() without a TTY = %v, want ExitRunnerError", code)
		}
	})
}

func TestVersionCommandPrintsVersion(t *testing.T) {
	t.Parallel()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}
	if !strings.Contains(out.String(), "glut v") || !strings.Contains(out.String(), "commit:") {
		t.Fatalf("version output = %q", out.String())
	}
}
