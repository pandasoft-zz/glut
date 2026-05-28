package main

import (
	"context"
	"fmt"
	"os"
	"time"

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

	defaultRunTimeout = 10 * time.Minute
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
	runWaitTimeout    time.Duration
	runDebug          bool
	runKeepWorkspace  bool
	runDebugPause     string
	runKeepLastFailed int
	runCopyStrategy   string
	runInclude        []string
	runInteractive    bool
	listPattern       string
	lintFormat        string
	doctorFormat      string
	doctorPattern     string
)

var rootCmd = &cobra.Command{
	Use:   "glut",
	Short: "GLUT is a test runner",
	Long: `GLUT runs GitLab CI component tests locally.

A GLUT test file has normal GitLab CI YAML and a .glut metadata document.
GLUT prepares an isolated workspace, starts mocks, runs gitlab-ci-local,
checks asserts, and prints a result.`,
	Example: `  glut run ./tests
  glut run -k release ./tests
  glut lint ./tests
  glut list ./tests
  glut version`,
}

var runCmd = &cobra.Command{
	Use:   "run [paths...]",
	Short: "Run tests",
	Long: `Run GLUT tests from one or more paths.

A path can be a directory or a YAML file. If no path is given, GLUT uses the
current directory. Each test gets its own workspace and mock services.`,
	Example: `  glut run
  glut run ./tests
  glut run ./tests/release.yml
  glut run -k release ./tests
  glut run --report=junit:report.xml ./tests
  glut run --debug --keep-workspace ./tests/release.yml`,
	Run: func(cmd *cobra.Command, args []string) {
		opts := runOptionsFromCommand(args)

		if runInteractive {
			result, exitCode := selectAndRun(context.Background(), opts)
			if result.Error != nil {
				writeError(result.Error)
			}
			os.Exit(int(exitCode))
		}

		sinks, fileReports, err := buildProgressSinks(opts, os.Stdout)
		if err != nil {
			writeError(err)
			os.Exit(ExitError)
		}
		result, exitCode := runner.Run(context.Background(), opts.Paths, runner.RunOptions{
			RunPattern:       opts.Pattern,
			FailFast:         opts.FailFast,
			MaxFail:          opts.MaxFail,
			Verbose:          opts.Verbose,
			Quiet:            opts.Quiet,
			Timeout:          opts.Timeout,
			WaitTimeout:      opts.WaitTimeout,
			Debug:            opts.Debug,
			KeepWorkspace:    opts.KeepWorkspace,
			DebugPause:       opts.DebugPause,
			KeepLastFailed:   opts.KeepLastFailed,
			CopyStrategy:     opts.CopyStrategy,
			Include:          opts.Include,
			Progress:         sinks,
			DockerWaitOutput: os.Stderr,
		})
		if result.Error != nil {
			writeError(result.Error)
		}
		if err := writeFileReports(fileReports); err != nil {
			writeError(err)
			os.Exit(ExitError)
		}
		os.Exit(int(exitCode))
	},
}

var listCmd = &cobra.Command{
	Use:   "list [paths...]",
	Short: "List tests",
	Long: `List GLUT tests without running them.

A path can be a directory or a YAML file. Use --run to filter by test name.`,
	Example: `  glut list
  glut list ./tests
  glut list -k release ./tests`,
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
	Use:   "lint [paths...]",
	Short: "Lint tests",
	Long: `Lint GLUT test files.

Lint checks YAML syntax, .glut schema errors, and semantic mistakes such as
assert.job references to missing pipeline jobs.`,
	Example: `  glut lint
  glut lint ./tests
  glut lint ./tests/release.yml`,
	Run: func(cmd *cobra.Command, args []string) {
		opts := lintOptionsFromCommand(args)
		report := buildLintReport(opts.Paths)
		if err := printLintReport(os.Stdout, os.Stderr, report, opts.Format); err != nil {
			writeError(err)
			os.Exit(ExitError)
		}
		if report.HasErrors {
			os.Exit(ExitTestFail)
		}
		os.Exit(ExitOK)
	},
}

