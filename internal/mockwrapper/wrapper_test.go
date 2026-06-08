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

	t.Run("stdin read failure", func(t *testing.T) {
		var stderr bytes.Buffer
		code := RunWithOptions(RunOptions{
			Args:    []string{"tool"},
			Stdin:   errReader{},
			Stderr:  &stderr,
			Environ: []string{config.EnvMockBinReal + "=" + t.TempDir()},
		})
		if code != 127 {
			t.Fatalf("code = %d", code)
		}
		if !strings.Contains(stderr.String(), "failed to read stdin") {
			t.Fatalf("stderr = %q", stderr.String())
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

	t.Run("scanner error on long line", func(t *testing.T) {
		logDir := t.TempDir()
		longLine := strings.Repeat("a", 70*1024)
		if err := os.WriteFile(filepath.Join(logDir, "tool.jsonl"), []byte(longLine), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := ReadBinaryLogs(logDir, nil)
		if err == nil || !strings.Contains(err.Error(), "scan mock log") {
			t.Fatalf("ReadBinaryLogs() error = %v", err)
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
		{"curl.jsonl.1234", "", false},   // no leading dot
		{".curl.jsonl.", "", false},      // empty pid
		{".curl.jsonl.abc", "", false},   // non-numeric pid
		{".curl.txt.1234", "", false},    // wrong extension
		{".curl.jsonl", "", false},       // missing pid
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

func TestReadStdinHelpers(t *testing.T) {
	t.Run("reader content", func(t *testing.T) {
		content, reader, err := readStdin(strings.NewReader("hello"))
		if err != nil {
			t.Fatal(err)
		}
		if content != "hello" {
			t.Fatalf("content = %q", content)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "hello" {
			t.Fatalf("reader data = %q", string(data))
		}
	})

	t.Run("char device file", func(t *testing.T) {
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

		content, reader, err := readStdin(file)
		if err != nil {
			t.Fatal(err)
		}
		if content != "" || reader != file {
			t.Fatalf("char device handling = %q %#v", content, reader)
		}
	})

	t.Run("closed file stat error", func(t *testing.T) {
		file, err := os.CreateTemp(t.TempDir(), "stdin")
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		_, _, err = readStdin(file)
		if err == nil {
			t.Fatal("expected readStdin to fail on closed file")
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

func TestWriteErrorIgnoresWriterFailure(t *testing.T) {
	writeError(errWriter{}, "message: %s", "boom")
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
	default:
		os.Exit(2)
	}
}

type errReader struct{}

func (errReader) Read(_ []byte) (int, error) {
	return 0, errors.New("boom")
}

type errWriter struct{}

func (errWriter) Write(_ []byte) (int, error) {
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
