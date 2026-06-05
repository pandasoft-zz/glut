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
	total   int
	done    int
	st      consoleStyles
}

type dotsConsole struct {
	writer      io.Writer
	quiet       bool
	verbose     bool
	debug       bool
	wroteStatus bool
	st          consoleStyles
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
			st:      newConsoleStyles(writer),
		}, nil
	case "dots":
		return &dotsConsole{
			writer:  writer,
			quiet:   opts.Quiet,
			verbose: opts.Verbose,
			debug:   opts.Debug,
			st:      newConsoleStyles(writer),
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
	c.total = totalTests
	if c.quiet {
		return
	}
	logo := logoEmoji + " " + c.st.logoGL.Render("GL") + c.st.logoUT.Render("UT")
	count := c.st.dim.Render(fmt.Sprintf("Running %d tests", totalTests))
	writef(c.writer, "%s  %s\n\n", logo, count)
}

func (c *prettyConsole) TestRetry(testName string, err error) {
	if c.quiet {
		return
	}
	writef(c.writer, "  %s retrying %q after infrastructure error: %v\n",
		c.st.dim.Render("[glut]"), testName, err)
}

func (c *prettyConsole) TestDone(result runner.TestResult) {
	c.done++
	if !c.quiet {
		width := len(fmt.Sprintf("%d", c.total))
		counter := c.st.counter.Render(fmt.Sprintf("[%*d/%d]", width, c.done, c.total))
		var icon, failTag string
		if result.Passed {
			icon = c.st.pass.Render(passEmoji)
		} else {
			icon = c.st.fail.Render(failEmoji)
			failTag = "  " + c.st.fail.Render("FAILED")
		}
		path := c.st.path.Render(result.FilePath)
		dur := c.st.dur.Render(formatDuration(result.Duration))
		writef(c.writer, "%s %s  %-45s  %s%s\n", counter, icon, path, dur, failTag)
	}

	if !result.Passed {
		writePrettyFailure(c.writer, c.st, result, c.debug)
		return
	}

	if c.verbose {
		writeJobLogs(c.writer, c.st, result.JobOutputs, false)
	}

	if result.PreservedWorkspace {
		writef(c.writer, "  workspace kept: %s\n", result.WorkspacePath)
	}
}

func (c *prettyConsole) Summary(result runner.RunResult) {
	sep := c.st.dim.Render(strings.Repeat("─", 60))
	writef(c.writer, "\n%s\n", sep)

	parts := make([]string, 0, 2)
	if result.Passed > 0 {
		parts = append(parts, c.st.pass.Render(fmt.Sprintf("%s %d passed", passEmoji, result.Passed)))
	}
	if result.Failed > 0 {
		parts = append(parts, c.st.fail.Render(fmt.Sprintf("%s %d failed", failEmoji, result.Failed)))
	}
	dur := c.st.dim.Render("in " + formatDuration(result.Duration))
	writef(c.writer, "%s  %s\n", strings.Join(parts, "   "), dur)
}

func (c *dotsConsole) Start(totalTests int) {
	_ = totalTests
}

func (c *dotsConsole) TestRetry(testName string, err error) {
	if c.quiet {
		return
	}
	if c.wroteStatus {
		writef(c.writer, "\n")
		c.wroteStatus = false
	}
	writef(c.writer, "  %s retrying %q after infrastructure error: %v\n",
		c.st.dim.Render("[glut]"), testName, err)
}

func (c *dotsConsole) TestDone(result runner.TestResult) {
	if !c.quiet {
		if result.Passed {
			writef(c.writer, "%s", c.st.pass.Render("."))
		} else {
			writef(c.writer, "%s", c.st.fail.Render("F"))
		}
		c.wroteStatus = true
	}

	if !result.Passed {
		if c.wroteStatus {
			writef(c.writer, "\n")
			c.wroteStatus = false
		}
		writePrettyFailure(c.writer, c.st, result, c.debug)
		return
	}

	if c.verbose {
		if c.wroteStatus {
			writef(c.writer, "\n")
			c.wroteStatus = false
		}
		writeJobLogs(c.writer, c.st, result.JobOutputs, false)
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

func (c *jsonConsole) TestRetry(_ string, _ error) {}

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

func writePrettyFailure(writer io.Writer, st consoleStyles, result runner.TestResult, debug bool) {
	writef(writer, "\n%s  %s\n", st.fail.Render(failEmoji+" FAILED"), st.path.Render(result.FilePath))
	if result.TestName != "" {
		writef(writer, "  %s %q\n", st.dim.Render("test:"), result.TestName)
	}

	for _, failure := range result.Failures {
		writef(writer, "\n  %s\n", st.fail.Render(failure.Path))
		writef(writer, "    %s %s\n", st.dim.Render("expected:"), failure.Expected)
		writef(writer, "    %s %s\n", st.dim.Render("actual:  "), failure.Actual)
	}

	if result.Error != nil {
		writef(writer, "\n  %s %s\n", st.fail.Render("error:"), result.Error.Error())
	}

	writeJobLogs(writer, st, result.JobOutputs, !debug)
	if debug && result.Debug != nil {
		writeDebugData(writer, *result.Debug)
	}

	if result.PreservedWorkspace && result.WorkspacePath != "" {
		writef(writer, "\n  %s %s\n", st.dim.Render("workspace kept:"), result.WorkspacePath)
	} else if !debug {
		writef(writer, "\n  %s\n", st.dim.Render("hint: run with --debug for full job logs and mock call history"))
		writef(writer, "  %s\n", st.dim.Render("hint: run with --keep-workspace to keep the workspace"))
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

func writeJobLogs(writer io.Writer, st consoleStyles, outputs map[string]executor.JobOutput, tailOnly bool) {
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

		label := "stdout"
		if tailOnly {
			label = "stdout (last 50 lines)"
		}
		if strings.TrimSpace(stdout) != "" {
			writeJobBlock(writer, st, label, job.Name, stdout)
		}

		errLabel := "stderr"
		if tailOnly {
			errLabel = "stderr (last 50 lines)"
		}
		if strings.TrimSpace(stderr) != "" {
			writeJobBlock(writer, st, errLabel, job.Name, stderr)
		}
	}
}

func writeJobBlock(writer io.Writer, st consoleStyles, label, jobName, content string) {
	const lineWidth = 60
	const leftDashes = 3
	header := fmt.Sprintf(" %s: %q ", label, jobName)
	rightLen := lineWidth - leftDashes - len(header)
	if rightLen < 2 {
		rightLen = 2
	}
	topLine := strings.Repeat("─", leftDashes) + header + strings.Repeat("─", rightLen)
	botLine := strings.Repeat("─", lineWidth)
	writef(writer, "\n%s\n", st.jobSep.Render(topLine))
	for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		writef(writer, "%s\n", st.jobOut.Render("  "+line))
	}
	writef(writer, "%s\n", st.jobSep.Render(botLine))
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
