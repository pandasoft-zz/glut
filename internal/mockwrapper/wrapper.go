package mockwrapper

import (
	"bufio"
	"bytes"
	"encoding/json"
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

func RunWithOptions(opts RunOptions) int {
	opts = opts.withDefaults()
	if len(opts.Args) == 0 {
		writeError(opts.Stderr, "mock wrapper failed: missing argv[0]\n")
		return 127
	}

	name := filepath.Base(opts.Args[0])
	stdinContent, stdin, err := readStdin(opts.Stdin)
	if err != nil {
		writeError(opts.Stderr, "mock wrapper failed to read stdin: %v\n", err)
		return 127
	}

	call := BinaryCall{
		Timestamp: opts.Now().UTC().Format(time.RFC3339Nano),
		PID:       os.Getpid(),
		PPID:      os.Getppid(),
		CWD:       currentDir(),
		Name:      name,
		Args:      append([]string(nil), opts.Args[1:]...),
		Stdin:     stdinContent,
	}

	env := envMap(opts.Environ)
	if logDir := env[config.EnvMockLogDir]; logDir != "" {
		if err := appendBinaryCall(logDir, call); err != nil {
			writeError(opts.Stderr, "mock wrapper log failed: %v\n", err)
		}
	}

	realDir := env[config.EnvMockBinReal]
	if realDir == "" {
		writeError(opts.Stderr, "mock wrapper failed: %s is not set\n", config.EnvMockBinReal)
		return 127
	}

	realPath := realBinaryPath(realDir, name)
	cmd := exec.Command(realPath, opts.Args[1:]...)
	cmd.Stdin = stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	cmd.Env = opts.Environ

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		writeError(opts.Stderr, "mock wrapper failed to run %s: %v\n", realPath, err)
		return 127
	}
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	return 0
}

func ReadBinaryLogs(logDir string) (map[string][]BinaryCall, error) {
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
		path := filepath.Join(logDir, entry.Name())
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("read mock log %s: %w", path, err)
		}

		scanner := bufio.NewScanner(file)
		line := 0
		for scanner.Scan() {
			line++
			var call BinaryCall
			if err := json.Unmarshal(scanner.Bytes(), &call); err != nil {
				if closeErr := file.Close(); closeErr != nil {
					return nil, fmt.Errorf("parse mock log %s line %d: %w; close mock log: %v", path, line, err, closeErr)
				}
				return nil, fmt.Errorf("parse mock log %s line %d: %w", path, line, err)
			}
			calls[name] = append(calls[name], call)
		}
		if err := scanner.Err(); err != nil {
			if closeErr := file.Close(); closeErr != nil {
				return nil, fmt.Errorf("scan mock log %s: %w; close mock log: %v", path, err, closeErr)
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
	if _, err := fmt.Fprintf(stderr, format, args...); err != nil {
		return
	}
}

func readStdin(stdin io.Reader) (string, io.Reader, error) {
	if file, ok := stdin.(*os.File); ok {
		info, err := file.Stat()
		if err != nil {
			return "", stdin, err
		}
		if info.Mode()&os.ModeCharDevice != 0 {
			return "", stdin, nil
		}
	}

	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", stdin, err
	}
	return string(data), bytes.NewReader(data), nil
}

func appendBinaryCall(logDir string, call BinaryCall) (err error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create mock log directory %s: %w", logDir, err)
	}

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
