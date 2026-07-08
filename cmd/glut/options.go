package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type RunOptions struct {
	Paths          []string
	Pattern        string
	FailFast       bool
	MaxFail        int
	Verbose        bool
	Quiet          bool
	Format         string
	Reports        []string
	Timeout        time.Duration
	WaitTimeout    time.Duration
	Debug          bool
	KeepWorkspace  bool
	DebugPause     string
	KeepLastFailed int
	CopyStrategy         string
	DockerVolumeStrategy string
	Include              []string
}

type LintOptions struct {
	Paths  []string
	Format string
}

type ListOptions struct {
	Paths   []string
	Pattern string
}

// toRunOptions converts the bound flag values into RunOptions. Slices are
// copied so later flag-struct mutation cannot leak into the returned options.
func (f *runFlags) toRunOptions(args []string) RunOptions {
	paths := args
	if len(paths) == 0 {
		paths = []string{"."}
	}

	return RunOptions{
		Paths:                paths,
		Pattern:              f.pattern,
		FailFast:             f.failFast,
		MaxFail:              f.maxFail,
		Verbose:              f.verbose,
		Quiet:                f.quiet,
		Format:               f.format,
		Reports:              append([]string(nil), f.reports...),
		Timeout:              f.timeout,
		WaitTimeout:          f.waitTimeout,
		Debug:                f.debug,
		KeepWorkspace:        f.keepWorkspace,
		DebugPause:           f.debugPause,
		KeepLastFailed:       f.keepLastFailed,
		CopyStrategy:         f.copyStrategy,
		DockerVolumeStrategy: f.dockerVolumeStrategy,
		Include:              append([]string(nil), f.include...),
	}
}

func listOptionsFromCommand(args []string, pattern string) ListOptions {
	paths := args
	if len(paths) == 0 {
		paths = []string{"."}
	}

	return ListOptions{
		Paths:   paths,
		Pattern: pattern,
	}
}

func lintOptionsFromCommand(args []string, format string) LintOptions {
	paths := args
	if len(paths) == 0 {
		paths = []string{"./tests/"}
	}
	return LintOptions{Paths: paths, Format: format}
}

// checkDefaultTestsDirExists returns a clear, actionable error when lint/doctor
// fell back to the default "./tests/" path (unlike run/list, which default to
// ".") and that directory does not exist. Without this, a missing default
// surfaces only as a raw "lstat ./tests/: no such file or directory" parse
// issue from the file-discovery walk.
func checkDefaultTestsDirExists(paths []string, usedDefault bool) error {
	if !usedDefault || len(paths) == 0 {
		return nil
	}
	if _, err := os.Stat(paths[0]); os.IsNotExist(err) {
		return fmt.Errorf("default test directory %q does not exist; pass a path (e.g. `glut lint ./tests`) or create it", paths[0])
	}
	return nil
}

func envList(env func(string) string, name string) []string {
	value := env(name)
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}
