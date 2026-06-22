package asserter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
)

func anyFailed(results []AssertResult) bool {
	for _, r := range results {
		if !r.Passed {
			return true
		}
	}
	return false
}

func mustWriteTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// ─── JUnit ───────────────────────────────────────────────────────────────────

// Finding #3: real JUnit producers report counts via attributes, not only via
// child elements. The parser must honour <testsuite tests=".." failures="..">.
func TestAssertJUnitHonoursCountAttributes(t *testing.T) {
	data := []byte(`<?xml version="1.0"?>
<testsuite name="AttrSuite" tests="10" failures="2" errors="1" skipped="3">
</testsuite>`)
	a := &config.ReportAssert{
		Format:   "junit",
		Tests:    10,
		Failures: 2,
		Errors:   1,
		Skipped:  3,
	}
	results := assertJUnit("art.report", data, a)
	if anyFailed(results) {
		t.Fatalf("expected attribute-based counts to pass, got %+v", results)
	}
}

// Finding #3: nested <testsuite> elements (suite-of-suites) must be traversed.
func TestAssertJUnitNestedSuites(t *testing.T) {
	data := []byte(`<?xml version="1.0"?>
<testsuites>
  <testsuite name="Outer">
    <testsuite name="Inner">
      <testcase name="a"/>
      <testcase name="b"><failure/></testcase>
    </testsuite>
  </testsuite>
</testsuites>`)
	a := &config.ReportAssert{
		Format:   "junit",
		Tests:    2,
		Failures: 1,
		Suites: []config.SuiteAssert{
			{Name: "Inner", Tests: 2, Failures: 1},
		},
	}
	results := assertJUnit("art.report", data, a)
	if anyFailed(results) {
		t.Fatalf("expected nested-suite counts to pass, got %+v", results)
	}
}

// Regression: element-based counting (no count attributes) must keep working,
// since the committed fixtures rely on it.
func TestAssertJUnitElementFallback(t *testing.T) {
	data := []byte(`<?xml version="1.0"?>
<testsuites>
  <testsuite name="UnitSuite">
    <testcase name="t1"/>
    <testcase name="t2"/>
    <testcase name="t3"><skipped/></testcase>
  </testsuite>
  <testsuite name="IntegrationSuite">
    <testcase name="t4"/>
  </testsuite>
</testsuites>`)
	a := &config.ReportAssert{
		Format:  "junit",
		Tests:   4,
		Skipped: 1,
		Suites: []config.SuiteAssert{
			{Name: "UnitSuite", Tests: 3},
			{Name: "IntegrationSuite", Tests: 1},
		},
	}
	results := assertJUnit("art.report", data, a)
	if anyFailed(results) {
		t.Fatalf("expected element-based counts to pass, got %+v", results)
	}
}

// ─── dotenv ──────────────────────────────────────────────────────────────────

// Finding #2a: {exists: true} combined with a value matcher must still evaluate
// the value matcher, not silently short-circuit on presence.
func TestAssertDotenvExistsWithValueMatcher(t *testing.T) {
	data := []byte("APP_VERSION=1.2.3\n")

	pass := assertDotenv("art.report", data, &config.ReportAssert{
		Format: "dotenv",
		Keys:   map[string]any{"APP_VERSION": map[string]any{"exists": true, "equal": "1.2.3"}},
	})
	if anyFailed(pass) {
		t.Fatalf("matching value with exists:true should pass, got %+v", pass)
	}

	fail := assertDotenv("art.report", data, &config.ReportAssert{
		Format: "dotenv",
		Keys:   map[string]any{"APP_VERSION": map[string]any{"exists": true, "equal": "9.9.9"}},
	})
	if !anyFailed(fail) {
		t.Fatalf("wrong value with exists:true must fail, got %+v", fail)
	}
}

// Finding #2b: a non-bool `exists` value must surface as a clear failure rather
// than being silently coerced to false (which inverts the assertion).
func TestAssertDotenvNonBoolExists(t *testing.T) {
	data := []byte("OTHER=1\n") // KEY is absent

	results := assertDotenv("art.report", data, &config.ReportAssert{
		Format: "dotenv",
		Keys:   map[string]any{"KEY": map[string]any{"exists": "true"}},
	})
	if !anyFailed(results) {
		t.Fatalf("non-bool exists value must not silently pass for an absent key, got %+v", results)
	}
}

// ─── coverage ────────────────────────────────────────────────────────────────

// Finding (coverage): rate parsing must be strict — trailing garbage and
// comma-decimals must be rejected, not silently truncated.
func TestAssertCoverageStrictRateParsing(t *testing.T) {
	ok := assertCoverage("art.report", []byte(`<coverage line-rate="0.85" branch-rate="0.72"/>`),
		&config.ReportAssert{Format: "coverage", LineRate: map[string]any{"ge": 0.80}})
	if anyFailed(ok) {
		t.Fatalf("valid rate should pass, got %+v", ok)
	}

	garbage := assertCoverage("art.report", []byte(`<coverage line-rate="0.85abc"/>`),
		&config.ReportAssert{Format: "coverage", LineRate: map[string]any{"le": 1.0}})
	if !anyFailed(garbage) {
		t.Fatalf("trailing-garbage rate must fail, got %+v", garbage)
	}

	comma := assertCoverage("art.report", []byte(`<coverage line-rate="0,85"/>`),
		&config.ReportAssert{Format: "coverage", LineRate: map[string]any{"ge": 0.80}})
	if !anyFailed(comma) {
		t.Fatalf("comma-decimal rate must fail, got %+v", comma)
	}
}

