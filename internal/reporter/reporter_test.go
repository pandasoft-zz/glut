package reporter

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pandasoft-zz/glut/internal/asserter"
	"github.com/pandasoft-zz/glut/internal/executor"
	"github.com/pandasoft-zz/glut/internal/mockserver"
	"github.com/pandasoft-zz/glut/internal/mockwrapper"
	"github.com/pandasoft-zz/glut/internal/runner"
)

func TestPrettyConsoleShowsVersionInTitle(t *testing.T) {
	var buffer bytes.Buffer
	console, err := NewConsole(ConsoleOptions{Version: "v1.10.1", Writer: &buffer})
	if err != nil {
		t.Fatalf("NewConsole() error = %v", err)
	}

	console.Start(3)

	output := buffer.String()
	if !strings.Contains(output, "(v1.10.1)") {
		t.Fatalf("title missing version: %s", output)
	}
}

func TestPrettyConsoleOmitsVersionWhenEmpty(t *testing.T) {
	var buffer bytes.Buffer
	console, err := NewConsole(ConsoleOptions{Writer: &buffer})
	if err != nil {
		t.Fatalf("NewConsole() error = %v", err)
	}

	console.Start(3)

	output := buffer.String()
	if strings.Contains(output, "(") {
		t.Fatalf("title should not contain version parentheses: %s", output)
	}
}

func TestPrettyConsoleFormatsPassFailAndSummary(t *testing.T) {
	var buffer bytes.Buffer
	console, err := NewConsole(ConsoleOptions{Writer: &buffer})
	if err != nil {
		t.Fatalf("NewConsole() error = %v", err)
	}

	console.Start(2)
	console.TestDone(samplePassResult())
	console.TestDone(sampleFailResult())
	console.Summary(sampleRunResult())

	output := buffer.String()
	if !strings.Contains(output, "tests/pass.yml") || !strings.Contains(output, passEmoji) {
		t.Fatalf("pretty output missing pass line: %s", output)
	}
	if !strings.Contains(output, "tests/fail.yml") || !strings.Contains(output, "FAIL") {
		t.Fatalf("pretty output missing fail line: %s", output)
	}
	if !strings.Contains(output, "FAILED  tests/fail.yml") {
		t.Fatalf("pretty output missing failure header: %s", output)
	}
	if !strings.Contains(output, "assert.job.\"release:publish\".exit-status") {
		t.Fatalf("pretty output missing assert path: %s", output)
	}
	if !strings.Contains(output, "1 passed") || !strings.Contains(output, "1 failed") {
		t.Fatalf("pretty output missing summary counts: %s", output)
	}
}

func TestDotsConsoleFormatsOutput(t *testing.T) {
	var buffer bytes.Buffer
	console, err := NewConsole(ConsoleOptions{Format: "dots", Writer: &buffer})
	if err != nil {
		t.Fatalf("NewConsole() error = %v", err)
	}

	console.TestDone(samplePassResult())
	console.TestDone(sampleFailResult())
	console.Summary(sampleRunResult())

	output := buffer.String()
	if !strings.Contains(output, ".F") {
		t.Fatalf("dots output missing status chars: %q", output)
	}
	if !strings.Contains(output, "1 failed, 1 passed in 3s") {
		t.Fatalf("dots output missing summary: %q", output)
	}
}

func TestJSONConsoleOutputsJSONLines(t *testing.T) {
	var buffer bytes.Buffer
	console, err := NewConsole(ConsoleOptions{Format: "json", Writer: &buffer})
	if err != nil {
		t.Fatalf("NewConsole() error = %v", err)
	}

	console.TestDone(samplePassResult())
	console.TestDone(sampleFailResult())
	console.Summary(sampleRunResult())

	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("json output lines = %d, want at least 3: %q", len(lines), buffer.String())
	}

	for _, line := range lines {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("json line %q did not parse: %v", line, err)
		}
	}
}

