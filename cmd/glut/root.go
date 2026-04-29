package main

import (
	"fmt"
	"os"

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

var rootCmd = &cobra.Command{
	Use:   "glut",
	Short: "GLUT is a test runner",
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run tests",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
		os.Exit(ExitOK)
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List tests",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
		os.Exit(ExitOK)
	},
}

var lintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Lint tests",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("not implemented")
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
	runCmd.Flags().StringP("run", "k", "", "Run specific tests")
	runCmd.Flags().BoolP("fail-fast", "x", false, "Stop after first failure")
	runCmd.Flags().Int("maxfail", 0, "Stop after N failures")
	runCmd.Flags().BoolP("verbose", "v", false, "Verbose output")
	runCmd.Flags().BoolP("quiet", "q", false, "Quiet output")
	runCmd.Flags().String("format", "", "Output format")
	runCmd.Flags().String("report", "", "Report format")
	runCmd.Flags().Duration("timeout", 0, "Global timeout")
	runCmd.Flags().Bool("debug", false, "Enable debug mode")
	runCmd.Flags().Bool("keep-workspace", false, "Keep workspace after run")
	runCmd.Flags().Bool("debug-pause", false, "Pause on failure for debugging")

	setEnvDefault := func(flagName, envVar string) {
		if val := os.Getenv(envVar); val != "" {
			runCmd.Flags().Lookup(flagName).DefValue = val
			_ = runCmd.Flags().Set(flagName, val)
		}
	}

	setEnvDefault("format", "GLUT_FORMAT")
	setEnvDefault("report", "GLUT_REPORT")
	setEnvDefault("timeout", "GLUT_TIMEOUT")
	setEnvDefault("verbose", "GLUT_VERBOSE")
	setEnvDefault("fail-fast", "GLUT_FAIL_FAST")
	setEnvDefault("debug", "GLUT_DEBUG")
	setEnvDefault("keep-workspace", "GLUT_KEEP_WORKSPACE")

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(lintCmd)
	rootCmd.AddCommand(versionCmd)
}
