package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pandasoft-zz/glut/internal/reporter"
	"github.com/pandasoft-zz/glut/internal/runner"
	"github.com/spf13/cobra"
)

// version and commit are set at link time via -ldflags "-X main.version=...".
// They are build constants written only by the linker, not mutable state.
var (
	version = "v0.0.0-dev"
	commit  = "unknown"
)

const (
	defaultRunTimeout = 10 * time.Minute
)

// runFlags holds the `glut run` flag values. newRunCmd binds a fresh instance
// into the command's closure, so there is no package-level mutable flag state
// and tests can build option structs directly and run in parallel.
type runFlags struct {
	pattern              string
	failFast             bool
	maxFail              int
	verbose              bool
	quiet                bool
	format               string
	reports              []string
	timeout              time.Duration
	waitTimeout          time.Duration
	debug                bool
	keepWorkspace        bool
	debugPause           string
	keepLastFailed       int
	copyStrategy         string
	dockerVolumeStrategy string
	include              []string
	interactive          bool
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
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

	// Set once on root; cobra traverses up the tree so all subcommands
	// that don't override will use this renderer automatically.
	root.SetHelpFunc(helpFunc)

	root.AddCommand(newRunCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newLintCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newVersionCmd())
	return root
}

func newRunCmd() *cobra.Command {
	flags := &runFlags{}
	cmd := &cobra.Command{
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
			opts := flags.toRunOptions(args)

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			if flags.interactive {
				result, exitCode := selectAndRun(ctx, opts)
				if result.Error != nil {
					writeError(result.Error)
				}
				os.Exit(int(exitCode))
			}

			sinks, fileReports, err := buildProgressSinks(opts, os.Stdout)
			if err != nil {
				writeError(err)
				os.Exit(int(runner.ExitRunnerError))
			}
			result, exitCode := runner.Run(ctx, opts.Paths, toRunnerOptions(opts, sinks, os.Stderr))
			if result.Error != nil {
				writeError(result.Error)
			}
			if err := writeFileReports(fileReports); err != nil {
				writeError(err)
				os.Exit(int(runner.ExitRunnerError))
			}
			os.Exit(int(exitCode))
		},
	}

	f := cmd.Flags()
	f.StringVarP(&flags.pattern, "run", "k", "", "Run tests matching substring or regex")
	f.BoolVarP(&flags.failFast, "fail-fast", "x", envBool(os.Getenv, "GLUT_FAIL_FAST"), "Stop after first failure")
	f.IntVar(&flags.maxFail, "maxfail", 0, "Stop after N failures")
	f.BoolVarP(&flags.verbose, "verbose", "v", envBool(os.Getenv, "GLUT_VERBOSE"), "Verbose output")
	f.BoolVarP(&flags.quiet, "quiet", "q", false, "Quiet output")
	f.StringVar(&flags.format, "format", os.Getenv("GLUT_FORMAT"), "Console output format")
	f.StringArrayVar(&flags.reports, "report", envList(os.Getenv, "GLUT_REPORT"), "Report output as <format>:<path>, repeatable")
	f.DurationVar(&flags.timeout, "timeout", envDuration(os.Getenv, "GLUT_TIMEOUT", defaultRunTimeout), "Timeout for one test")
	f.DurationVar(&flags.waitTimeout, "wait-timeout", envDuration(os.Getenv, "GLUT_WAIT_TIMEOUT", runner.DefaultWaitTimeout), "Max time to wait for Docker daemon to become ready")
	f.BoolVar(&flags.debug, "debug", envBool(os.Getenv, "GLUT_DEBUG"), "Enable debug mode")
	f.BoolVar(&flags.keepWorkspace, "keep-workspace", envBool(os.Getenv, "GLUT_KEEP_WORKSPACE"), "Keep workspace after run")
	f.StringVar(&flags.debugPause, "debug-pause", "", "Pause point: before-pipeline, before-asserts, after-pipeline, or on-fail")
	f.IntVar(&flags.keepLastFailed, "keep-last-failed", 3, "Keep the last N failed workspaces")
	f.StringVar(&flags.copyStrategy, "copy-strategy", "auto", "Copy strategy: auto, rsync, native")
	f.StringVar(&flags.dockerVolumeStrategy, "docker-volume-strategy", "auto", "Docker volume strategy: auto (detect), bind (native Linux), volume (Docker Desktop/WSL2)")
	f.StringArrayVar(&flags.include, "include", nil, "Copy only these subdirectories into the workspace (repeatable)")
	f.BoolVarP(&flags.interactive, "interactive", "i", false, "Select tests to run interactively")
	return cmd
}