func TestDebugModeShowsCapturedDebugData(t *testing.T) {
	var buffer bytes.Buffer
	console, err := NewConsole(ConsoleOptions{Debug: true, Writer: &buffer})
	if err != nil {
		t.Fatalf("NewConsole() error = %v", err)
	}

	result := sampleFailResult()
	result.Debug = &runner.DebugData{
		RawStdout:       "full stdout",
		RawStderr:       "full stderr",
		BinaryLogs:      map[string][]mockwrapper.BinaryCall{"release-cli": {{Name: "release-cli", Args: []string{"create"}}}},
		APICalls:        []mockserver.APICall{{Method: "POST", Path: "/api/v4/projects/1/releases"}},
		WorkspaceGitLog: "abc123 main",
		OriginGitLog:    "def456 origin/main",
		PhaseTimings: map[string]time.Duration{
			"pipeline": time.Second,
		},
		CleanupErrors: []string{"remove workspace failed"},
	}
	console.TestDone(result)

	output := buffer.String()
	for _, want := range []string{"Raw gitlab-ci-local stdout", "full stdout", "Mock binary calls", "Mock API calls", "Workspace git log", "Origin git log", "Phase timings", "remove workspace failed"} {
		if !strings.Contains(output, want) {
			t.Fatalf("debug output missing %q: %s", want, output)
		}
	}
	if strings.Contains(output, "Hint: run with --debug") {
		t.Fatalf("debug output should not include debug hint: %s", output)
	}
}

func TestJUnitXMLStructureAndEscaping(t *testing.T) {
	report := NewJUnit()
	report.TestDone(samplePassResult())
	report.TestDone(sampleFailResultWithXML())
	report.Summary(sampleRunResult())

	path := filepath.Join(t.TempDir(), "report.xml")
	if err := report.WriteFile(path); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), "&lt;tag&gt;&amp;bad") {
		t.Fatalf("JUnit output did not escape XML: %s", string(data))
	}

	var decoded junitSuites
	if err := xml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("xml.Unmarshal() error = %v", err)
	}
	if decoded.XMLName.Local != "testsuites" {
		t.Fatalf("root = %s, want testsuites", decoded.XMLName.Local)
	}
	if len(decoded.Suites) == 0 || len(decoded.Suites[0].Cases) == 0 {
		t.Fatalf("decoded suites = %#v", decoded.Suites)
	}
}

// TestJUnitCountsFailedTestcasesNotFailureElements guards against
// suite.Failures incrementing once per failed assertion instead of once per
// failed testcase: a test with 3 failed assertions must report failures="1"
// (one failed test), not failures="3".
func TestJUnitCountsFailedTestcasesNotFailureElements(t *testing.T) {
	multi := sampleFailResult()
	multi.Failures = []asserter.AssertResult{
		{Path: "assert.job.a", Expected: "1", Actual: "2"},
		{Path: "assert.job.b", Expected: "3", Actual: "4"},
		{Path: "assert.job.c", Expected: "5", Actual: "6"},
	}

	report := NewJUnit()
	report.TestDone(multi)
	report.Summary(runner.RunResult{
		Tests:    []runner.TestResult{multi},
		Failed:   1,
		Duration: time.Second,
	})

	path := filepath.Join(t.TempDir(), "multi-failure.xml")
	if err := report.WriteFile(path); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var decoded junitSuites
	if err := xml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("xml.Unmarshal() error = %v", err)
	}
	if decoded.Failures != 1 {
		t.Fatalf("root failures = %d, want 1 (one failed testcase, not 3 failed assertions)", decoded.Failures)
	}
	if len(decoded.Suites) != 1 || decoded.Suites[0].Failures != 1 {
		t.Fatalf("suite failures = %#v, want 1", decoded.Suites)
	}
	if len(decoded.Suites[0].Cases) != 1 || len(decoded.Suites[0].Cases[0].Failures) != 3 {
		t.Fatalf("expected one testcase with 3 <failure> elements, got %#v", decoded.Suites[0].Cases)
	}
}

