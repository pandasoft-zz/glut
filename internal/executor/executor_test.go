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
	t.Parallel()
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

	cfg := ExecutorConfig{
		WorkspacePath: tempDir,
		PipelineYAML:  "job:\n  script: echo hi\n",
		EnvVars: map[string]string{
			"CI_JOB_NAME": "build",
		},
		MockBinPath: mockDir,
		HostEnv: []string{
			"PATH=" + joinPath(binDir, hostPath),
			"HOME=" + tempDir,
			"TMP=" + tempDir,
			"HOST_SECRET=should-not-leak",
			config.EnvMockLogDir + "=" + filepath.Join(tempDir, "mock-logs"),
			config.EnvMockBinReal + "=" + filepath.Join(tempDir, "mock-real"),
		},
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
	t.Parallel()
	hostPath := os.Getenv("PATH")
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}

	if err := writeExecutable(binDir, "gitlab-ci-local", sleepScript()); err != nil {
		t.Fatalf("write timeout script: %v", err)
	}

	_, err := Run(context.Background(), ExecutorConfig{
		WorkspacePath: tempDir,
		PipelineYAML:  "job:\n  script: sleep 1\n",
		Timeout:       50 * time.Millisecond,
		HostEnv: []string{
			"PATH=" + joinPath(binDir, hostPath),
			"HOME=" + tempDir,
			"TMP=" + tempDir,
		},
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "test timeout after 50ms") {
		t.Fatalf("timeout error = %q", err)
	}
}

func TestRunAllowsFailedPipelineWhenJobOutputWasCaptured(t *testing.T) {
	t.Parallel()
	hostPath := os.Getenv("PATH")
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}

	if err := writeExecutable(binDir, "gitlab-ci-local", failedJobScript()); err != nil {
		t.Fatalf("write failing gitlab-ci-local: %v", err)
	}

	result, err := Run(context.Background(), ExecutorConfig{
		WorkspacePath: tempDir,
		PipelineYAML:  "failing-job:\n  script: exit 7\n",
		HostEnv: []string{
			"PATH=" + joinPath(binDir, hostPath),
			"HOME=" + tempDir,
			"TMP=" + tempDir,
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil for captured job failure", err)
	}
	job := result.Jobs["failing-job"]
	if job.ExitStatus != 7 || job.Stderr != "denied" {
		t.Fatalf("job = %#v", job)
	}
}

func TestRunReturnsCommandErrorWhenNoJobWasCaptured(t *testing.T) {
	t.Parallel()
	hostPath := os.Getenv("PATH")
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}

	if err := writeExecutable(binDir, "gitlab-ci-local", badArgsScript()); err != nil {
		t.Fatalf("write bad gitlab-ci-local: %v", err)
	}

	_, err := Run(context.Background(), ExecutorConfig{
		WorkspacePath: tempDir,
		PipelineYAML:  "job:\n  script: echo hi\n",
		HostEnv: []string{
			"PATH=" + joinPath(binDir, hostPath),
			"HOME=" + tempDir,
			"TMP=" + tempDir,
		},
	})
	if err == nil {
		t.Fatal("Run() error = nil, want command error")
	}
	if !strings.Contains(err.Error(), "Unknown arguments") {
		t.Fatalf("Run() error = %v, want command output", err)
	}
}

func TestListJobsParsesNames(t *testing.T) {
	t.Parallel()
	hostPath := os.Getenv("PATH")
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}

	if err := writeExecutable(binDir, "gitlab-ci-local", listScript()); err != nil {
		t.Fatalf("write list script: %v", err)
	}

	jobs, err := ListJobs(context.Background(), ExecutorConfig{
		WorkspacePath: tempDir,
		PipelineYAML:  "job:\n  script: echo hi\n",
		HostEnv: []string{
			"PATH=" + joinPath(binDir, hostPath),
			"HOME=" + tempDir,
			"TMP=" + tempDir,
		},
	})
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}

	want := []string{"build", "test"}
	if strings.Join(jobs, ",") != strings.Join(want, ",") {
		t.Fatalf("ListJobs() = %#v, want %#v", jobs, want)
	}
}

