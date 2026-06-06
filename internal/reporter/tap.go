package reporter

import (
	"fmt"
	"os"
	"strings"

	"github.com/pandasoft-zz/glut/internal/runner"
)

type tapReport struct {
	results runner.RunResult
}

func NewTAP() FileReport {
	return &tapReport{}
}

func (r *tapReport) Start(totalTests int) {
	_ = totalTests
}

func (r *tapReport) TestRetry(_ string, _ error) {}

func (r *tapReport) TestDone(result runner.TestResult) {
	r.results.Tests = append(r.results.Tests, result)
	if result.Passed {
		r.results.Passed++
		return
	}
	r.results.Failed++
}

func (r *tapReport) Summary(result runner.RunResult) {
	if len(result.Tests) == 0 && len(r.results.Tests) > 0 {
		result.Tests = append([]runner.TestResult(nil), r.results.Tests...)
	}
	r.results = result
}

func (r *tapReport) WriteFile(path string) error {
	var builder strings.Builder
	builder.WriteString("TAP version 14\n")
	fmt.Fprintf(&builder, "1..%d\n", len(r.results.Tests))

	for index, testResult := range r.results.Tests {
		status := "ok"
		if !testResult.Passed {
			status = "not ok"
		}

		fmt.Fprintf(&builder, "%s %d - %s: %s\n", status, index+1, testResult.FilePath, testDisplayName(testResult))
		if testResult.Passed {
			continue
		}

		message := failureMessage(testResult)
		if message == "" {
			message = "test failed"
		}
		message = strings.ReplaceAll(message, "\n", " ")
		builder.WriteString("  ---\n")
		fmt.Fprintf(&builder, "  message: %s\n", message)
		builder.WriteString("  ...\n")
	}

	if err := os.WriteFile(path, []byte(builder.String()), 0644); err != nil {
		return fmt.Errorf("write TAP report %s: %w", path, err)
	}
	return nil
}