func newListCmd() *cobra.Command {
	var pattern string
	cmd := &cobra.Command{
		Use:   "list [paths...]",
		Short: "List tests",
		Long: `List GLUT tests without running them.

A path can be a directory or a YAML file. Use --run to filter by test name.`,
		Example: `  glut list
  glut list ./tests
  glut list -k release ./tests`,
		Run: func(cmd *cobra.Command, args []string) {
			opts := listOptionsFromCommand(args, pattern)
			tests, err := runner.List(context.Background(), opts.Paths, runner.ListOptions{
				RunPattern: opts.Pattern,
			})
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(int(runner.ExitRunnerError))
			}
			reporter.PrintList(os.Stdout, tests)
		},
	}
	cmd.Flags().StringVarP(&pattern, "run", "k", "", "List tests matching substring or regex")
	return cmd
}

func newLintCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "lint [paths...]",
		Short: "Lint tests",
		Long: `Lint GLUT test files.

Lint checks YAML syntax, .glut schema errors, and semantic mistakes such as
assert.job references to missing pipeline jobs.`,
		Example: `  glut lint
  glut lint ./tests
  glut lint ./tests/release.yml`,
		Run: func(cmd *cobra.Command, args []string) {
			opts := lintOptionsFromCommand(args, format)
			if err := checkDefaultTestsDirExists(opts.Paths, len(args) == 0); err != nil {
				writeError(err)
				os.Exit(int(runner.ExitRunnerError))
			}
			report := buildLintReport(opts.Paths)
			if err := printLintReport(os.Stdout, os.Stderr, report, opts.Format); err != nil {
				writeError(err)
				os.Exit(int(runner.ExitRunnerError))
			}
			if report.HasErrors {
				os.Exit(int(runner.ExitTestFailed))
			}
			os.Exit(int(runner.ExitOK))
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	return cmd
}

func newDoctorCmd() *cobra.Command {
	var format string
	var pattern string
	cmd := &cobra.Command{
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
			opts := lintOptionsFromCommand(args, format)
			if err := checkDefaultTestsDirExists(opts.Paths, len(args) == 0); err != nil {
				writeError(err)
				os.Exit(int(runner.ExitRunnerError))
			}
			report := buildDoctorReportFiltered(opts.Paths, pattern)
			if err := printDoctorReport(os.Stdout, os.Stderr, report, opts.Format); err != nil {
				writeError(err)
				os.Exit(int(runner.ExitRunnerError))
			}
			if report.HasErrors {
				os.Exit(int(runner.ExitTestFailed))
			}
			os.Exit(int(runner.ExitOK))
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	cmd.Flags().StringVarP(&pattern, "run", "k", "", "Analyse tests matching substring")
	return cmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "Print version",
		Long:    "Print the GLUT version and build commit.",
		Example: `  glut version`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("glut %s (commit: %s, built: unknown)\n", version, commit)
		},
	}
}

func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(int(runner.ExitRunnerError))
	}
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
		_, _ = fmt.Fprintf(stderrWriter(), "warning: %s=%q is not a valid duration, using default %s\n", name, value, fallback)
		return fallback
	}
	return duration
}

func writeError(err error) {
	_, _ = fmt.Fprintln(stderrWriter(), err)
}