func TestListJobsReportsCommandAndTimeoutErrors(t *testing.T) {
	t.Parallel()
	hostPath := os.Getenv("PATH")

	t.Run("command error", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		binDir := filepath.Join(tempDir, "bin")
		if err := os.MkdirAll(binDir, 0755); err != nil {
			t.Fatalf("create bin dir: %v", err)
		}
		if err := writeExecutable(binDir, "gitlab-ci-local", badArgsScript()); err != nil {
			t.Fatalf("write bad gitlab-ci-local: %v", err)
		}

		_, err := ListJobs(context.Background(), ExecutorConfig{
			WorkspacePath: tempDir,
			PipelineYAML:  "job:\n  script: echo hi\n",
			HostEnv: []string{
				"PATH=" + joinPath(binDir, hostPath),
				"HOME=" + tempDir,
				"TMP=" + tempDir,
			},
		})
		if err == nil || !strings.Contains(err.Error(), "list gitlab-ci-local jobs") {
			t.Fatalf("ListJobs() error = %v", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		binDir := filepath.Join(tempDir, "bin")
		if err := os.MkdirAll(binDir, 0755); err != nil {
			t.Fatalf("create bin dir: %v", err)
		}
		if err := writeExecutable(binDir, "gitlab-ci-local", sleepScript()); err != nil {
			t.Fatalf("write timeout script: %v", err)
		}

		_, err := ListJobs(context.Background(), ExecutorConfig{
			WorkspacePath: tempDir,
			PipelineYAML:  "job:\n  script: echo hi\n",
			Timeout:       50 * time.Millisecond,
			HostEnv: []string{
				"PATH=" + joinPath(binDir, hostPath),
				"HOME=" + tempDir,
				"TMP=" + tempDir,
			},
		})
		if err == nil || !strings.Contains(err.Error(), "test timeout after 50ms") {
			t.Fatalf("ListJobs() error = %v", err)
		}
	})
}

// TestGitLabCILocalDockerModeAddsVolumeAndExtraHost verifies BUG-2 and BUG-4: when
// UseDocker is true the executor must pass --volume (so mock binaries and the git
// origin inside work.Dir are reachable from inside Docker containers) and --extra-host
// (glut-mock and host.docker.internal aliases pointing to the GLUT bridge IP).
func TestGitLabCILocalDockerModeAddsVolumeAndExtraHost(t *testing.T) {
	hostPath := os.Getenv("PATH")
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}

	if err := writeExecutable(binDir, "gitlab-ci-local", echoArgsScript()); err != nil {
		t.Fatalf("write echo-args script: %v", err)
	}

	t.Setenv("PATH", joinPath(binDir, hostPath))
	t.Setenv("HOME", tempDir)
	t.Setenv("TMP", tempDir)

	volumeMount := tempDir + ":" + tempDir
	extraHost := "host.docker.internal:host-gateway"

	result, err := Run(context.Background(), ExecutorConfig{
		WorkspacePath:    tempDir,
		PipelineYAML:     "job:\n  image: alpine\n  script: echo hi\n",
		UseDocker:        true,
		DockerVolumes:    []string{volumeMount},
		DockerExtraHosts: []string{extraHost},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(result.RawStdout, "--volume") {
		t.Errorf("--volume missing from gitlab-ci-local args; stdout = %q", result.RawStdout)
	}
	if !strings.Contains(result.RawStdout, volumeMount) {
		t.Errorf("volume mount %q missing from args; stdout = %q", volumeMount, result.RawStdout)
	}
	if !strings.Contains(result.RawStdout, "--extra-host") {
		t.Errorf("--extra-host missing from gitlab-ci-local args; stdout = %q", result.RawStdout)
	}
	if !strings.Contains(result.RawStdout, extraHost) {
		t.Errorf("extra host %q missing from args; stdout = %q", extraHost, result.RawStdout)
	}
	if strings.Contains(result.RawStdout, "--shell-executor-no-image") {
		t.Errorf("--shell-executor-no-image must not appear in docker mode; stdout = %q", result.RawStdout)
	}
}

func TestGitLabCILocalArgumentsMatchVendoredVersion(t *testing.T) {
	t.Parallel()
	args := append(baseArgs(ExecutorConfig{}), envArgs(map[string]string{"CI": "true"})...)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--shell-executor-no-image") {
		t.Fatalf("args = %q, want shell executor flag", joined)
	}
	if strings.Contains(joined, "--force-shell-executor") {
		t.Fatalf("args = %q, must not use force-shell flag for default mode", joined)
	}
	if !strings.Contains(joined, "--variable CI=true") {
		t.Fatalf("args = %q, want --variable", joined)
	}
	if strings.Contains(joined, "--env") {
		t.Fatalf("args = %q, must not use unsupported --env", joined)
	}
}

func TestGitLabCILocalForceShellUsesForceShellExecutor(t *testing.T) {
	args := baseArgs(ExecutorConfig{ForceShell: true})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--force-shell-executor") {
		t.Fatalf("args = %q, want --force-shell-executor", joined)
	}
	if strings.Contains(joined, "--shell-executor-no-image") {
		t.Fatalf("args = %q, --shell-executor-no-image must not appear alongside --force-shell-executor", joined)
	}
}

