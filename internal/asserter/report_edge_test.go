package asserter

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
)

func TestAssertReportUnreadableFileFails(t *testing.T) {
	t.Parallel()
	a := &config.ReportAssert{Format: "junit", Tests: 1}
	results := assertReport("artifacts.report.xml", filepath.Join(t.TempDir(), "missing.xml"), a)
	if len(results) == 0 {
		t.Fatal("unreadable report must fail")
	}
	last := results[len(results)-1]
	if last.Passed || !strings.Contains(last.Path, ".report") {
		t.Fatalf("unexpected result %+v", last)
	}

	if got := assertReport("x", "y", nil); got != nil {
		t.Fatalf("nil assert must be a no-op, got %v", got)
	}
}

func TestAssertReportDataUnknownFormatFails(t *testing.T) {
	t.Parallel()
	a := &config.ReportAssert{Format: "sarif"}
	results := assertReportData("artifacts.out", []byte("{}"), a)
	if len(results) != 1 || results[0].Passed {
		t.Fatalf("unknown format must produce one failure, got %+v", results)
	}
}

func TestAssertReportDataDotenvWithoutKeysFails(t *testing.T) {
	t.Parallel()
	a := &config.ReportAssert{Format: "dotenv"}
	results := assertReportData("artifacts.env", []byte("A=1\n"), a)
	failed := failedResults(results)
	if len(failed) == 0 {
		t.Fatal("a key-less dotenv assertion must not pass vacuously")
	}
}

// TestReportFieldErrorsRejectsForeignFields also exercises every branch of
// reportFieldsSet: all assertion fields are populated on a coverage report,
// so everything except line-rate/branch-rate must be rejected.
func TestReportFieldErrorsRejectsForeignFields(t *testing.T) {
	t.Parallel()
	a := &config.ReportAssert{
		Format:     "coverage",
		Tests:      1,
		Failures:   0,
		Errors:     0,
		Skipped:    0,
		Suites:     []config.SuiteAssert{{}},
		LineRate:   0.9,
		BranchRate: 0.8,
		Keys:       map[string]any{"K": "v"},
		Critical:   0,
		High:       0,
		Medium:     0,
		Low:        0,
	}
	results := reportFieldErrors("artifacts.report", a)
	rejected := map[string]bool{}
	for _, r := range results {
		if r.Passed {
			t.Fatalf("field error must fail, got %+v", r)
		}
		rejected[r.Path[strings.LastIndex(r.Path, ".")+1:]] = true
	}
	for _, field := range []string{"tests", "failures", "errors", "skipped", "suites", "keys", "critical", "high", "medium", "low"} {
		if !rejected[field] {
			t.Fatalf("field %q set on a coverage assert must be rejected; got %v", field, rejected)
		}
	}
	for _, field := range []string{"line-rate", "branch-rate"} {
		if rejected[field] {
			t.Fatalf("field %q is valid for coverage and must not be rejected", field)
		}
	}
}

func TestAssertGitLabSecurityCountsBySeverity(t *testing.T) {
	t.Parallel()
	report := `{"vulnerabilities":[
		{"severity":"Critical"},
		{"severity":"high"},
		{"severity":"HIGH"},
		{"severity":"unknown"}
	]}`
	a := &config.ReportAssert{Format: "gitlab-security", Critical: 1, High: 2, Medium: 0, Low: 0}
	results := assertGitLabSecurity("r", []byte(report), a)
	if len(results) != 4 {
		t.Fatalf("results = %+v, want one per asserted severity", results)
	}
	for _, r := range results {
		if !r.Passed {
			t.Fatalf("severity count mismatch: %+v", r)
		}
	}
}

func TestAssertGitLabSecurityRejectsNonReports(t *testing.T) {
	t.Parallel()
	a := &config.ReportAssert{Format: "gitlab-security", Critical: 0}

	if results := assertGitLabSecurity("r", []byte("not json"), a); len(results) != 1 || results[0].Passed {
		t.Fatalf("invalid JSON must fail, got %+v", results)
	}
	// A JSON object without a vulnerabilities array is not a security report —
	// it must not false-pass a "0 criticals" assertion.
	if results := assertGitLabSecurity("r", []byte(`{"version":"15.0.0"}`), a); len(results) != 1 || results[0].Passed {
		t.Fatalf("missing vulnerabilities array must fail, got %+v", results)
	}
}
