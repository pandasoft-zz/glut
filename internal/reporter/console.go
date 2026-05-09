package reporter

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/pandasoft-zz/glut/internal/runner"
)

type Console struct {
	writer  io.Writer
	verbose bool
	quiet   bool
}

func NewConsole(verbose bool, quiet bool) *Console {
	return &Console{
		writer:  os.Stdout,
		verbose: verbose,
		quiet:   quiet,
	}
}

func (c *Console) Start(totalTests int) {
	if c.quiet {
		return
	}
	fmt.Fprintf(c.writer, "Found %d test(s)\n", totalTests)
}

func (c *Console) TestDone(result runner.TestResult) {
	if c.quiet {
		return
	}

	status := "PASS"
	if !result.Passed {
		status = "FAIL"
	}
	fmt.Fprintf(c.writer, "%s  %s (%s)\n", status, displayName(result), formatDuration(result.Duration))

	if result.Error != nil {
		fmt.Fprintf(c.writer, "  error: %v\n", result.Error)
	}
	for _, failure := range result.Failures {
		fmt.Fprintf(c.writer, "  assert: %s\n", failure.Path)
		fmt.Fprintf(c.writer, "  expected: %s\n", failure.Expected)
		fmt.Fprintf(c.writer, "  actual: %s\n", failure.Actual)
	}

	if c.verbose {
		for _, job := range result.JobOutputs {
			if !job.Present {
				continue
			}
			fmt.Fprintf(c.writer, "  job %s exit=%d\n", job.Name, job.ExitStatus)
		}
	}

	if result.PreservedWorkspace {
		fmt.Fprintf(c.writer, "  workspace kept: %s\n", result.WorkspacePath)
	}
}

func (c *Console) Summary(result runner.RunResult) {
	fmt.Fprintf(c.writer, "%d passed, %d failed in %s\n", result.Passed, result.Failed, formatDuration(result.Duration))
}

func PrintList(writer io.Writer, tests []runner.ListedTest) {
	for _, test := range tests {
		name := test.TestName
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Fprintf(writer, "%s\t%s\n", test.FilePath, name)
	}
}

func displayName(result runner.TestResult) string {
	if result.TestName != "" {
		return result.TestName
	}
	return result.FilePath
}

func formatDuration(duration time.Duration) string {
	if duration <= 0 {
		return "0s"
	}
	return duration.Round(time.Millisecond).String()
}