func TestGitLabCILocalDockerModeHasNoShellFlag(t *testing.T) {
	args := baseArgs(ExecutorConfig{UseDocker: true})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--force-shell-executor") {
		t.Fatalf("args = %q, docker mode must not use force-shell", joined)
	}
	if strings.Contains(joined, "--shell-executor-no-image") {
		t.Fatalf("args = %q, docker mode must not use shell-executor-no-image", joined)
	}
}

func TestParseJobOutputsFromGitLabCILocalLogs(t *testing.T) {
	t.Parallel()
	stdout := strings.Join([]string{
		"build:image starting shell (test)",
		"build:image $ echo ok",
		"build:image > ok",
		"",
		" PASS  build:image",
		"release:publish starting shell (deploy)",
		"release:publish $ exit 7",
		"",
		" FAIL  release:publish",
		"  > $ exit 7",
	}, "\n")
	stderr := strings.Join([]string{
		"release:publish > denied",
		"release:publish finished in 19 ms  FAIL 7 ",
	}, "\n")

	jobs := parseJobOutputs(stdout, stderr)
	if jobs["build:image"].ExitStatus != 0 || jobs["build:image"].Stdout != "ok" {
		t.Fatalf("build job = %#v", jobs["build:image"])
	}
	if jobs["release:publish"].ExitStatus != 7 || jobs["release:publish"].Stderr != "denied" {
		t.Fatalf("release job = %#v", jobs["release:publish"])
	}
}

func TestParseJobOutputsHandlesAllowFailureWarnStatus(t *testing.T) {
	t.Parallel()
	stdout := strings.Join([]string{
		"check-fail starting shell (test)",
		"check-fail $ exit 2",
		"check-fail finished in 15 ms  WARN 2 ",
		"",
		" WARN  check-fail  pre_script",
		"  > $ exit 2",
	}, "\n")

	jobs := parseJobOutputs(stdout, "")
	job, ok := jobs["check-fail"]
	if !ok {
		t.Fatalf("expected check-fail job to be present, got %#v", jobs)
	}
	if job.ExitStatus != 2 {
		t.Fatalf("exit status = %d, want 2", job.ExitStatus)
	}
}

func TestParseJobOutputsHandlesMultilineAndMissingFailCode(t *testing.T) {
	t.Parallel()
	stdout := strings.Join([]string{
		"test-job > first",
		"test-job > second",
		" FAIL  test-job",
	}, "\n")

	jobs := parseJobOutputs(stdout, "")
	job := jobs["test-job"]
	if job.ExitStatus != 1 {
		t.Fatalf("exit status = %d, want 1", job.ExitStatus)
	}
	if job.Stdout != "first\nsecond" {
		t.Fatalf("stdout = %q", job.Stdout)
	}
}

func TestParseJobListIgnoresToolWarningsOnStderr(t *testing.T) {
	t.Parallel()
	jobs := parseJobList("build\ntest\n", "Using fallback git data\n")
	if strings.Join(jobs, ",") != "build,test" {
		t.Fatalf("jobs = %#v", jobs)
	}
}

