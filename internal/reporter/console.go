package reporter

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pandasoft-zz/glut/internal/asserter"
	"github.com/pandasoft-zz/glut/internal/executor"
	"github.com/pandasoft-zz/glut/internal/runner"
)

type ConsoleOptions struct {
	Format  string
	Quiet   bool
	Verbose bool
	Debug   bool
	Writer  io.Writer
}

type prettyConsole struct {
	writer  io.Writer
	quiet   bool
	verbose bool
	debug   bool
}

type dotsConsole struct {
	writer      io.Writer
	quiet       bool
	verbose     bool
	debug       bool
	wroteStatus bool
}

type jsonConsole struct {
	writer io.Writer
	quiet  bool
}

type jsonFailure struct {
	Path     string `json:"path"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type jsonResult struct {
	FilePath   string        `json:"file"`
	Name       string        `json:"name,omitempty"`
	Passed     bool          `json:"passed"`
	DurationMS int64         `json:"duration_ms"`
	Failures   []jsonFailure `json:"failures,omitempty"`
	Error      string        `json:"error,omitempty"`
}

type jsonSummary struct {
	Passed     int   `json:"passed"`
	Failed     int   `json:"failed"`
	DurationMS int64 `json:"duration_ms"`
}

func NewConsole(opts ConsoleOptions) (runner.ProgressSink, error) {
	writer := opts.Writer
	if writer == nil {
		writer = os.Stdout
	}

	format := strings.TrimSpace(opts.Format)
	if format == "" {
		format = "pretty"
	}

	switch format {
	case "pretty":
		return &prettyConsole{
			writer:  writer,
			quiet:   opts.Quiet,
			verbose: opts.Verbose,
			debug:   opts.Debug,
		}, nil
	case "dots":
		return &dotsConsole{
			writer:  writer,
			quiet:   opts.Quiet,
			verbose: opts.Verbose,
			debug:   opts.Debug,
		}, nil
	case "json":
		return &jsonConsole{
			writer: writer,
			quiet:  opts.Quiet,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported console format %q", format)
	}
}

func (c *prettyConsole) Start(totalTests int) {
	_ = totalTests
}

func (c *prettyConsole) TestDone(result runner.TestResult) {
	if !c.quiet {
		status := "PASS"
		if !result.Passed {
			status = "FAIL"
		}
		writef(c.writer, "%-40s  %-4s  %s\n", result.FilePath, status, formatDuration(result.Duration))
	}

	if !result.Passed {
		writePrettyFailure(c.writer, result, c.debug)
		return
	}

	if c.verbose {
		writeJobLogs(c.writer, result.JobOutputs, false)
	}

	if result.PreservedWorkspace {
		writef(c.writer, "  workspace kept: %s\n", result.WorkspacePath)
	}
}

func (c *prettyConsole) Summary(result runner.RunResult) {
	writef(c.writer, "%s\n", summaryLine(result))
}

func (c *dotsConsole) Start(totalTests int) {
	_ = totalTests
}

func (c *dotsConsole) TestDone(result runner.TestResult) {
	if !c.quiet {
		if result.Passed {
			writef(c.writer, ".")
		} else {
			writef(c.writer, "F")
		}
		c.wroteStatus = true
	}

	if !result.Passed {
		if c.wroteStatus {
			writef(c.writer, "\n")
			c.wroteStatus = false
		}
		writePrettyFailure(c.writer, result, c.debug)
		return
	}

	if c.verbose {
		if c.wroteStatus {
			writef(c.writer, "\n")
			c.wroteStatus = false
		}
		writeJobLogs(c.writer, result.JobOutputs, false)
	}
}

func (c *dotsConsole) Summary(result runner.RunResult) {
	if c.wroteStatus {
		writef(c.writer, "\n")
	}
	writef(c.writer, "%s\n", summaryLine(result))
	c.wroteStatus = false
}

func (c *jsonConsole) Start(totalTests int) {
	_ = totalTests
}

func (c *jsonConsole) TestDone(result runner.TestResult) {
	if c.quiet && result.Passed {
		return
	}

	entry := jsonResult{
		FilePath:   result.FilePath,
		Name:       result.TestName,
		Passed:     result.Passed,
		DurationMS: result.Duration.Milliseconds(),
		Failures:   jsonFailures(result.Failures),
	}
	if result.Error != nil {
		entry.Error = result.Error.Error()
	}
	writeJSON(c.writer, entry)
}

func (c *jsonConsole) Summary(result runner.RunResult) {
	writeJSON(c.writer, jsonSummary{
		Passed:     result.Passed,
		Failed:     result.Failed,
		DurationMS: result.Duration.Milliseconds(),
	})
}

func PrintList(writer io.Writer, tests []runner.ListedTest) {
	for _, test := range tests {
		name := test.TestName
		if name == "" {
			name = "(unnamed)"
		}
		writef(writer, "%s\t%s\n", test.FilePath, name)
	}
}

func writePrettyFailure(writer io.Writer, result runner.TestResult, debug bool) {
	writef(writer, "\nFAILED  %s\n", result.FilePath)
	if result.TestName != "" {
		writef(writer, "  Test: %q\n", result.TestName)
	}

	for _, failure := range result.Failures {
		writef(writer, "\n  %s\n", failure.Path)
		writef(writer, "    expected: %s\n", failure.Expected)
		writef(writer, "    actual:   %s\n", failure.Actual)
	}

	if result.Error != nil {
		writef(writer, "\n  Error: %s\n", result.Error.Error())
	}

	writeJobLogs(writer, result.JobOutputs, !debug)
	if debug && result.Debug != nil {
		writeDebugData(writer, *result.Debug)
	}

	if result.PreservedWorkspace && result.WorkspacePath != "" {
		writef(writer, "\n  Workspace kept: %s\n", result.WorkspacePath)
	} else if !debug {
		writef(writer, "\n  Hint: run with --debug for full job logs and mock call history\n")
		writef(writer, "  Hint: run with --keep-workspace to keep the workspace\n")
	}
}

func writeDebugData(writer io.Writer, debug runner.DebugData) {
	writeRawDebugBlock(writer, "Raw gitlab-ci-local stdout", debug.RawStdout)
	writeRawDebugBlock(writer, "Raw gitlab-ci-local stderr", debug.RawStderr)
	writeJSONDebugBlock(writer, "Mock binary calls", debug.BinaryLogs)
	writeJSONDebugBlock(writer, "Mock API calls", debug.APICalls)
	writeRawDebugBlock(writer, "Workspace git log", debug.WorkspaceGitLog)
	writeRawDebugBlock(writer, "Origin git log", debug.OriginGitLog)
	writePhaseTimings(writer, debug.PhaseTimings)
	writeStringListBlock(writer, "Cleanup errors", debug.CleanupErrors)
}

func writeRawDebugBlock(writer io.Writer, title string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	writef(writer, "\n  %s:\n", title)
	writeIndentedBlock(writer, value, "  ")
}

func writeJSONDebugBlock(writer io.Writer, title string, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil || strings.TrimSpace(string(data)) == "null" || strings.TrimSpace(string(data)) == "{}" || strings.TrimSpace(string(data)) == "[]" {
		return
	}
	writef(writer, "\n  %s:\n", title)
	writeIndentedBlock(writer, string(data), "  ")
}

func writePhaseTimings(writer io.Writer, timings map[string]time.Duration) {
	if len(timings) == 0 {
		return
	}
	names := make([]string, 0, len(timings))
	for name := range timings {
		names = append(names, name)
	}
	sort.Strings(names)
	writef(writer, "\n  Phase timings:\n")
	for _, name := range names {
		writef(writer, "  %s: %s\n", name, formatDuration(timings[name]))
	}
}

func writeStringListBlock(writer io.Writer, title string, values []string) {
	if len(values) == 0 {
		return
	}
	writef(writer, "\n  %s:\n", title)
	for _, value := range values {
		writef(writer, "  - %s\n", value)
	}
}

func writeJobLogs(writer io.Writer, outputs map[string]executor.JobOutput, tailOnly bool) {
	names := sortedJobNames(outputs)
	for _, name := range names {
		job := outputs[name]
		if !job.Present {
			continue
		}

		stdout := job.Stdout
		stderr := job.Stderr
		if tailOnly {
			stdout = tailLines(stdout, 50)
			stderr = tailLines(stderr, 50)
		}

		if strings.TrimSpace(stdout) != "" {
			title := fmt.Sprintf("  Stdout of %q", job.Name)
			if tailOnly {
				title += " (last 50 lines)"
			}
			writef(writer, "\n%s:\n", title)
			writeIndentedBlock(writer, stdout, "  ")
		}
		if strings.TrimSpace(stderr) != "" {
			title := fmt.Sprintf("  Stderr of %q", job.Name)
			if tailOnly {
				title += " (last 50 lines)"
			}
			writef(writer, "\n%s:\n", title)
			writeIndentedBlock(writer, stderr, "  ")
		}
	}
}

func sortedJobNames(outputs map[string]executor.JobOutput) []string {
	names := make([]string, 0, len(outputs))
	for name := range outputs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func tailLines(raw string, maxLines int) string {
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return ""
	}

	lines := strings.Split(raw, "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}

func writeIndentedBlock(writer io.Writer, raw string, prefix string) {
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		writef(writer, "%s%s\n", prefix, line)
	}
}

func summaryLine(result runner.RunResult) string {
	parts := make([]string, 0, 2)
	if result.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", result.Failed))
	}
	if result.Passed > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d passed", result.Passed))
	}
	return fmt.Sprintf("%s in %s", strings.Join(parts, ", "), formatDuration(result.Duration))
}

func jsonFailures(failures []asserter.AssertResult) []jsonFailure {
	if len(failures) == 0 {
		return nil
	}

	items := make([]jsonFailure, 0, len(failures))
	for _, failure := range failures {
		items = append(items, jsonFailure{
			Path:     failure.Path,
			Expected: failure.Expected,
			Actual:   failure.Actual,
		})
	}
	return items
}

func writeJSON(writer io.Writer, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		writef(writer, "{\"error\":%q}\n", err.Error())
		return
	}
	writef(writer, "%s\n", data)
}

func testDisplayName(result runner.TestResult) string {
	if result.TestName != "" {
		return result.TestName
	}
	return filepath.Base(result.FilePath)
}

func failureMessage(result runner.TestResult) string {
	parts := make([]string, 0, len(result.Failures)+1)
	for _, failure := range result.Failures {
		parts = append(parts, fmt.Sprintf("%s expected %s, got %s", failure.Path, failure.Expected, failure.Actual))
	}
	if result.Error != nil {
		parts = append(parts, result.Error.Error())
	}
	return strings.Join(parts, "; ")
}

func formatDuration(duration time.Duration) string {
	if duration <= 0 {
		return "0s"
	}
	return duration.Round(time.Millisecond).String()
}

func writef(writer io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(writer, format, args...)
}
