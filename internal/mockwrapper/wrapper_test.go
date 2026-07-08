package mockwrapper

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pandasoft-zz/glut/internal/config"
)

func TestShouldRunAsMock(t *testing.T) {
	t.Parallel()

	realDir := t.TempDir()
	// A mock real-script for "glut" exists in this dir; the genuine CLI never has
	// one because GLUT_MOCK_BIN_REAL only points at a mock's bin-real directory.
	if err := os.WriteFile(filepath.Join(realDir, "glut"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	mockEnv := []string{config.EnvMockBinReal + "=" + realDir}

	tests := []struct {
		name    string
		args    []string
		environ []string
		want    bool
	}{
		{
			name:    "non-glut name is always a mock",
			args:    []string{"/some/bin/release-cli", "create"},
			environ: nil,
			want:    true,
		},
		{
			name:    "glut name without mock env is the real CLI",
			args:    []string{"/usr/local/bin/glut", "run", "./tests"},
			environ: nil,
			want:    false,
		},
		{
			name:    "glut name with mock env and matching real script is a mock",
			args:    []string{"/work/bin/glut", "run", "--report", "./tests"},
			environ: mockEnv,
			want:    true,
		},
		{
			name:    "glut name with mock env but no matching real script is the real CLI",
			args:    []string{"/usr/local/bin/glut", "run"},
			environ: []string{config.EnvMockBinReal + "=" + t.TempDir()},
			want:    false,
		},
		{
			name:    "empty args is not a mock",
			args:    nil,
			environ: mockEnv,
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldRunAsMock(tc.args, tc.environ); got != tc.want {
				t.Errorf("ShouldRunAsMock(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

// TestNormalizeMockName guards against argv[0] casing/suffix mismatches on
// Windows: without normalization, "Glut.exe" would be misrouted as a mock
// (case-sensitive comparison against "glut"), and a mock invoked as
// "release-cli.exe" would log to "release-cli.exe.jsonl" — a name
// ReadBinaryLogs' "only" filter never matches, so assert.binary sees zero
// calls. goos is a parameter so this is testable without an actual Windows
// host.
func TestNormalizeMockName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		goos string
		name string
		want string
	}{
		{"linux", "release-cli", "release-cli"},
		{"linux", "release-cli.exe", "release-cli.exe"}, // non-Windows: never touched
		{"windows", "release-cli", "release-cli"},
		{"windows", "release-cli.exe", "release-cli"},
		{"windows", "Release-CLI.EXE", "release-cli"},
		{"windows", "Glut.exe", "glut"},
		{"windows", "GLUT", "glut"},
	}
	for _, tc := range tests {
		if got := normalizeMockName(tc.goos, tc.name); got != tc.want {
			t.Errorf("normalizeMockName(%q, %q) = %q, want %q", tc.goos, tc.name, got, tc.want)
		}
	}
}

func TestRunWithOptionsLogsAndPassesThroughStreams(t *testing.T) {
	t.Parallel()
	logDir := t.TempDir()
	realDir := t.TempDir()
	linkHelperBinary(t, filepath.Join(realDir, helperBinaryName("release-cli")))

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
			"GO_WANT_HELPER_PROCESS=1",
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

	logs, err := ReadBinaryLogs(logDir, nil)
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

// TestRunWithOptionsStdinCaptureReflectsWhatMockReads pins the stdin-capture
// semantics documented in docs/reference/assert-syntax.md: the wrapper tees
// stdin as it is streamed to the real binary (never draining before exec, which
// would hang on a never-closing producer). A mock that consumes stdin has it
// captured in full, and — because os/exec buffers the stream into the OS pipe —
// a typical (small) input is captured in full even when the mock never reads
// it. Only a payload larger than the OS pipe buffer that the mock never drains
// could be captured only in part.
func TestRunWithOptionsStdinCaptureReflectsWhatMockReads(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, subcommand string) BinaryCall {
		t.Helper()
		logDir := t.TempDir()
		realDir := t.TempDir()
		linkHelperBinary(t, filepath.Join(realDir, helperBinaryName("tool")))

		var stdout, stderr bytes.Buffer
		code := RunWithOptions(RunOptions{
			Args:   []string{"tool", "-test.run=TestHelperProcess", "--", subcommand},
			Stdin:  strings.NewReader("piped-input"),
			Stdout: &stdout,
			Stderr: &stderr,
			Environ: append(os.Environ(),
				"GO_WANT_HELPER_PROCESS=1",
				config.EnvMockLogDir+"="+logDir,
				config.EnvMockBinReal+"="+realDir,
			),
			Now: fixedNow,
		})
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
		}
		logs, err := ReadBinaryLogs(logDir, nil)
		if err != nil {
			t.Fatalf("read logs: %v", err)
		}
		if len(logs["tool"]) != 1 {
			t.Fatalf("call count = %d, want 1", len(logs["tool"]))
		}
		return logs["tool"][0]
	}

	t.Run("mock that reads stdin captures the full input", func(t *testing.T) {
		if got := run(t, "echo").Stdin; got != "piped-input" {
			t.Fatalf("stdin = %q, want %q", got, "piped-input")
		}
	})

	t.Run("typical input is captured even when the mock ignores stdin", func(t *testing.T) {
		if got := run(t, "ignore-stdin").Stdin; got != "piped-input" {
			t.Fatalf("stdin = %q, want %q (small input is buffered into the pipe and captured)", got, "piped-input")
		}
	})
}

func TestRunWithOptionsPropagatesExitCode(t *testing.T) {
	t.Parallel()
	logDir := t.TempDir()
	realDir := t.TempDir()
	linkHelperBinary(t, filepath.Join(realDir, helperBinaryName("tool")))

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
			"GO_WANT_HELPER_PROCESS=1",
			config.EnvMockLogDir+"="+logDir,
			config.EnvMockBinReal+"="+realDir,
		),
	})

	if code != 7 {
		t.Fatalf("exit code = %d, want 7; stderr: %s", code, stderr.String())
	}
}

func TestAppendBinaryCallKeepsJSONLValidForConcurrentWrites(t *testing.T) {
	t.Parallel()
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

	logs, err := ReadBinaryLogs(logDir, nil)
	if err != nil {
		t.Fatalf("read logs: %v", err)
	}
	if got := len(logs["tool"]); got != count {
		t.Fatalf("call count = %d, want %d", got, count)
	}
}

func TestRunWithOptionsContinuesWhenLogWriteFails(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	logPath := filepath.Join(parent, "not-a-dir")
	if err := os.WriteFile(logPath, []byte("file"), 0644); err != nil {
		t.Fatal(err)
	}
	realDir := t.TempDir()
	linkHelperBinary(t, filepath.Join(realDir, helperBinaryName("tool")))

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
			"GO_WANT_HELPER_PROCESS=1",
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

func TestRunWithOptionsFailureBranches(t *testing.T) {
	t.Run("missing argv0", func(t *testing.T) {
		var stderr bytes.Buffer
		code := RunWithOptions(RunOptions{
			Args:   []string{},
			Stderr: &stderr,
		})
		if code != 127 {
			t.Fatalf("code = %d", code)
		}
		if !strings.Contains(stderr.String(), "missing argv[0]") {
			t.Fatalf("stderr = %q", stderr.String())
		}
	})

	t.Run("broken stdin reader does not block launching the real binary", func(t *testing.T) {
		// teeStdin no longer drains stdin before exec, so a reader that
		// errors (or a producer that never sends EOF) must not stop the
		// real binary from running when it never actually needs stdin —
		// matching how the unmocked binary would behave.
		realDir := t.TempDir()
		linkHelperBinary(t, filepath.Join(realDir, helperBinaryName("tool")))

		code := RunWithOptions(RunOptions{
			Args: []string{
				"tool",
				"-test.run=TestHelperProcess",
				"--",
				"exit",
				"7",
			},
			Stdin: errReader{},
			Environ: append(os.Environ(),
				"GO_WANT_HELPER_PROCESS=1",
				config.EnvMockBinReal+"="+realDir,
			),
		})
		if code != 7 {
			t.Fatalf("code = %d, want 7", code)
		}
	})

	t.Run("missing real dir env", func(t *testing.T) {
		var stderr bytes.Buffer
		code := RunWithOptions(RunOptions{
			Args:    []string{"tool"},
			Stdin:   strings.NewReader(""),
			Stderr:  &stderr,
			Environ: nil,
		})
		if code != 127 {
			t.Fatalf("code = %d", code)
		}
		if !strings.Contains(stderr.String(), config.EnvMockBinReal+" is not set") {
			t.Fatalf("stderr = %q", stderr.String())
		}
	})

	t.Run("real binary execution failure", func(t *testing.T) {
		var stderr bytes.Buffer
		code := RunWithOptions(RunOptions{
			Args:   []string{"tool"},
			Stdin:  strings.NewReader(""),
			Stderr: &stderr,
			Environ: []string{
				config.EnvMockBinReal + "=" + t.TempDir(),
			},
		})
		if code != 127 {
			t.Fatalf("code = %d", code)
		}
		if !strings.Contains(stderr.String(), "failed to run") {
			t.Fatalf("stderr = %q", stderr.String())
		}
	})
}

func TestRunUsesProcessDefaults(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	realDir := t.TempDir()
	linkHelperBinary(t, filepath.Join(realDir, filepath.Base(exe)))

	cmd := exec.Command(exe, "-test.run=TestHelperProcess", "--", "echo")
	cmd.Env = append(os.Environ(),
		"GO_WANT_RUN_FUNCTION=1",
		"GO_WANT_HELPER_PROCESS=1",
		config.EnvMockBinReal+"="+realDir,
	)
	cmd.Stdin = strings.NewReader("payload")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Run subprocess failed: %v, output: %s", err, string(out))
	}
	if !strings.Contains(string(out), "stdout:payload") {
		t.Fatalf("output = %q", string(out))
	}
}

func TestReadBinaryLogsErrors(t *testing.T) {
	t.Run("missing directory", func(t *testing.T) {
		_, err := ReadBinaryLogs(filepath.Join(t.TempDir(), "missing"), nil)
		if err == nil {
			t.Fatal("expected missing directory error")
		}
	})

	t.Run("invalid json line", func(t *testing.T) {
		logDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(logDir, "tool.jsonl"), []byte("{bad json}\n"), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := ReadBinaryLogs(logDir, nil)
		if err == nil || !strings.Contains(err.Error(), "parse mock log") {
			t.Fatalf("ReadBinaryLogs() error = %v", err)
		}
	})

	t.Run("scanner error when a line exceeds the raised buffer limit", func(t *testing.T) {
		logDir := t.TempDir()
		tooLong := `{"name":"tool","stdin":"` + strings.Repeat("a", 65*1024*1024) + `"}` + "\n"
		if err := os.WriteFile(filepath.Join(logDir, "tool.jsonl"), []byte(tooLong), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := ReadBinaryLogs(logDir, nil)
		if err == nil || !strings.Contains(err.Error(), "scan mock log") {
			t.Fatalf("ReadBinaryLogs() error = %v", err)
		}
	})

	t.Run("reads a line past the old 64 KiB default limit", func(t *testing.T) {
		logDir := t.TempDir()
		// appendBinaryCall captures full stdin with no size cap; the default
		// bufio.Scanner limit (64 KiB) used to make this a hard failure.
		bigStdin := strings.Repeat("a", 128*1024)
		line := `{"name":"tool","stdin":"` + bigStdin + `"}` + "\n"
		if err := os.WriteFile(filepath.Join(logDir, "tool.jsonl"), []byte(line), 0644); err != nil {
			t.Fatal(err)
		}
		logs, err := ReadBinaryLogs(logDir, nil)
		if err != nil {
			t.Fatalf("ReadBinaryLogs() error = %v", err)
		}
		if len(logs["tool"]) != 1 || logs["tool"][0].Stdin != bigStdin {
			t.Fatalf("ReadBinaryLogs() did not capture the full oversized stdin, got %d bytes", len(logs["tool"][0].Stdin))
		}
	})

	t.Run("skips non json files", func(t *testing.T) {
		logDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(logDir, "tool.jsonl"), []byte("{\"name\":\"tool\"}\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(logDir, "ignore.txt"), []byte("skip"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(logDir, "subdir"), 0755); err != nil {
			t.Fatal(err)
		}
		logs, err := ReadBinaryLogs(logDir, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(logs["tool"]) != 1 {
			t.Fatalf("ReadBinaryLogs() logs = %#v", logs)
		}
	})

	t.Run("open log error", func(t *testing.T) {
		logDir := t.TempDir()
		if err := os.Symlink(filepath.Join(logDir, "missing"), filepath.Join(logDir, "broken.jsonl")); err != nil {
			if runtime.GOOS == "windows" {
				t.Skip("symlink creation needs extra permissions on windows")
			}
			t.Fatal(err)
		}
		_, err := ReadBinaryLogs(logDir, nil)
		if err == nil || !strings.Contains(err.Error(), "read mock log") {
			t.Fatalf("ReadBinaryLogs() error = %v", err)
		}
	})

	t.Run("skips file not in only filter", func(t *testing.T) {
		logDir := t.TempDir()
		// curl.jsonl is malformed — simulates a truncated/corrupted file
		if err := os.WriteFile(filepath.Join(logDir, "curl.jsonl"), []byte("{bad\n"), 0644); err != nil {
			t.Fatal(err)
		}
		// git.jsonl is valid and is the only asserted binary
		if err := os.WriteFile(filepath.Join(logDir, "git.jsonl"), []byte("{\"name\":\"git\"}\n"), 0644); err != nil {
			t.Fatal(err)
		}
		only := map[string]struct{}{"git": {}}
		logs, err := ReadBinaryLogs(logDir, only)
		if err != nil {
			t.Fatalf("ReadBinaryLogs() with filter should skip curl: %v", err)
		}
		if len(logs["git"]) != 1 {
			t.Fatalf("ReadBinaryLogs() logs[git] = %d, want 1", len(logs["git"]))
		}
		if _, ok := logs["curl"]; ok {
			t.Fatal("ReadBinaryLogs() should not include curl when not in only filter")
		}
	})

	t.Run("empty only filter reads nothing", func(t *testing.T) {
		logDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(logDir, "curl.jsonl"), []byte("{bad\n"), 0644); err != nil {
			t.Fatal(err)
		}
		logs, err := ReadBinaryLogs(logDir, map[string]struct{}{})
		if err != nil {
			t.Fatalf("ReadBinaryLogs() with empty filter: %v", err)
		}
		if len(logs) != 0 {
			t.Fatalf("ReadBinaryLogs() with empty filter returned %d entries, want 0", len(logs))
		}
	})
}

func TestCheckMockLogBarriers(t *testing.T) {
	t.Parallel()

	t.Run("clean directory returns no error", func(t *testing.T) {
		logDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(logDir, "curl.jsonl"), []byte("{}\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := CheckMockLogBarriers(logDir); err != nil {
			t.Fatalf("CheckMockLogBarriers() unexpected error: %v", err)
		}
	})

	t.Run("missing directory is not an error", func(t *testing.T) {
		if err := CheckMockLogBarriers(filepath.Join(t.TempDir(), "missing")); err != nil {
			t.Fatalf("CheckMockLogBarriers() on missing dir: %v", err)
		}
	})

	t.Run("barrier file signals interrupted write", func(t *testing.T) {
		logDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(logDir, ".curl.jsonl.1234"), nil, 0644); err != nil {
			t.Fatal(err)
		}
		err := CheckMockLogBarriers(logDir)
		if err == nil {
			t.Fatal("CheckMockLogBarriers() expected error for barrier file")
		}
		if !strings.Contains(err.Error(), "curl") {
			t.Fatalf("CheckMockLogBarriers() error should mention binary name: %v", err)
		}
	})

	t.Run("multiple barrier files reported together", func(t *testing.T) {
		logDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(logDir, ".curl.jsonl.1234"), nil, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(logDir, ".git.jsonl.5678"), nil, 0644); err != nil {
			t.Fatal(err)
		}
		err := CheckMockLogBarriers(logDir)
		if err == nil {
			t.Fatal("CheckMockLogBarriers() expected error")
		}
		if !strings.Contains(err.Error(), "curl") || !strings.Contains(err.Error(), "git") {
			t.Fatalf("CheckMockLogBarriers() should name both binaries: %v", err)
		}
	})
}

