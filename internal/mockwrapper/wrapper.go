package mockwrapper

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pandasoft-zz/glut/internal/config"
)

type BinaryCall struct {
	Timestamp string   `json:"ts"`
	PID       int      `json:"pid"`
	PPID      int      `json:"ppid"`
	CWD       string   `json:"cwd"`
	Name      string   `json:"name"`
	Args      []string `json:"args"`
	Stdin     string   `json:"stdin"`
}

type RunOptions struct {
	Args    []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Environ []string
	Now     func() time.Time
}

func Run() {
	os.Exit(RunWithOptions(RunOptions{
		Args:    os.Args,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Environ: os.Environ(),
		Now:     time.Now,
	}))
}

// ShouldRunAsMock reports whether this process was invoked as a mocked binary
// rather than the real glut CLI.
//
// A non-"glut" argv[0] is always a mock: SetupMockBinaries symlinks each mock
// under its own name, so the binary can only be reached under that name when it
// is standing in for a mocked tool.
//
// When argv[0] IS "glut" — i.e. a test mocks the `glut` binary itself — the
// basename alone cannot distinguish the mock from the genuine CLI, so we
// additionally require that the mock environment is active (GLUT_MOCK_BIN_REAL
// set) and that a real script for "glut" exists in it. The real glut CLI never
// has GLUT_MOCK_BIN_REAL in its own process environment (it only sets it for the
// job it spawns), so the genuine CLI is never misrouted into the mock wrapper.
func ShouldRunAsMock(args, environ []string) bool {
	if len(args) == 0 {
		return false
	}
	base := normalizeMockName(runtime.GOOS, filepath.Base(args[0]))
	if base != "glut" {
		return true
	}
	realDir := envMap(environ)[config.EnvMockBinReal]
	if realDir == "" {
		return false
	}
	_, err := os.Stat(realBinaryPath(realDir, base))
	return err == nil
}

func RunWithOptions(opts RunOptions) int {
	opts = opts.withDefaults()
	if len(opts.Args) == 0 {
		writeError(opts.Stderr, "mock wrapper failed: missing argv[0]\n")
		return 127
	}

	name := normalizeMockName(runtime.GOOS, filepath.Base(opts.Args[0]))
	env := envMap(opts.Environ)

	realDir := env[config.EnvMockBinReal]
	if realDir == "" {
		writeError(opts.Stderr, "mock wrapper failed: %s is not set\n", config.EnvMockBinReal)
		return 127
	}

	stdin, capture := teeStdin(opts.Stdin)

	realPath := realBinaryPath(realDir, name)
	cmd := exec.Command(realPath, opts.Args[1:]...)
	cmd.Stdin = stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	cmd.Env = opts.Environ
	// stdin is a non-*os.File reader, so os/exec runs a copy goroutine and
	// Wait blocks until it returns. If the real binary exits without draining
	// stdin while the producer keeps the pipe open, that goroutine would hang
	// forever; WaitDelay bounds the wait and closes the pipe to unblock it.
	cmd.WaitDelay = time.Second

	runErr := cmd.Run()

	call := BinaryCall{
		Timestamp: opts.Now().UTC().Format(time.RFC3339Nano),
		PID:       os.Getpid(),
		PPID:      os.Getppid(),
		CWD:       currentDir(),
		Name:      name,
		Args:      append([]string(nil), opts.Args[1:]...),
		Stdin:     capture.buf.String(),
	}
	if logDir := env[config.EnvMockLogDir]; logDir != "" {
		if err := appendBinaryCall(logDir, call); err != nil {
			writeError(opts.Stderr, "mock wrapper log failed: %v\n", err)
		}
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return exitErr.ExitCode()
		}
		writeError(opts.Stderr, "mock wrapper failed to run %s: %v\n", realPath, runErr)
		return 127
	}
	return cmd.ProcessState.ExitCode()
}