var doctorCmd = &cobra.Command{
	Use:   "doctor [paths...]",
	Short: "Explain tests for AI tools",
	Long: `Explain GLUT test files for AI tools.

Doctor returns lint issues, authoring hints, and job coverage per file.
Use --run to focus on a single test by name substring.
Use JSON output when another tool or AI assistant needs structured feedback.`,
	Example: `  glut doctor ./tests
  glut doctor -k release ./tests
  glut doctor --format=json ./tests/release.yml`,
	Run: func(cmd *cobra.Command, args []string) {
		opts := lintOptionsFromCommand(args)
		opts.Format = doctorFormat
		report := buildDoctorReportFiltered(opts.Paths, doctorPattern)
		if err := printDoctorReport(os.Stdout, os.Stderr, report, opts.Format); err != nil {
			writeError(err)
			os.Exit(ExitError)
		}
		if report.HasErrors {
			os.Exit(ExitTestFail)
		}
		os.Exit(ExitOK)
	},
}

var versionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Print version",
	Long:    "Print the GLUT version and build commit.",
	Example: `  glut version`,
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
	runReports = envList(os.Getenv, "GLUT_REPORT")

	runCmd.Flags().StringVarP(&runPattern, "run", "k", "", "Run tests matching substring or regex")
	runCmd.Flags().BoolVarP(&runFailFast, "fail-fast", "x", envBool(os.Getenv, "GLUT_FAIL_FAST"), "Stop after first failure")
	runCmd.Flags().IntVar(&runMaxFail, "maxfail", 0, "Stop after N failures")
	runCmd.Flags().BoolVarP(&runVerbose, "verbose", "v", envBool(os.Getenv, "GLUT_VERBOSE"), "Verbose output")
	runCmd.Flags().BoolVarP(&runQuiet, "quiet", "q", false, "Quiet output")
	runCmd.Flags().StringVar(&runFormat, "format", runFormat, "Console output format")
	runCmd.Flags().StringArrayVar(&runReports, "report", runReports, "Report output as <format>:<path>, repeatable")
	runCmd.Flags().DurationVar(&runTimeout, "timeout", envDuration(os.Getenv, "GLUT_TIMEOUT", defaultRunTimeout), "Timeout for one test")
	runCmd.Flags().DurationVar(&runWaitTimeout, "wait-timeout", envDuration(os.Getenv, "GLUT_WAIT_TIMEOUT", runner.DefaultWaitTimeout), "Max time to wait for Docker daemon to become ready")
	runCmd.Flags().BoolVar(&runDebug, "debug", envBool(os.Getenv, "GLUT_DEBUG"), "Enable debug mode")
	runCmd.Flags().BoolVar(&runKeepWorkspace, "keep-workspace", envBool(os.Getenv, "GLUT_KEEP_WORKSPACE"), "Keep workspace after run")
	runCmd.Flags().StringVar(&runDebugPause, "debug-pause", "", "Pause point: before-pipeline, before-asserts, after-pipeline, or on-fail")
	runCmd.Flags().IntVar(&runKeepLastFailed, "keep-last-failed", 3, "Keep the last N failed workspaces")
	runCmd.Flags().StringVar(&runCopyStrategy, "copy-strategy", "auto", "Copy strategy: auto, rsync, native")
	runCmd.Flags().StringArrayVar(&runInclude, "include", nil, "Copy only these subdirectories into the workspace (repeatable)")
	runCmd.Flags().BoolVarP(&runInteractive, "interactive", "i", false, "Select tests to run interactively")

	listCmd.Flags().StringVarP(&listPattern, "run", "k", "", "List tests matching substring or regex")
	lintCmd.Flags().StringVar(&lintFormat, "format", "text", "Output format: text or json")
	doctorCmd.Flags().StringVar(&doctorFormat, "format", "text", "Output format: text or json")
	doctorCmd.Flags().StringVarP(&doctorPattern, "run", "k", "", "Analyse tests matching substring")

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(lintCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(versionCmd)
}

func envBool(env func(string) string, name string) bool {
	switch env(name) {
	case "1", "true", "TRUE", "yes", "YES":
		return true
	default:
		return false
	}
}

func envDuration(env func(string) string, name string, fallback time.Duration) time.Duration {
	value := env(name)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return duration
}

func writeError(err error) {
	_, _ = fmt.Fprintln(stderrWriter(), err)
}
