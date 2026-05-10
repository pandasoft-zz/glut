package main

import (
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
	Debug          bool
	KeepWorkspace  bool
	DebugPause     string
	KeepLastFailed int
}

type LintOptions struct {
	Paths  []string
	Format string
}

type ListOptions struct {
	Paths   []string
	Pattern string
}

func runOptionsFromCommand(args []string) RunOptions {
	paths := args
	if len(paths) == 0 {
		paths = []string{"."}
	}

	return RunOptions{
		Paths:          paths,
		Pattern:        runPattern,
		FailFast:       runFailFast,
		MaxFail:        runMaxFail,
		Verbose:        runVerbose,
		Quiet:          runQuiet,
		Format:         runFormat,
		Reports:        append([]string(nil), runReports...),
		Timeout:        runTimeout,
		Debug:          runDebug,
		KeepWorkspace:  runKeepWorkspace,
		DebugPause:     runDebugPause,
		KeepLastFailed: runKeepLastFailed,
	}
}

func listOptionsFromCommand(args []string) ListOptions {
	paths := args
	if len(paths) == 0 {
		paths = []string{"."}
	}

	return ListOptions{
		Paths:   paths,
		Pattern: listPattern,
	}
}

func lintOptionsFromCommand(args []string) LintOptions {
	paths := args
	if len(paths) == 0 {
		paths = []string{"./tests/"}
	}
	return LintOptions{Paths: paths, Format: lintFormat}
}

func envList(name string) []string {
	value := os.Getenv(name)
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
