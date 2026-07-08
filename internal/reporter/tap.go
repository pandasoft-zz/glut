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

		description := tapEscapeDescription(fmt.Sprintf("%s: %s", testResult.FilePath, testDisplayName(testResult)))
		fmt.Fprintf(&builder, "%s %d - %s\n", status, index+1, description)
		if testResult.Passed {
			continue
		}

		message := failureMessage(testResult)
		if message == "" {
			message = "test failed"
		}
		builder.WriteString("  ---\n")
		fmt.Fprintf(&builder, "  message: %s\n", yamlQuoteString(message))
		builder.WriteString("  ...\n")
	}

	if err := os.WriteFile(path, []byte(builder.String()), 0644); err != nil {
		return fmt.Errorf("write TAP report %s: %w", path, err)
	}
	return nil
}

// tapEscapeDescription makes a string safe to place after "ok N - " on a TAP
// line: an unescaped "#" would start a directive (e.g. "# SKIP ..."), and a
// raw newline or carriage return would break the single-line TAP format.
func tapEscapeDescription(description string) string {
	description = strings.ReplaceAll(description, "#", "\\#")
	description = strings.ReplaceAll(description, "\r\n", " ")
	description = strings.ReplaceAll(description, "\n", " ")
	description = strings.ReplaceAll(description, "\r", " ")
	return description
}

// yamlQuoteString renders s as a double-quoted YAML scalar, so a failure
// message containing ": ", quotes, or a control character still produces valid
// YAML inside the TAP "---"/"..." diagnostic block. Raw job output routinely
// contains control characters (e.g. ANSI escape 0x1B); any C0 control or DEL
// that is not \n/\r/\t is emitted as a \xNN escape, which strict YAML parsers
// accept where a literal control byte would be rejected.
func yamlQuoteString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\x%02X`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