func TestBarrierBinaryName(t *testing.T) {
	cases := []struct {
		input    string
		wantName string
		wantOK   bool
	}{
		{".curl.jsonl.1234", "curl", true},
		{".release-cli.jsonl.99", "release-cli", true},
		{"curl.jsonl.1234", "", false}, // no leading dot
		{".curl.jsonl.", "", false},    // empty pid
		{".curl.jsonl.abc", "", false}, // non-numeric pid
		{".curl.txt.1234", "", false},  // wrong extension
		{".curl.jsonl", "", false},     // missing pid
		{"", "", false},
	}
	for _, tc := range cases {
		gotName, gotOK := barrierBinaryName(tc.input)
		if gotOK != tc.wantOK || gotName != tc.wantName {
			t.Errorf("barrierBinaryName(%q) = (%q, %v), want (%q, %v)",
				tc.input, gotName, gotOK, tc.wantName, tc.wantOK)
		}
	}
}

func TestAppendBinaryCallRemovesBarrierOnSuccess(t *testing.T) {
	t.Parallel()
	logDir := t.TempDir()
	if err := appendBinaryCall(logDir, BinaryCall{Name: "curl", PID: 1}); err != nil {
		t.Fatalf("appendBinaryCall: %v", err)
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if _, ok := barrierBinaryName(e.Name()); ok {
			t.Errorf("barrier file %s was not removed after successful write", e.Name())
		}
	}
}

