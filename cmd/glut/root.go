package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/pandasoft-zz/glut/internal/parser"
	"github.com/pandasoft-zz/glut/internal/reporter"
	"github.com/pandasoft-zz/glut/internal/runner"
	"github.com/spf13/cobra"
)

var (
	version = "v0.0.0-dev"
	commit  = "unknown"
)

const (
	ExitOK       = 0
	ExitTestFail = 1
	ExitError    = 2
)

var (
	runPattern        string
	runFailFast       bool
	runMaxFail        int
	runVerbose        bool
	runQuiet          bool
	runFormat         string
	runReports        []string
	runTimeout        time.Duration
	runDebug          bool
	runKeepWorkspace  bool
	runDebugPause     string
	runKeepLastFailed int
	listPattern       string
)

var rootCmd = &cobra.Command{
	Use:   "glut",
	Short: "GLUT is a test runner",
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run tests",
	Run: func(cmd *cobra.Command, args []string) {
		opts := runOptionsFromCommand(args)
		sinks, fileReports, err := buildProgressSinks(opts, os.Stdout)
		if err != nil {
			fmt.Fprintln(stderrWriter(), err)
			os.Exit(ExitError)
		}
		result, exitCode := runner.Run(context.Background(), opts.Paths, runner.RunOptions{
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
			Progress:       sinks,
		})
		_ = result
		if err := writeFileReports(fileReports); err != nil {
			fmt.Fprintln(stderrWriter(), err)
			os.Exit(ExitError)
		}
		os.Exit(int(exitCode))
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List tests",
	Run: func(cmd *cobra.Command, args []string) {
		opts := listOptionsFromCommand(args)
		tests, err := runner.List(context.Background(), opts.Paths, runner.ListOptions{
			RunPattern: opts.Pattern,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(ExitError)
		}
		reporter.PrintList(os.Stdout, tests)
	},
}

var lintCmd = &cobra.Command{
	Use:   "lint [dirs...]",
	Short: "Lint tests",
	Run: func(cmd *cobra.Command, args []string) {
		opts := lintOptionsFromCommand(args)

		hasError := false
		for _, dir := range opts.Paths {
			files, errs := parser.ParseDir(dir)
			if len(errs) > 0 {
				for _, err := range errs {
					fmt.Fprintf(os.Stderr, "Parse error: %v\n", err)
				}
				hasError = true
			}

			for _, f := range files {
				lints := parser.Lint(f.FilePath)
				for _, l := range lints {
					prefix := "WARNING"
					if l.Level == parser.LevelError {
						prefix = "ERROR"
						hasError = true
					}
					fmt.Printf("[%s] %s: %s\n", prefix, l.File, l.Message)
				}
			}
		}

		if hasError {
			os.Exit(ExitTestFail)
		}
		os.Exit(ExitOK)
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("glut %s (commit: %s, built: unknown)\n", version, commit)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(ExitError)
	}
}

func init() {
	runFormat = os.Getenv("GLUT_FORMAT")
	runReports = envList("GLUT_REPORT")

	runCmd.Flags().StringVarP(&runPattern, "run", "k", "", "Run tests matching substring or regex")
	runCmd.Flags().BoolVarP(&runFailFast, "fail-fast", "x", envBool("GLUT_FAIL_FAST"), "Stop after first failure")
	runCmd.Flags().IntVar(&runMaxFail, "maxfail", 0, "Stop after N failures")
	runCmd.Flags().BoolVarP(&runVerbose, "verbose", "v", envBool("GLUT_VERBOSE"), "Verbose output")
	runCmd.Flags().BoolVarP(&runQuiet, "quiet", "q", false, "Quiet output")
	runCmd.Flags().StringVar(&runFormat, "format", runFormat, "Console output format")
	runCmd.Flags().StringArrayVar(&runReports, "report", runReports, "Report output as <format>:<path>, repeatable")
	runCmd.Flags().DurationVar(&runTimeout, "timeout", envDuration("GLUT_TIMEOUT"), "Timeout for one test")
	runCmd.Flags().BoolVar(&runDebug, "debug", envBool("GLUT_DEBUG"), "Enable debug mode")
	runCmd.Flags().BoolVar(&runKeepWorkspace, "keep-workspace", envBool("GLUT_KEEP_WORKSPACE"), "Keep workspace after run")
	runCmd.Flags().StringVar(&runDebugPause, "debug-pause", "", "Pause point: before-pipeline, after-pipeline, or on-fail")
	runCmd.Flags().IntVar(&runKeepLastFailed, "keep-last-failed", 3, "Keep the last N failed workspaces")

	listCmd.Flags().StringVarP(&listPattern, "run", "k", "", "List tests matching substring or regex")

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(lintCmd)
	rootCmd.AddCommand(versionCmd)
}

func envBool(name string) bool {
	switch os.Getenv(name) {
	case "1", "true", "TRUE", "yes", "YES":
		return true
	default:
		return false
	}
}

func envDuration(name string) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return 0
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0
	}
	return duration
}