func TestJUnitGroupsByDirectory(t *testing.T) {
	report := NewJUnit()
	first := samplePassResult()
	first.FilePath = filepath.Join("tests", "image-build", "one.yml")
	second := samplePassResult()
	second.FilePath = filepath.Join("tests", "release", "two.yml")

	report.TestDone(first)
	report.TestDone(second)
	report.Summary(runner.RunResult{
		Tests:    []runner.TestResult{first, second},
		Passed:   2,
		Duration: 2 * time.Second,
	})

	path := filepath.Join(t.TempDir(), "grouped.xml")
	if err := report.WriteFile(path); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), "tests/image-build") || !strings.Contains(string(data), "tests/release") {
		t.Fatalf("JUnit output missing grouped directories: %s", string(data))
	}
}

// TestJUnitSuiteTimeSumsDurationsExactly guards against summing suite
// duration by re-parsing each testcase's already-rounded "%.3f" time string:
// two cases at 333.333ms each sum to 666.666ms (rounds to "0.667"), but
// re-parsing the rounded per-case strings ("0.333" + "0.333") gives 666ms
// ("0.666") — a different, wrong answer purely from the string round-trip.
func TestJUnitSuiteTimeSumsDurationsExactly(t *testing.T) {
	report := NewJUnit()
	first := samplePassResult()
	first.FilePath = filepath.Join("tests", "group", "one.yml")
	first.Duration = 333333 * time.Microsecond
	second := samplePassResult()
	second.FilePath = filepath.Join("tests", "group", "two.yml")
	second.Duration = 333333 * time.Microsecond

	report.TestDone(first)
	report.TestDone(second)
	report.Summary(runner.RunResult{
		Tests:    []runner.TestResult{first, second},
		Passed:   2,
		Duration: first.Duration + second.Duration,
	})

	path := filepath.Join(t.TempDir(), "precise.xml")
	if err := report.WriteFile(path); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var suites junitSuites
	if err := xml.Unmarshal(data, &suites); err != nil {
		t.Fatalf("xml.Unmarshal() error = %v", err)
	}
	if len(suites.Suites) != 1 {
		t.Fatalf("expected 1 suite, got %d: %s", len(suites.Suites), data)
	}
	if got := suites.Suites[0].Time; got != "0.667" {
		t.Fatalf("suite time = %q, want %q (sum of the exact durations, not a re-parse of the rounded per-case strings)", got, "0.667")
	}
}

func TestTAPFormatsHeaderCountAndResults(t *testing.T) {
	report := NewTAP()
	report.TestDone(samplePassResult())
	report.TestDone(sampleFailResult())
	report.Summary(sampleRunResult())

	path := filepath.Join(t.TempDir(), "report.tap")
	if err := report.WriteFile(path); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	output := string(data)
	if !strings.Contains(output, "TAP version 14") {
		t.Fatalf("TAP output missing header: %s", output)
	}
	if !strings.Contains(output, "1..2") {
		t.Fatalf("TAP output missing plan: %s", output)
	}
	if !strings.Contains(output, "ok 1") || !strings.Contains(output, "not ok 2") {
		t.Fatalf("TAP output missing result lines: %s", output)
	}
}

func TestQuietModeSuppressesPassingProgressButKeepsFailuresAndSummary(t *testing.T) {
	var buffer bytes.Buffer
	console, err := NewConsole(ConsoleOptions{Quiet: true, Writer: &buffer})
	if err != nil {
		t.Fatalf("NewConsole() error = %v", err)
	}

	console.TestDone(samplePassResult())
	console.TestDone(sampleFailResult())
	console.Summary(sampleRunResult())

	output := buffer.String()
	if strings.Contains(output, "tests/pass.yml") && strings.Contains(output, "PASS") {
		t.Fatalf("quiet output should not show passing progress: %s", output)
	}
	if !strings.Contains(output, "FAILED  tests/fail.yml") {
		t.Fatalf("quiet output missing failure details: %s", output)
	}
	if !strings.Contains(output, "1 passed") || !strings.Contains(output, "1 failed") {
		t.Fatalf("quiet output missing summary counts: %s", output)
	}
}

