package executor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pandasoft-zz/glut/internal/config"
)

func TestRunIsolatesEnvironmentAndCapturesJobs(t *testing.T) {
	hostPath := os.Getenv("PATH")
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}

	mockDir := filepath.Join(tempDir, "mock-bin")
	if err := os.MkdirAll(mockDir, 0755); err != nil {
		t.Fatalf("create mock dir: %v", err)
	}

	if err := writeExecutable(binDir, "gitlab-ci-local", runScript(mockDir)); err != nil {
		t.Fatalf("write mock gitlab-ci-local: %v", err)
	}

	t.Setenv("PATH", joinPath(binDir, hostPath))
	t.Setenv("HOME", tempDir)
	t.Setenv("TMP", tempDir)
	t.Setenv("HOST_SECRET", "should-not-leak")
	t.Setenv(config.EnvMockLogDir, filepath.Join(tempDir, "mock-logs"))
	t.Setenv(config.EnvMockBinReal, filepath.Join(tempDir, "mock-real"))

	cfg := ExecutorConfig{
		WorkspacePath: tempDir,
		PipelineYAML:  "job:\n  script: echo hi\n",
		EnvVars: map[string]string{
			"CI_JOB_NAME": "build",
		},
		MockBinPath: mockDir,
	}

	result, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	job, ok := result.Jobs["build"]
	if !ok {
		t.Fatalf("expected build job, got %#v", result.Jobs)
	}
	if job.ExitStatus != 0 {
		t.Fatalf("job exit = %d, want 0", job.ExitStatus)
	}
	if job.Stdout != "job ok" {
		t.Fatalf("job stdout = %q, want %q", job.Stdout, "job ok")
	}
	if job.Stderr != "warn" {
		t.Fatalf("job stderr = %q, want %q", job.Stderr, "warn")
	}
	if strings.Contains(result.RawStdout, "HOST_SECRET_PRESENT=yes") {
		t.Fatal("expected host env var to stay hidden")
	}
	if !strings.Contains(result.RawStdout, "FIRST_PATH="+mockDir) {
		t.Fatalf("expected mock path first, got stdout %q", result.RawStdout)
	}

	pipelineData, err := os.ReadFile(filepath.Join(tempDir, pipelineFileName))
	if err != nil {
		t.Fatalf("read pipeline file: %v", err)
	}
	if string(pipelineData) != cfg.PipelineYAML {
		t.Fatalf("pipeline file = %q, want %q", string(pipelineData), cfg.PipelineYAML)
	}
}

func TestRunTimeoutReturnsClearError(t *testing.T) {
	hostPath := os.Getenv("PATH")
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}

	if err := writeExecutable(binDir, "gitlab-ci-local", sleepScript()); err != nil {
		t.Fatalf("write timeout script: %v", err)
	}

	t.Setenv("PATH", joinPath(binDir, hostPath))
	t.Setenv("HOME", tempDir)
	t.Setenv("TMP", tempDir)

	_, err := Run(context.Background(), ExecutorConfig{
		WorkspacePath: tempDir,
		PipelineYAML:  "job:\n  script: sleep 1\n",
		Timeout:       50 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "test timeout after 50ms") {
		t.Fatalf("timeout error = %q", err)
	}
}

func TestListJobsParsesNames(t *testing.T) {
	hostPath := os.Getenv("PATH")
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}

	if err := writeExecutable(binDir, "gitlab-ci-local", listScript()); err != nil {
		t.Fatalf("write list script: %v", err)
	}

	t.Setenv("PATH", joinPath(binDir, hostPath))
	t.Setenv("HOME", tempDir)
	t.Setenv("TMP", tempDir)

	jobs, err := ListJobs(context.Background(), ExecutorConfig{
		WorkspacePath: tempDir,
		PipelineYAML:  "job:\n  script: echo hi\n",
	})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}

	want := []string{"build", "test"}
	if strings.Join(jobs, ",") != strings.Join(want, ",") {
		t.Fatalf("ListJobs() = %#v, want %#v", jobs, want)
	}
}

func TestCheckDependenciesReportsMissingCommands(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}

	for _, name := range []string{"gitlab-ci-local", "git", "bash"} {
		if err := writeExecutable(binDir, name, versionScript(name)); err != nil {
			t.Fatalf("write dependency script %s: %v", name, err)
		}
	}

	t.Setenv("PATH", binDir)

	problems := CheckDependencies(context.Background())
	if len(problems) != 1 {
		t.Fatalf("CheckDependencies() problems = %#v, want one optional rsync message", problems)
	}
	if !strings.Contains(problems[0], "rsync") {
		t.Fatalf("expected rsync warning, got %#v", problems)
	}
}

func writeExecutable(dir string, name string, content string) error {
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, name+".cmd")
		return os.WriteFile(path, []byte(content), 0755)
	}
	path := filepath.Join(dir, name)
	return os.WriteFile(path, []byte(content), 0755)
}

func runScript(mockDir string) string {
	if runtime.GOOS == "windows" {
		return "@echo off\r\n" +
			"if defined HOST_SECRET echo HOST_SECRET_PRESENT=yes\r\n" +
			"for /f \"tokens=1 delims=;\" %%A in (\"%PATH%\") do echo FIRST_PATH=%%A\r\n" +
			"echo GLUT_JOB^|name=build^|exit=0^|stdout=job ok^|stderr=warn\r\n"
	}
	return "#!/bin/sh\n" +
		"if [ -n \"$HOST_SECRET\" ]; then echo HOST_SECRET_PRESENT=yes; fi\n" +
		"echo FIRST_PATH=\"${PATH%%:*}\"\n" +
		"echo 'GLUT_JOB|name=build|exit=0|stdout=job ok|stderr=warn'\n"
}

func sleepScript() string {
	if runtime.GOOS == "windows" {
		return "@echo off\r\nping 127.0.0.1 -n 3 >nul\r\n"
	}
	return "#!/bin/sh\nsleep 1\n"
}

func listScript() string {
	if runtime.GOOS == "windows" {
		return "@echo off\r\necho build\r\necho test\r\n"
	}
	return "#!/bin/sh\necho build\necho test\n"
}

func versionScript(name string) string {
	if runtime.GOOS == "windows" {
		return "@echo off\r\necho " + name + " version\r\n"
	}
	return "#!/bin/sh\necho '" + name + " version'\n"
}

func joinPath(first string, rest string) string {
	if rest == "" {
		return first
	}
	return first + string(os.PathListSeparator) + rest
}