func TestRunOptionsWithDefaults(t *testing.T) {
	opts := RunOptions{}.withDefaults()
	if opts.Args == nil || opts.Stdin == nil || opts.Stdout == nil || opts.Stderr == nil || opts.Environ == nil || opts.Now == nil {
		t.Fatalf("withDefaults() did not fill all fields: %+v", opts)
	}
}

func TestTeeStdinHelpers(t *testing.T) {
	t.Run("reader content is captured as it is read", func(t *testing.T) {
		reader, capture := teeStdin(strings.NewReader("hello"))
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "hello" {
			t.Fatalf("reader data = %q", string(data))
		}
		if capture.buf.String() != "hello" {
			t.Fatalf("captured = %q", capture.buf.String())
		}
	})

	t.Run("char device file is passed through unwrapped", func(t *testing.T) {
		file, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if closeErr := file.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
		}()

		info, err := file.Stat()
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeCharDevice == 0 {
			t.Skip("os.DevNull is not a char device on this platform")
		}

		reader, capture := teeStdin(file)
		if reader != file || capture.buf.Len() != 0 {
			t.Fatalf("char device handling: reader == file: %v, captured = %q", reader == file, capture.buf.String())
		}
	})

	t.Run("does not drain a pipe before the reader is consumed", func(t *testing.T) {
		pr, pw := io.Pipe()
		reader, _ := teeStdin(pr)

		done := make(chan string, 1)
		go func() {
			buf := make([]byte, 5)
			n, _ := io.ReadFull(reader, buf)
			done <- string(buf[:n])
		}()

		// A producer that keeps the pipe open (never closes pw) must not
		// block teeStdin: the real binary drives consumption directly, so
		// the first chunk is observed without waiting for EOF.
		if _, err := pw.Write([]byte("hello")); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-done:
			if got != "hello" {
				t.Fatalf("read = %q, want %q", got, "hello")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("teeStdin blocked reading a still-open pipe instead of letting the consumer drive it")
		}
		_ = pw.Close()
	})

	t.Run("capture is capped without truncating what the real binary sees", func(t *testing.T) {
		limit := 8
		capture := &cappedWriter{limit: limit}
		full := "0123456789"
		reader := io.TeeReader(strings.NewReader(full), capture)

		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != full {
			t.Fatalf("reader must see the full stream, got %q", string(data))
		}
		if capture.buf.String() != full[:limit] {
			t.Fatalf("captured = %q, want first %d bytes", capture.buf.String(), limit)
		}
	})
}