func TestExecutorHelperErrorBranches(t *testing.T) {
	t.Parallel()
	if err := writePipeline(ExecutorConfig{}); err == nil {
		t.Fatal("writePipeline should require workspace path")
	}
	if err := writePipeline(ExecutorConfig{WorkspacePath: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("writePipeline should fail for missing workspace directory")
	}

	jobs := map[string]JobOutput{}
	parseJobMarkers(jobs, "not a marker\nGLUT_JOB|exit=0|stdout=missing name\n")
	if len(jobs) != 0 {
		t.Fatalf("parseJobMarkers should ignore invalid lines, got %#v", jobs)
	}
	if output, ok := parseJobMarker("GLUT_JOB|name=build|exit=bad|stdout=ok"); ok || output.Name != "" {
		t.Fatalf("parseJobMarker bad exit = %#v %v", output, ok)
	}
	if got := statusFromGitLab("FAIL", ""); got != 1 {
		t.Fatalf("statusFromGitLab missing code = %d", got)
	}
	if got := statusFromGitLab("PASS", "7"); got != 0 {
		t.Fatalf("statusFromGitLab pass = %d", got)
	}
	if got := firstLine("", "fallback"); got != "fallback" {
		t.Fatalf("firstLine fallback = %q", got)
	}
}

func TestCheckDependenciesReportsMissingCommands(t *testing.T) {
	t.Parallel()
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

	problems := CheckDependencies(context.Background(), []string{"PATH=" + binDir})
	if len(problems) != 1 {
		t.Fatalf("CheckDependencies() problems = %#v, want one optional rsync message", problems)
	}
	if !strings.Contains(problems[0], "rsync") {
		t.Fatalf("expected rsync warning, got %#v", problems)
	}
}

func TestCheckDependenciesReportsMissingRequiredCommand(t *testing.T) {
	t.Parallel()
	problems := CheckDependencies(context.Background(), []string{"PATH="})
	required := []string{"gitlab-ci-local", "git", "bash"}
	for _, name := range required {
		found := false
		for _, p := range problems {
			if strings.Contains(p, name) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected problem for required binary %q, got %#v", name, problems)
		}
	}
}

func TestCheckDependenciesReportsBrokenCommands(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create bin dir: %v", err)
	}
	failScript := "#!/bin/sh\nexit 42\n"
	for _, name := range []string{"gitlab-ci-local", "git", "bash", "rsync"} {
		if err := writeExecutable(binDir, name, failScript); err != nil {
			t.Fatalf("write fail script %s: %v", name, err)
		}
	}
	problems := CheckDependencies(context.Background(), []string{"PATH=" + binDir})
	if len(problems) < 4 {
		t.Fatalf("CheckDependencies() problems = %#v, want problems for all 4 binaries", problems)
	}
	for _, name := range []string{"gitlab-ci-local", "git", "bash", "rsync"} {
		found := false
		for _, p := range problems {
			if strings.Contains(p, name) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected problem for binary %q, got %#v", name, problems)
		}
	}
}

func echoArgsScript() string {
	if runtime.GOOS == "windows" {
		return "@echo off\r\necho %*\r\n"
	}
	return "#!/bin/sh\necho \"$@\"\n"
}

