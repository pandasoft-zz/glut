package main

import (
	"fmt"
	"os"
	"time"

	"github.com/pandasoft-zz/glut/internal/parser"
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
	runPattern       string
	runFailFast      bool
	runMaxFail       int
	runVerbose       bool
	runQuiet         bool
	runFormat        string
	runReports       []string
	runTimeout       time.Duration
	runDebug         bool
	runKeepWorkspace bool
	runDebugPause    string
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
		_ = opts
		fmt.Fprintln(os.Stderr, "glut run is not implemented yet")
		os.Exit(ExitError)
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List tests",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Fprintln(os.Stderr, "glut list is not implemented yet")
		os.Exit(ExitError)
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