// CheckMockLogBarriers looks for barrier files left behind by mock wrapper
// processes that were killed before completing their log write. Each call to
// appendBinaryCall creates a file named .{binary}.jsonl.{pid} at the start of
// the write and removes it on normal exit. A remaining barrier file means the
// write was interrupted and the corresponding log may be incomplete.
func CheckMockLogBarriers(logDir string) error {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read mock log directory %s: %w", logDir, err)
	}
	var interrupted []string
	for _, entry := range entries {
		if name, ok := barrierBinaryName(entry.Name()); ok {
			interrupted = append(interrupted, name)
		}
	}
	if len(interrupted) > 0 {
		return fmt.Errorf("mock binary log write was interrupted for: %s", strings.Join(interrupted, ", "))
	}
	return nil
}

// barrierBinaryName reports whether name matches the barrier file pattern
// .{binary}.jsonl.{pid} and returns the binary name if so.
func barrierBinaryName(name string) (string, bool) {
	if !strings.HasPrefix(name, ".") {
		return "", false
	}
	inner := name[1:] // strip leading dot → "{binary}.jsonl.{pid}"
	lastDot := strings.LastIndex(inner, ".")
	if lastDot < 0 {
		return "", false
	}
	pidPart := inner[lastDot+1:]
	if len(pidPart) == 0 {
		return "", false
	}
	for _, c := range pidPart {
		if c < '0' || c > '9' {
			return "", false
		}
	}
	rest := inner[:lastDot] // "{binary}.jsonl"
	if !strings.HasSuffix(rest, ".jsonl") {
		return "", false
	}
	return strings.TrimSuffix(rest, ".jsonl"), true
}

// ReadBinaryLogs reads binary call logs from logDir.
// only restricts which binary names are read; if nil, all log files are read.
// Pass the set of binary names that have assertions so log files for binaries
// without assertions are not parsed — a partially-written log for an
// un-asserted binary should not fail a test.
func ReadBinaryLogs(logDir string, only map[string]struct{}) (map[string][]BinaryCall, error) {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return nil, fmt.Errorf("read mock log directory %s: %w", logDir, err)
	}

	calls := make(map[string][]BinaryCall)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".jsonl")
		if only != nil {
			if _, ok := only[name]; !ok {
				continue
			}
		}
		path := filepath.Join(logDir, entry.Name())
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("read mock log %s: %w", path, err)
		}

		scanner := bufio.NewScanner(file)
		// appendBinaryCall writes JSONL lines containing captured stdin (capped
		// at maxCapturedStdinBytes); the default 64 KiB scanner limit would
		// still make ReadBinaryLogs fail on any mocked binary that received
		// more than 64 KiB on stdin, so the read buffer matches the cap with
		// headroom for JSON escaping.
		scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			var call BinaryCall
			if err := json.Unmarshal(scanner.Bytes(), &call); err != nil {
				if closeErr := file.Close(); closeErr != nil {
					return nil, fmt.Errorf("parse mock log %s line %d: %w; close mock log: %w", path, line, err, closeErr)
				}
				return nil, fmt.Errorf("parse mock log %s line %d: %w", path, line, err)
			}
			calls[name] = append(calls[name], call)
		}
		if err := scanner.Err(); err != nil {
			if closeErr := file.Close(); closeErr != nil {
				return nil, fmt.Errorf("scan mock log %s: %w; close mock log: %w", path, err, closeErr)
			}
			return nil, fmt.Errorf("scan mock log %s: %w", path, err)
		}
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("close mock log %s: %w", path, err)
		}
	}

	return calls, nil
}

