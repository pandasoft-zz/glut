package mockwrapper

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pandasoft-zz/glut/internal/config"
)

func TestRunWithOptionsLogsAndPassesThroughStreams(t *testing.T) {
	logDir := t.TempDir()
	realDir := t.TempDir()
	linkHelperBinary(t, filepath.Join(realDir, helperBinaryName("release-cli")))
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunWithOptions(RunOptions{
		Args: []string{
			"release-cli",
			"-test.run=TestHelperProcess",
			"--",
			"echo",
			"--dry-run",
		},
		Stdin:  strings.NewReader("payload"),
		Stdout: &stdout,
		Stderr: &stderr,
		Environ: append(os.Environ(),
			config.EnvMockLogDir+"="+logDir,
			config.EnvMockBinReal+"="+realDir,
		),
		Now: fixedNow,
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
	}
	if stdout.String() != "stdout:payload" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != "stderr:payload" {
		t.Fatalf("stderr = %q", stderr.String())
	}

	logs, err := ReadBinaryLogs(logDir)
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}
	calls := logs["release-cli"]
	if len(calls) != 1 {
		t.Fatalf("call count = %d, want 1", len(calls))
	}
	if calls[0].Timestamp != fixedNow().UTC().Format(time.RFC3339Nano) {
		t.Fatalf("timestamp = %q", calls[0].Timestamp)
	}
	if calls[0].Name != "release-cli" {
		t.Fatalf("name = %q", calls[0].Name)
	}
	if calls[0].Stdin != "payload" {
		t.Fatalf("stdin = %q", calls[0].Stdin)
	}
	if !contains(calls[0].Args, "--dry-run") {
		t.Fatalf("args do not contain user arg: %v", calls[0].Args)
	}
}

func TestRunWithOptionsPropagatesExitCode(t *testing.T) {
	logDir := t.TempDir()
	realDir := t.TempDir()
	linkHelperBinary(t, filepath.Join(realDir, helperBinaryName("tool")))
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	var stderr bytes.Buffer
	code := RunWithOptions(RunOptions{
		Args: []string{
			"tool",
			"-test.run=TestHelperProcess",
			"--",
			"exit",
			"7",
		},
		Stdin:  strings.NewReader(""),
		Stderr: &stderr,
		Environ: append(os.Environ(),
			config.EnvMockLogDir+"="+logDir,
			config.EnvMockBinReal+"="+realDir,
		),
	})

	if code != 7 {
		t.Fatalf("exit code = %d, want 7; stderr: %s", code, stderr.String())
	}
}

func TestAppendBinaryCallKeepsJSONLValidForConcurrentWrites(t *testing.T) {
	logDir := t.TempDir()
	const count = 40

	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- appendBinaryCall(logDir, BinaryCall{
				Timestamp: fixedNow().Format(time.RFC3339Nano),
				PID:       i,
				Name:      "tool",
				Args:      []string{fmt.Sprintf("%d", i)},
			})
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("append call: %v", err)
		}
	}

	logs, err := ReadBinaryLogs(logDir)
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}
	if got := len(logs["tool"]); got != count {
		t.Fatalf("call count = %d, want %d", got, count)
	}
}

func TestRunWithOptionsContinuesWhenLogWriteFails(t *testing.T) {
	parent := t.TempDir()
	logPath := filepath.Join(parent, "not-a-dir")
	if err := os.WriteFile(logPath, []byte("file"), 0644); err != nil {
		t.Fatal(err)
	}
	realDir := t.TempDir()
	linkHelperBinary(t, filepath.Join(realDir, helperBinaryName("tool")))
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunWithOptions(RunOptions{
		Args: []string{
			"tool",
			"-test.run=TestHelperProcess",
			"--",
			"echo",
		},
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
		Environ: append(os.Environ(),
			config.EnvMockLogDir+"="+logPath,
			config.EnvMockBinReal+"="+realDir,
		),
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stdout.String() != "stdout:" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "mock wrapper log failed") {
		t.Fatalf("stderr does not include log failure: %q", stderr.String())
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		os.Exit(2)
	}
	args = args[1:]

	switch args[0] {
	case "echo":
		data, _ := io.ReadAll(os.Stdin)
		if _, err := fmt.Fprintf(os.Stdout, "stdout:%s", string(data)); err != nil {
			os.Exit(2)
		}
		if _, err := fmt.Fprintf(os.Stderr, "stderr:%s", string(data)); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	case "exit":
		if args[1] == "7" {
			os.Exit(7)
		}
		os.Exit(1)
	default:
		os.Exit(2)
	}
}

func linkHelperBinary(t *testing.T, path string) {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(exe, path); err == nil {
		return
	} else if runtime.GOOS != "windows" {
		t.Fatal(err)
	}

	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0755); err != nil {
		t.Fatal(err)
	}
}

func helperBinaryName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func fixedNow() time.Time {
	return time.Date(2026, 4, 25, 10, 0, 1, 123000000, time.UTC)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
