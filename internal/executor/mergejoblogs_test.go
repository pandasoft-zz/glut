package executor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeJobLogsOverwritesStdoutFromLogFiles(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	logDir := filepath.Join(workspace, ".gitlab-ci-local", "output")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}

	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(logDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("build.log", "full container output\n")
	write("deploy%2Fweb.log", "deploy output\n") // URL-encoded job name
	write("empty.log", "")                       // must be skipped
	write("notes.txt", "not a log file")         // wrong extension, skipped

	jobs := map[string]JobOutput{
		"build": {Name: "build", Present: true, Stdout: "truncated", ExitStatus: 0, StatusKnown: true},
	}
	mergeJobLogs(jobs, workspace)

	if got := jobs["build"].Stdout; got != "full container output\n" {
		t.Fatalf("build stdout = %q, want the log file content", got)
	}
	if !jobs["build"].StatusKnown {
		t.Fatal("merging a log file must not reset StatusKnown")
	}
	deploy, ok := jobs["deploy/web"]
	if !ok || deploy.Stdout != "deploy output\n" {
		t.Fatalf("deploy/web = %#v, want URL-decoded job with log content", deploy)
	}
	if _, ok := jobs["empty"]; ok {
		t.Fatal("empty log file must not create a job entry")
	}
	if _, ok := jobs["notes"]; ok {
		t.Fatal("non-.log file must not create a job entry")
	}
}

func TestMergeJobLogsMissingDirIsNoOp(t *testing.T) {
	t.Parallel()
	jobs := map[string]JobOutput{"build": {Name: "build"}}
	mergeJobLogs(jobs, t.TempDir())
	if len(jobs) != 1 {
		t.Fatalf("jobs mutated without a log dir: %#v", jobs)
	}
}