func TestVerboseModeShowsPassingJobOutput(t *testing.T) {
	var buffer bytes.Buffer
	console, err := NewConsole(ConsoleOptions{Verbose: true, Writer: &buffer})
	if err != nil {
		t.Fatalf("NewConsole() error = %v", err)
	}

	console.TestDone(samplePassResult())

	output := buffer.String()
	if !strings.Contains(output, `stdout: "pass-job"`) {
		t.Fatalf("verbose output missing stdout section: %s", output)
	}
	if !strings.Contains(output, "all good") {
		t.Fatalf("verbose output missing stdout data: %s", output)
	}
}

func TestPrintListUsesFallbackName(t *testing.T) {
	var buffer bytes.Buffer
	PrintList(&buffer, []runner.ListedTest{
		{FilePath: "tests/unnamed.yml"},
		{FilePath: "tests/named.yml", TestName: "named"},
	})

	output := buffer.String()
	if !strings.Contains(output, "tests/unnamed.yml\t(unnamed)") {
		t.Fatalf("list output missing fallback name: %s", output)
	}
	if !strings.Contains(output, "tests/named.yml\tnamed") {
		t.Fatalf("list output missing test name: %s", output)
	}
}

func TestDotsVerboseWritesFullPassingOutput(t *testing.T) {
	var buffer bytes.Buffer
	console, err := NewConsole(ConsoleOptions{Format: "dots", Verbose: true, Writer: &buffer})
	if err != nil {
		t.Fatalf("NewConsole() error = %v", err)
	}

	console.TestDone(samplePassResult())
	console.Summary(runner.RunResult{Passed: 1, Duration: time.Second})

	output := buffer.String()
	if !strings.Contains(output, `stdout: "pass-job"`) || !strings.Contains(output, "all good") {
		t.Fatalf("dots verbose output missing job logs: %s", output)
	}
}