func TestHelperUtilities(t *testing.T) {
	env := envMap([]string{"A=1", "BROKEN", "B=2=3"})
	if env["A"] != "1" || env["B"] != "2=3" {
		t.Fatalf("envMap() = %#v", env)
	}
	if _, ok := env["BROKEN"]; ok {
		t.Fatalf("envMap should skip invalid entry: %#v", env)
	}

	if currentDir() == "" {
		t.Fatal("currentDir() should not be empty")
	}

	realDir := t.TempDir()
	got := realBinaryPath(realDir, "tool")
	want := filepath.Join(realDir, "tool")
	if got != want {
		t.Fatalf("realBinaryPath() = %q, want %q", got, want)
	}
}

// TestWriteErrorIgnoresWriterFailure verifies writeError attempts the
// formatted write (rather than silently no-oping) and does not panic or
// propagate the error when the writer itself fails — it is a best-effort
// diagnostic with nowhere else to report a failure.
func TestWriteErrorIgnoresWriterFailure(t *testing.T) {
	w := &errWriter{}
	writeError(w, "message: %s", "boom")
	if string(w.written) != "message: boom" {
		t.Fatalf("writeError did not attempt the formatted write: got %q", w.written)
	}
}

func TestAppendBinaryCallMkdirError(t *testing.T) {
	parent := t.TempDir()
	filePath := filepath.Join(parent, "not-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	err := appendBinaryCall(filePath, BinaryCall{Name: "tool"})
	if err == nil || !strings.Contains(err.Error(), "create mock log directory") {
		t.Fatalf("appendBinaryCall() error = %v", err)
	}
}

func TestAppendBinaryCallOpenError(t *testing.T) {
	err := appendBinaryCall(t.TempDir(), BinaryCall{Name: filepath.Join("missing", "tool")})
	if err == nil || !strings.Contains(err.Error(), "open mock log") {
		t.Fatalf("appendBinaryCall() error = %v", err)
	}
}

func TestCurrentDirReturnsEmptyWhenCWDIsGone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cwd removal behavior is not stable on windows")
	}

	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	temp := t.TempDir()
	if err := os.Chdir(temp); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(temp); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chdir(original)
	}()

	if got := currentDir(); got != "" {
		t.Fatalf("currentDir() = %q, want empty string", got)
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_RUN_FUNCTION") == "1" {
		if err := os.Unsetenv("GO_WANT_RUN_FUNCTION"); err != nil {
			t.Fatal(err)
		}
		Run()
		return
	}

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
	case "ignore-stdin":
		// Exit 0 without reading stdin at all, to model a mock stub that does
		// not consume its input.
		os.Exit(0)
	default:
		os.Exit(2)
	}
}

type errReader struct{}

func (errReader) Read(_ []byte) (int, error) {
	return 0, errors.New("boom")
}

type errWriter struct {
	written []byte
}

func (w *errWriter) Write(p []byte) (int, error) {
	w.written = append(w.written, p...)
	return 0, errors.New("boom")
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
