package reporter

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/pandasoft-zz/glut/internal/runner"
)

type FileReport interface {
	runner.ProgressSink
	WriteFile(path string) error
}

type junitReport struct {
	results runner.RunResult
}

type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Errors   int          `xml:"errors,attr"`
	Time     string       `xml:"time,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Errors   int             `xml:"errors,attr"`
	Time     string          `xml:"time,attr"`
	Cases    []junitTestCase `xml:"testcase"`
}

type junitTestCase struct {
	ClassName string         `xml:"classname,attr,omitempty"`
	Name      string         `xml:"name,attr"`
	File      string         `xml:"file,attr,omitempty"`
	Time      string         `xml:"time,attr"`
	Failures  []junitFailure `xml:"failure,omitempty"`
	Error     *junitError    `xml:"error,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

type junitError struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

func NewJUnit() FileReport {
	return &junitReport{}
}

func (r *junitReport) Start(totalTests int) {
	_ = totalTests
}

func (r *junitReport) TestRetry(_ string, _ error) {}

func (r *junitReport) TestDone(result runner.TestResult) {
	r.results.Tests = append(r.results.Tests, result)
	if result.Passed {
		r.results.Passed++
		return
	}
	r.results.Failed++
}

func (r *junitReport) Summary(result runner.RunResult) {
	if len(result.Tests) == 0 && len(r.results.Tests) > 0 {
		result.Tests = append([]runner.TestResult(nil), r.results.Tests...)
	}
	r.results = result
}

func (r *junitReport) WriteFile(path string) error {
	report := buildJUnitSuites(r.results)
	data, err := xml.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JUnit report %s: %w", path, err)
	}

	payload := append([]byte(xml.Header), data...)
	payload = append(payload, '\n')
	if err := os.WriteFile(path, payload, 0644); err != nil {
		return fmt.Errorf("write JUnit report %s: %w", path, err)
	}
	return nil
}

func buildJUnitSuites(result runner.RunResult) junitSuites {
	grouped := make(map[string][]runner.TestResult)
	for _, testResult := range result.Tests {
		dir := filepath.ToSlash(filepath.Dir(testResult.FilePath))
		if dir == "." || dir == "" {
			dir = "."
		}
		grouped[dir] = append(grouped[dir], testResult)
	}

	names := make([]string, 0, len(grouped))
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)

	report := junitSuites{
		Tests: len(result.Tests),
		Time:  junitSeconds(result.Duration),
	}

	for _, name := range names {
		suite := junitSuite{Name: name}
		var suiteDuration time.Duration
		for _, testResult := range grouped[name] {
			testCase := junitTestCase{
				ClassName: name,
				Name:      testDisplayName(testResult),
				File:      filepath.ToSlash(testResult.FilePath),
				Time:      junitSeconds(testResult.Duration),
			}
			suiteDuration += testResult.Duration

			for _, failure := range testResult.Failures {
				testCase.Failures = append(testCase.Failures, junitFailure{
					Message: failure.Path,
					Text:    fmt.Sprintf("expected: %s\nactual: %s", failure.Expected, failure.Actual),
				})
			}
			// Count failed testcases, not failure elements: a testcase with
			// several failed assertions is still one failed test, so
			// tests="N" and failures="M" stay consistent for GitLab's test UI.
			if len(testResult.Failures) > 0 {
				suite.Failures++
			}

			if testResult.Error != nil {
				testCase.Error = &junitError{
					Message: testResult.Error.Error(),
					Text:    testResult.Error.Error(),
				}
				suite.Errors++
				report.Errors++
			}

			suite.Tests++
			suite.Cases = append(suite.Cases, testCase)
		}
		suite.Time = junitSeconds(suiteDuration)
		report.Failures += suite.Failures
		report.Suites = append(report.Suites, suite)
	}

	return report
}

func junitSeconds(d time.Duration) string {
	return fmt.Sprintf("%.3f", d.Seconds())
}