// ─── gitlab-security ─────────────────────────────────────────────────────────

// Finding #8: a report missing the `vulnerabilities` array must not be treated
// as a clean scan (all-zero counts that false-pass).
func TestAssertGitLabSecurityMissingArray(t *testing.T) {
	empty := assertGitLabSecurity("art.report", []byte(`{"version":"15.0.0","vulnerabilities":[]}`),
		&config.ReportAssert{Format: "gitlab-security", Critical: 0})
	if anyFailed(empty) {
		t.Fatalf("explicit empty vulnerabilities array should pass critical:0, got %+v", empty)
	}

	missing := assertGitLabSecurity("art.report", []byte(`{"version":"15.0.0"}`),
		&config.ReportAssert{Format: "gitlab-security", Critical: 0})
	if !anyFailed(missing) {
		t.Fatalf("missing vulnerabilities array must fail, got %+v", missing)
	}
}

// ─── format/field validation ─────────────────────────────────────────────────

// Finding #4: fields that do not apply to the chosen format must be reported as
// an error instead of being silently ignored (vacuous pass).
func TestAssertReportRejectsFieldsForWrongFormat(t *testing.T) {
	path := mustWriteTemp(t, "cov.xml", `<coverage line-rate="0.9" branch-rate="0.9"/>`)
	a := &config.ReportAssert{
		Format:   "coverage",
		LineRate: map[string]any{"ge": 0.8},
		Tests:    5, // not valid for coverage
	}
	results := assertReport("art", path, a)
	if !anyFailed(results) {
		t.Fatalf("setting junit-only field on coverage format must fail, got %+v", results)
	}

	jpath := mustWriteTemp(t, "junit.xml", `<testsuite name="S"><testcase/></testsuite>`)
	ja := &config.ReportAssert{
		Format:   "junit",
		Tests:    1,
		LineRate: map[string]any{"ge": 0.8}, // not valid for junit
	}
	jresults := assertReport("art", jpath, ja)
	if !anyFailed(jresults) {
		t.Fatalf("setting coverage-only field on junit format must fail, got %+v", jresults)
	}
}

// Regression: dotenv values are strings, but a bare scalar expectation decoded
// by YAML as int/bool/float must still match (e.g. `KEY: true`, `KEY: 5`) rather
// than failing on a type mismatch.
func TestAssertDotenvScalarCoercion(t *testing.T) {
	data := []byte("DEPLOY_OK=true\nRETRIES=5\nRATIO=0.5\n")

	pass := assertDotenv("art.report", data, &config.ReportAssert{
		Format: "dotenv",
		Keys:   map[string]any{"DEPLOY_OK": true, "RETRIES": 5, "RATIO": 0.5},
	})
	if anyFailed(pass) {
		t.Fatalf("unquoted scalar dotenv values should match their string form, got %+v", pass)
	}

	fail := assertDotenv("art.report", data, &config.ReportAssert{
		Format: "dotenv",
		Keys:   map[string]any{"RETRIES": 6},
	})
	if !anyFailed(fail) {
		t.Fatalf("wrong scalar value must still fail, got %+v", fail)
	}
}

// A dotenv assertion with no keys passes vacuously against any readable file
// (parseDotenv never fails), so it must be rejected. The XML/JSON formats, by
// contrast, validate structure during parsing, so a field-less assertion there
// is a legitimate "is this a valid <format> report" check and must still pass.
func TestAssertReportNoFieldsFails(t *testing.T) {
	dotenvPath := mustWriteTemp(t, "x.env", "ANYTHING=1\n")
	if !anyFailed(assertReport("art", dotenvPath, &config.ReportAssert{Format: "dotenv"})) {
		t.Fatalf("dotenv report with no keys must fail")
	}

	// Valid JUnit / Cobertura with no specific field assertions is a structural
	// validity check and must pass.
	junitPath := mustWriteTemp(t, "j.xml", `<testsuite name="S"><testcase/></testsuite>`)
	if anyFailed(assertReport("art", junitPath, &config.ReportAssert{Format: "junit"})) {
		t.Fatalf("valid junit with no fields should pass as a validity check")
	}
	covPath := mustWriteTemp(t, "c.xml", `<coverage line-rate="0.9"/>`)
	if anyFailed(assertReport("art", covPath, &config.ReportAssert{Format: "coverage"})) {
		t.Fatalf("valid coverage with no fields should pass as a validity check")
	}

	// But an invalid document for the format must still fail to parse.
	badCov := mustWriteTemp(t, "bad.xml", `<notcoverage/>`)
	if !anyFailed(assertReport("art", badCov, &config.ReportAssert{Format: "coverage"})) {
		t.Fatalf("non-Cobertura XML must fail coverage parsing")
	}
}