func writeExecutable(dir string, name string, content string) error {
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, name+".cmd")
		return os.WriteFile(path, []byte(content), 0755)
	}
	// Write via temp+rename to avoid ETXTBSY on overlayfs in Docker CI.
	tmp, err := os.CreateTemp(dir, ".tmp-exec-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(0755); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, filepath.Join(dir, name))
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

func failedJobScript() string {
	if runtime.GOOS == "windows" {
		return "@echo off\r\n" +
			"echo failing-job ^> denied 1>&2\r\n" +
			"echo failing-job finished in 19 ms  FAIL 7 1>&2\r\n" +
			"exit /b 1\r\n"
	}
	return "#!/bin/sh\n" +
		"echo 'failing-job > denied' >&2\n" +
		"echo 'failing-job finished in 19 ms  FAIL 7 ' >&2\n" +
		"exit 1\n"
}

func badArgsScript() string {
	if runtime.GOOS == "windows" {
		return "@echo off\r\necho Unknown arguments: env 1>&2\r\nexit /b 1\r\n"
	}
	return "#!/bin/sh\necho 'Unknown arguments: env' >&2\nexit 1\n"
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

func TestBuildCommandEnvForwardsDockerVars(t *testing.T) {
	t.Parallel()

	t.Run("forwards docker vars when set", func(t *testing.T) {
		t.Parallel()
		cfg := ExecutorConfig{
			HostEnv: []string{
				"HOME=/home/runner",
				"PATH=/usr/bin",
				"DOCKER_HOST=tcp://docker:2375",
				"DOCKER_TLS_VERIFY=1",
				"DOCKER_CERT_PATH=/certs/client",
				"DOCKER_CONFIG=/home/runner/.docker",
				"SECRET=should-not-leak",
			},
		}
		result := envSliceToMap(buildCommandEnv(cfg))

		for _, key := range []string{"DOCKER_HOST", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH", "DOCKER_CONFIG"} {
			if _, ok := result[key]; !ok {
				t.Errorf("expected %s to be forwarded, got env %v", key, result)
			}
		}
		if result["DOCKER_HOST"] != "tcp://docker:2375" {
			t.Errorf("DOCKER_HOST = %q, want tcp://docker:2375", result["DOCKER_HOST"])
		}
		if _, ok := result["SECRET"]; ok {
			t.Error("expected SECRET to be isolated, but it leaked into subprocess env")
		}
	})

	t.Run("does not set docker vars when absent from host env", func(t *testing.T) {
		t.Parallel()
		cfg := ExecutorConfig{
			HostEnv: []string{
				"HOME=/home/runner",
				"PATH=/usr/bin",
			},
		}
		result := envSliceToMap(buildCommandEnv(cfg))

		for _, key := range []string{"DOCKER_HOST", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH"} {
			if v, ok := result[key]; ok {
				t.Errorf("expected %s to be absent when not in host env, got %q", key, v)
			}
		}
	})
}

func TestBaseArgsDockerMode(t *testing.T) {
	t.Parallel()
	args := baseArgs(ExecutorConfig{UseDocker: true})
	for _, a := range args {
		if a == "--shell-executor-no-image" {
			t.Error("docker mode should not include --shell-executor-no-image")
		}
	}
	if len(args) == 0 {
		t.Error("expected non-empty args in docker mode")
	}
}

func TestEnvArgsSkipsGCLConfigKeys(t *testing.T) {
	t.Parallel()

	args := envArgs(map[string]string{
		"CI_JOB_ID":         "1",
		"GCL_UMASK":         "false",
		"CI_PIPELINE_ID":    "2",
		"GCL_SOME_OTHER_OP": "x",
	})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "GCL_UMASK") {
		t.Fatalf("args must not include GCL_* keys, got %q", joined)
	}
	if !strings.Contains(joined, "CI_JOB_ID=1") || !strings.Contains(joined, "CI_PIPELINE_ID=2") {
		t.Fatalf("args must keep CI variables, got %q", joined)
	}
}

func TestUnsetArgs(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		if got := unsetArgs(nil); len(got) != 0 {
			t.Fatalf("expected empty, got %v", got)
		}
	})

	t.Run("single var", func(t *testing.T) {
		t.Parallel()
		got := unsetArgs([]string{"CI_COMMIT_BRANCH"})
		want := []string{"--unset-variable", "CI_COMMIT_BRANCH"}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("multiple vars", func(t *testing.T) {
		t.Parallel()
		got := unsetArgs([]string{"FOO", "BAR"})
		joined := strings.Join(got, " ")
		if joined != "--unset-variable FOO --unset-variable BAR" {
			t.Fatalf("got %q", joined)
		}
	})
}

func TestResolveExecutable(t *testing.T) {
	t.Parallel()

	t.Run("nil_hostenv_found", func(t *testing.T) {
		t.Parallel()
		result := resolveExecutable("sh", nil)
		if result == "" {
			t.Error("expected non-empty result for sh")
		}
	})

	t.Run("nil_hostenv_not_found", func(t *testing.T) {
		t.Parallel()
		result := resolveExecutable("no-such-binary-xyzzy-9999", nil)
		if result != "no-such-binary-xyzzy-9999" {
			t.Errorf("expected fallback name, got %q", result)
		}
	})

	t.Run("custom_path_found", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		bin := filepath.Join(dir, "myexec")
		if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
		result := resolveExecutable("myexec", []string{"PATH=" + dir})
		if result != bin {
			t.Errorf("expected %q, got %q", bin, result)
		}
	})

	t.Run("custom_path_not_found", func(t *testing.T) {
		t.Parallel()
		result := resolveExecutable("no-such-binary-xyzzy-9999", []string{"PATH=/tmp"})
		if result != "no-such-binary-xyzzy-9999" {
			t.Errorf("expected fallback name, got %q", result)
		}
	})

	t.Run("empty_dir_in_path_skipped", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		bin := filepath.Join(dir, "myexec2")
		if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
		result := resolveExecutable("myexec2", []string{"PATH=:" + dir})
		if result != bin {
			t.Errorf("expected %q, got %q", bin, result)
		}
	})
}
