package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestShellMockWrapperCapturesStdin guards against the Docker shell mock
// wrapper hardcoding "stdin":"" — unlike the native Go wrapper, which
// captures real stdin, the same stdin assertion used to behave differently
// between docker:false and docker:true.
func TestShellMockWrapperCapturesStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shellMockWrapper targets POSIX sh")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(realDir, 0755); err != nil {
		t.Fatal(err)
	}

	realScript := "#!/bin/sh\ncat >/dev/null\necho real-ran\n"
	if err := os.WriteFile(filepath.Join(realDir, "mytool"), []byte(realScript), 0755); err != nil {
		t.Fatal(err)
	}

	wrapperPath := filepath.Join(dir, "mytool")
	if err := os.WriteFile(wrapperPath, []byte(shellMockWrapper), 0755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", wrapperPath, "--flag")
	cmd.Env = append(os.Environ(),
		"GLUT_MOCK_LOG_DIR="+logDir,
		"GLUT_MOCK_BIN_REAL="+realDir,
	)
	cmd.Stdin = strings.NewReader("hello stdin")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("wrapper run failed: %v; output: %s", err, out)
	}
	if !strings.Contains(string(out), "real-ran") {
		t.Fatalf("expected the real binary to run, got %q", out)
	}

	logData, err := os.ReadFile(filepath.Join(logDir, "mytool.jsonl"))
	if err != nil {
		t.Fatalf("read wrapper log: %v", err)
	}
	var call struct {
		Name  string `json:"name"`
		Stdin string `json:"stdin"`
	}
	if err := json.Unmarshal(logData, &call); err != nil {
		t.Fatalf("unmarshal wrapper log %q: %v", logData, err)
	}
	if call.Name != "mytool" {
		t.Fatalf("name = %q, want mytool", call.Name)
	}
	if call.Stdin != "hello stdin" {
		t.Fatalf("stdin = %q, want %q", call.Stdin, "hello stdin")
	}

	if entries, err := os.ReadDir(logDir); err == nil {
		for _, e := range entries {
			if strings.Contains(e.Name(), ".stdin.") {
				t.Fatalf("leftover stdin temp file not cleaned up: %s", e.Name())
			}
		}
	}
}

// TestShellMockWrapperHandlesEmptyStdin guards against the JSON escaping
// path breaking when there is nothing on stdin.
func TestShellMockWrapperHandlesEmptyStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shellMockWrapper targets POSIX sh")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "mytool"), []byte("#!/bin/sh\ncat >/dev/null\n"), 0755); err != nil {
		t.Fatal(err)
	}

	wrapperPath := filepath.Join(dir, "mytool")
	if err := os.WriteFile(wrapperPath, []byte(shellMockWrapper), 0755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", wrapperPath)
	cmd.Env = append(os.Environ(),
		"GLUT_MOCK_LOG_DIR="+logDir,
		"GLUT_MOCK_BIN_REAL="+realDir,
	)
	cmd.Stdin = strings.NewReader("")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("wrapper run failed: %v; output: %s", err, out)
	}

	logData, err := os.ReadFile(filepath.Join(logDir, "mytool.jsonl"))
	if err != nil {
		t.Fatalf("read wrapper log: %v", err)
	}
	var call struct {
		Stdin string `json:"stdin"`
	}
	if err := json.Unmarshal(logData, &call); err != nil {
		t.Fatalf("unmarshal wrapper log %q: %v", logData, err)
	}
	if call.Stdin != "" {
		t.Fatalf("stdin = %q, want empty", call.Stdin)
	}
}

// TestShellMockWrapperEscapesBinaryNameInLogLine guards against the wrapper
// interpolating $name raw into the "name" JSON field while cwd/stdin go
// through json_str — validateMockBinaryName allows quotes, so a name like
// `foo"bar` used to produce a malformed, unparsable JSONL line.
func TestShellMockWrapperEscapesBinaryNameInLogLine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shellMockWrapper targets POSIX sh")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	const quotedName = `foo"bar`

	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, quotedName), []byte("#!/bin/sh\ncat >/dev/null\n"), 0755); err != nil {
		t.Fatal(err)
	}

	wrapperPath := filepath.Join(dir, quotedName)
	if err := os.WriteFile(wrapperPath, []byte(shellMockWrapper), 0755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", wrapperPath)
	cmd.Env = append(os.Environ(),
		"GLUT_MOCK_LOG_DIR="+logDir,
		"GLUT_MOCK_BIN_REAL="+realDir,
	)
	cmd.Stdin = strings.NewReader("")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("wrapper run failed: %v; output: %s", err, out)
	}

	logData, err := os.ReadFile(filepath.Join(logDir, quotedName+".jsonl"))
	if err != nil {
		t.Fatalf("read wrapper log: %v", err)
	}
	var call struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(logData, &call); err != nil {
		t.Fatalf("unmarshal wrapper log %q: %v", logData, err)
	}
	if call.Name != quotedName {
		t.Fatalf("name = %q, want %q", call.Name, quotedName)
	}
}

// TestReadLogsFromDockerVolumeFailsLoudOnMissingLogDir guards against a
// non-zero tar exit with empty stderr being silently treated as "no mock
// calls yet" and returning nil. CreateDockerVolume always populates an empty
// mock-logs directory, so tar only fails there for a real reason (container
// OOM-killed, daemon race, the volume never being populated) — this must
// surface as an *InfraError so the runner's retry logic can apply, not be
// swallowed as a false-negative "zero binary calls".
func TestReadLogsFromDockerVolumeFailsLoudOnMissingLogDir(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	volName := fmt.Sprintf("glut-test-readlogs-%d", os.Getpid())
	if out, err := exec.Command("docker", "volume", "create", volName).CombinedOutput(); err != nil {
		t.Skipf("docker volume create failed (no daemon?): %v (%s)", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("docker", "volume", "rm", volName).Run()
	})

	// Deliberately do not populate the volume, so MockBinaryLogDir(workDir)
	// does not exist inside it and tar fails for real (not the "empty
	// directory" case, which CreateDockerVolume always avoids by creating
	// the directory up front even when empty). workDir must be a real local
	// path so the local log-dir MkdirAll succeeds; only the container-side
	// mock-logs subdirectory is missing.
	workDir := t.TempDir()

	err := ReadLogsFromDockerVolume(volName, workDir)
	if err == nil {
		t.Fatal("expected an error when the mock-logs directory does not exist in the volume")
	}
	var infraErr *InfraError
	if !errors.As(err, &infraErr) {
		t.Fatalf("ReadLogsFromDockerVolume() error = %v (%T), want an *InfraError so the runner retries", err, err)
	}
}