func (opts RunOptions) withDefaults() RunOptions {
	if opts.Args == nil {
		opts.Args = os.Args
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Environ == nil {
		opts.Environ = os.Environ()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return opts
}

func writeError(stderr io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(stderr, format, args...) // best-effort: nothing useful to do if stderr write fails
}

// maxCapturedStdinBytes bounds how much of a mocked binary's stdin the
// wrapper retains for logging/assertions. The real binary still sees the
// full, unbounded stream via io.TeeReader — only the captured copy is capped.
const maxCapturedStdinBytes = 10 << 20 // 10 MiB

// cappedWriter accumulates up to limit bytes and silently discards the rest,
// always reporting a full write so it never breaks the TeeReader it backs.
type cappedWriter struct {
	buf   bytes.Buffer
	limit int
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if remaining := c.limit - c.buf.Len(); remaining > 0 {
		if len(p) < remaining {
			remaining = len(p)
		}
		c.buf.Write(p[:remaining])
	}
	return len(p), nil
}

// teeStdin lets the real binary read stdin directly — driving consumption at
// its own pace, which matters for a producer that keeps the pipe open (e.g.
// `tail -f x | tool`) — while capturing a capped copy for logging. Draining
// stdin fully before exec (the previous approach) would hang on such a
// producer even though the unmocked binary would run fine. An interactive
// terminal is passed through unwrapped so it is never read here.
func teeStdin(stdin io.Reader) (io.Reader, *cappedWriter) {
	capture := &cappedWriter{limit: maxCapturedStdinBytes}
	if file, ok := stdin.(*os.File); ok {
		if info, err := file.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			return stdin, capture
		}
	}
	return io.TeeReader(stdin, capture), capture
}

func appendBinaryCall(logDir string, call BinaryCall) (err error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create mock log directory %s: %w", logDir, err)
	}

	// Create a barrier file before writing. It signals that a write is in
	// progress. On normal exit (success or error) the defer below removes it.
	// If this process is killed before the defer runs, the file remains and
	// CheckMockLogBarriers will detect the interrupted write.
	barrierPath := filepath.Join(logDir, fmt.Sprintf(".%s.jsonl.%d", call.Name, os.Getpid()))
	_ = os.WriteFile(barrierPath, nil, 0644) // best-effort; don't fail if it can't be created
	defer func() { _ = os.Remove(barrierPath) }()

	path := filepath.Join(logDir, call.Name+".jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open mock log %s: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close mock log %s: %w", path, closeErr)
		}
	}()

	// lockFile always errors on Windows (unimplemented — see flock_windows.go);
	// this falls back to relying on NTFS's own append-write atomicity for
	// small writes instead of failing every mock invocation on that platform.
	locked := false
	if err := lockFile(file); err == nil {
		locked = true
		defer func() {
			if unlockErr := unlockFile(file); err == nil && unlockErr != nil {
				err = fmt.Errorf("unlock mock log %s: %w", path, unlockErr)
			}
		}()
	}

	data, err := json.Marshal(call)
	if err != nil {
		return fmt.Errorf("encode mock log record: %w", err)
	}
	data = append(data, '\n')

	if written, err := file.Write(data); err != nil {
		if locked {
			return fmt.Errorf("write locked mock log %s: %w", path, err)
		}
		return fmt.Errorf("write mock log %s without lock: %w", path, err)
	} else if written != len(data) {
		return fmt.Errorf("write mock log %s: short write", path)
	}
	return nil
}

func envMap(environ []string) map[string]string {
	env := make(map[string]string, len(environ))
	for _, item := range environ {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		env[key] = value
	}
	return env
}

func currentDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

func realBinaryPath(realDir string, name string) string {
	path := filepath.Join(realDir, name)
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if _, err := os.Stat(path + ".exe"); err == nil {
				return path + ".exe"
			}
		}
	}
	return path
}

// normalizeMockName normalizes an argv[0] basename for cross-platform mock
// name comparisons and logging. Windows filesystems are case-insensitive and
// executables commonly carry a ".exe" suffix that a validated mock binary
// name (e.g. "release-cli") never has, so without normalization "Release-cli.exe"
// would be misrouted (case mismatch against "glut") and, once routed, would
// log to "release-cli.exe.jsonl" instead of "release-cli.jsonl" — a name the
// "only" filter in ReadBinaryLogs never matches, so assert.binary sees zero
// calls. goos is a parameter (rather than reading runtime.GOOS directly) so
// this logic can be exercised in tests regardless of the host OS.
func normalizeMockName(goos, name string) string {
	if goos != "windows" {
		return name
	}
	name = strings.ToLower(name)
	return strings.TrimSuffix(name, ".exe")
}