func TestWriteFileReturnsPathContextOnError(t *testing.T) {
	report := NewJUnit()
	path := filepath.Join(t.TempDir(), "missing", "report.xml")

	err := report.WriteFile(path)
	if err == nil {
		t.Fatal("WriteFile() error = nil, want error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("WriteFile() error missing path context: %v", err)
	}
}

func TestReporterHelperErrorBranches(t *testing.T) {
	if _, err := NewConsole(ConsoleOptions{Format: "bad"}); err == nil {
		t.Fatal("NewConsole() error = nil, want unsupported format")
	}

	var buffer bytes.Buffer
	writeJSON(&buffer, make(chan int))
	if !strings.Contains(buffer.String(), "error") {
		t.Fatalf("writeJSON error output = %q", buffer.String())
	}

	buffer.Reset()
	writeJSONDebugBlock(&buffer, "Bad JSON", make(chan int))
	if buffer.Len() != 0 {
		t.Fatalf("bad debug JSON should be skipped: %q", buffer.String())
	}

	if formatDuration(-time.Second) != "0s" {
		t.Fatalf("negative duration should format as 0s")
	}
	message := failureMessage(runner.TestResult{Error: errors.New("boom")})
	if message != "boom" {
		t.Fatalf("failureMessage() = %q", message)
	}
}

func TestReporterStartMethods(t *testing.T) {
	var buffer bytes.Buffer
	dots, err := NewConsole(ConsoleOptions{Format: "dots", Writer: &buffer})
	if err != nil {
		t.Fatal(err)
	}
	dots.Start(2)

	jsonConsole, err := NewConsole(ConsoleOptions{Format: "json", Writer: &buffer})
	if err != nil {
		t.Fatal(err)
	}
	jsonConsole.Start(2)

	NewJUnit().Start(2)
	NewTAP().Start(2)
}

// TestTAPEscapesHashInDescriptionAndQuotesYAMLMessage guards against two TAP
// output bugs: an unescaped "#" in a test description would start an
// unintended TAP directive, and an unquoted "message: %s" line with ": ",
// quotes, or a carriage return would produce invalid YAML in the
// "---"/"..." diagnostic block. Failure messages embed raw job output, so
// these characters are realistic.
func TestTAPEscapesHashInDescriptionAndQuotesYAMLMessage(t *testing.T) {
	result := sampleFailResult()
	result.TestName = "release #123 check"
	result.Failures = []asserter.AssertResult{
		{Path: "assert.job.stdout", Expected: `say "hi": ok`, Actual: "line1\r\nline2"},
	}

	report := NewTAP()
	report.TestDone(result)
	report.Summary(runner.RunResult{Tests: []runner.TestResult{result}, Failed: 1})

	path := filepath.Join(t.TempDir(), "escaped.tap")
	if err := report.WriteFile(path); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	output := string(data)

	if !strings.Contains(output, `release \#123 check`) {
		t.Fatalf("expected # in description to be escaped, got: %s", output)
	}
	if !strings.Contains(output, `message: "`) {
		t.Fatalf("expected the message line to be YAML double-quoted, got: %s", output)
	}
	if strings.Contains(output, "\r") {
		t.Fatalf("expected no raw carriage return in output, got: %q", output)
	}
}

func TestTAPWriteFileReturnsPathContextOnError(t *testing.T) {
	report := NewTAP()
	path := filepath.Join(t.TempDir(), "missing", "report.tap")

	err := report.WriteFile(path)
	if err == nil {
		t.Fatal("WriteFile() error = nil, want error")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("WriteFile() error missing path context: %v", err)
	}
}

func sampleRunResult() runner.RunResult {
	return runner.RunResult{
		Passed:   1,
		Failed:   1,
		Duration: 3 * time.Second,
	}
}

func samplePassResult() runner.TestResult {
	return runner.TestResult{
		FilePath: filepath.Join("tests", "pass.yml"),
		TestName: "pass test",
		Passed:   true,
		Duration: time.Second,
		JobOutputs: map[string]executor.JobOutput{
			"pass-job": {
				Name:       "pass-job",
				Present:    true,
				ExitStatus: 0,
				Stdout:     "all good",
			},
		},
	}
}

func sampleFailResult() runner.TestResult {
	return runner.TestResult{
		FilePath: filepath.Join("tests", "fail.yml"),
		TestName: "release flow creates GitLab release",
		Passed:   false,
		Duration: 2 * time.Second,
		Failures: []asserter.AssertResult{
			{
				Path:     `assert.job."release:publish".exit-status`,
				Expected: "0",
				Actual:   "1",
			},
		},
		JobOutputs: map[string]executor.JobOutput{
			"release:publish": {
				Name:       "release:publish",
				Present:    true,
				ExitStatus: 1,
				Stdout:     "line one\nline two",
			},
		},
	}
}

func sampleFailResultWithXML() runner.TestResult {
	result := sampleFailResult()
	result.Failures = []asserter.AssertResult{
		{
			Path:     "assert.api",
			Expected: "<tag>&bad",
			Actual:   "actual",
		},
	}
	result.Error = errors.New("runner <error>")
	return result
}

func TestCondenseError(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no stdout section passes through unchanged",
			input: "run pipeline: exit status 1",
			want:  "run pipeline: exit status 1",
		},
		{
			name:  "stdout without stderr is stripped entirely",
			input: "run pipeline: exit status 1; stdout: WARN huge list of vars",
			want:  "run pipeline: exit status 1",
		},
		{
			name:  "stdout between prefix and stderr is removed",
			input: "run pipeline: exit status 1; stdout: WARN huge list; stderr: Invalid config!",
			want:  "run pipeline: exit status 1; stderr: Invalid config!",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := condenseError(errors.New(tc.input))
			if got != tc.want {
				t.Errorf("condenseError(%q)\n  got:  %q\n  want: %q", tc.input, got, tc.want)
			}
		})
	}
}
