package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/charmbracelet/huh"
	"github.com/mattn/go-isatty"
	"github.com/pandasoft-zz/glut/internal/runner"
)

func selectAndRun(ctx context.Context, opts RunOptions) (runner.RunResult, runner.ExitCode) {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		fmt.Fprintln(os.Stderr, "error: --interactive requires an interactive terminal")
		return runner.RunResult{}, runner.ExitRunnerError
	}

	all, err := runner.List(ctx, opts.Paths, runner.ListOptions{RunPattern: opts.Pattern})
	if err != nil {
		return runner.RunResult{Error: err}, runner.ExitRunnerError
	}
	if len(all) == 0 {
		fmt.Fprintln(os.Stderr, "no tests found")
		return runner.RunResult{}, runner.ExitOK
	}

	selected, ok := pickTests(all)
	if !ok {
		fmt.Fprintln(os.Stderr, "cancelled")
		return runner.RunResult{}, runner.ExitOK
	}
	if len(selected) == 0 {
		fmt.Fprintln(os.Stderr, "no tests selected")
		return runner.RunResult{}, runner.ExitOK
	}

	sinks, fileReports, err := buildProgressSinks(opts, os.Stdout)
	if err != nil {
		return runner.RunResult{Error: err}, runner.ExitRunnerError
	}
	result, code := runner.Run(ctx, selected, runner.RunOptions{
		RunPattern:     opts.Pattern,
		FailFast:       opts.FailFast,
		MaxFail:        opts.MaxFail,
		Verbose:        opts.Verbose,
		Quiet:          opts.Quiet,
		Timeout:        opts.Timeout,
		Debug:          opts.Debug,
		KeepWorkspace:  opts.KeepWorkspace,
		DebugPause:     opts.DebugPause,
		KeepLastFailed: opts.KeepLastFailed,
		CopyStrategy:   opts.CopyStrategy,
		Include:        opts.Include,
		Progress:       sinks,
	})
	if werr := writeFileReports(fileReports); werr != nil {
		return result, runner.ExitRunnerError
	}
	return result, code
}

// pickTests shows directory + test selection prompts and returns the chosen file paths.
// Returns ok=false if the user cancelled.
func pickTests(all []runner.ListedTest) ([]string, bool) {
	dirs := groupByDir(all)

	candidates := all
	if len(dirs) > 1 {
		chosenDir, ok := pickDirectory(dirs)
		if !ok {
			return nil, false
		}
		if chosenDir != "" {
			filtered := make([]runner.ListedTest, 0)
			for _, t := range all {
				if filepath.Dir(t.FilePath) == chosenDir {
					filtered = append(filtered, t)
				}
			}
			candidates = filtered
		}
	}

	return pickTestFiles(candidates)
}

// pickDirectory shows a select prompt for choosing a test directory.
// Returns empty string for "all directories", false if cancelled.
func pickDirectory(dirs []string) (string, bool) {
	opts := make([]huh.Option[string], 0, len(dirs)+1)
	opts = append(opts, huh.NewOption("All directories", ""))
	for _, d := range dirs {
		opts = append(opts, huh.NewOption(d, d))
	}

	var chosen string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select test directory").
				Options(opts...).
				Value(&chosen),
		),
	)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", false
		}
		fmt.Fprintf(os.Stderr, "selection error: %v\n", err)
		return "", false
	}
	return chosen, true
}

// pickTestFiles shows a select prompt for choosing a test to run.
// The first option is "All tests". Returns the selected file paths, false if cancelled.
func pickTestFiles(tests []runner.ListedTest) ([]string, bool) {
	allPaths := make([]string, 0, len(tests))
	opts := make([]huh.Option[string], 0, len(tests)+1)
	opts = append(opts, huh.NewOption("All tests", ""))
	for _, t := range tests {
		label := t.TestName
		if label == "" {
			label = filepath.Base(t.FilePath)
		}
		opts = append(opts, huh.NewOption(label, t.FilePath))
		allPaths = append(allPaths, t.FilePath)
	}

	var chosen string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select test to run").
				Options(opts...).
				Value(&chosen),
		),
	)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, false
		}
		fmt.Fprintf(os.Stderr, "selection error: %v\n", err)
		return nil, false
	}
	if chosen == "" {
		return allPaths, true
	}
	return []string{chosen}, true
}

// groupByDir returns a sorted, deduplicated list of parent directories from the test list.
func groupByDir(tests []runner.ListedTest) []string {
	seen := make(map[string]bool)
	for _, t := range tests {
		seen[filepath.Dir(t.FilePath)] = true
	}
	dirs := make([]string, 0, len(seen))
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs
}
