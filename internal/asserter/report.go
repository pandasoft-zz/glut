package asserter

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pandasoft-zz/glut/internal/config"
)

func assertReport(basePath, fullPath string, a *config.ReportAssert) []AssertResult {
	if a == nil {
		return nil
	}
	// Surface fields that do not apply to the chosen format instead of silently
	// ignoring them (which would let a misconfigured assertion pass vacuously).
	fieldErrs := reportFieldErrors(basePath+".report", a)

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return append(fieldErrs, failResult(basePath+".report", "read file", err))
	}

	// A dotenv assertion with no keys passes vacuously against ANY readable file
	// (parseDotenv never fails), so require at least one key. The XML/JSON formats
	// do not need this guard: parsing itself is a meaningful assertion (the file
	// must be a valid JUnit/Cobertura document or a security report whose
	// vulnerabilities array is present), so a field-less assertion there is a
	// deliberate "is this a valid <format> report" check.
	if a.Format == "dotenv" && len(a.Keys) == 0 {
		fieldErrs = append(fieldErrs, failResult(basePath+".report",
			"at least one key assertion for dotenv", "no keys set"))
	}

	var results []AssertResult
	switch a.Format {
	case "junit":
		results = assertJUnit(basePath+".report", data, a)
	case "dotenv":
		results = assertDotenv(basePath+".report", data, a)
	case "coverage":
		results = assertCoverage(basePath+".report", data, a)
	case "gitlab-security":
		results = assertGitLabSecurity(basePath+".report", data, a)
	default:
		results = []AssertResult{failResult(basePath+".report.format",
			"known format (junit|dotenv|coverage|gitlab-security)", a.Format)}
	}
	return append(fieldErrs, results...)
}

// reportAllowedFields maps each known report format to the assertion fields it
// supports; reportFieldErrors uses it to reject fields set for the wrong format.
var reportAllowedFields = map[string]map[string]bool{
	"junit":           {"tests": true, "failures": true, "errors": true, "skipped": true, "suites": true},
	"coverage":        {"line-rate": true, "branch-rate": true},
	"dotenv":          {"keys": true},
	"gitlab-security": {"critical": true, "high": true, "medium": true, "low": true},
}

// reportFieldsSet returns the set of assertion field names populated on a.
func reportFieldsSet(a *config.ReportAssert) map[string]bool {
	set := map[string]bool{}
	if a.Tests != nil {
		set["tests"] = true
	}
	if a.Failures != nil {
		set["failures"] = true
	}
	if a.Errors != nil {
		set["errors"] = true
	}
	if a.Skipped != nil {
		set["skipped"] = true
	}
	if len(a.Suites) > 0 {
		set["suites"] = true
	}
	if a.LineRate != nil {
		set["line-rate"] = true
	}
	if a.BranchRate != nil {
		set["branch-rate"] = true
	}
	if a.Keys != nil {
		set["keys"] = true
	}
	if a.Critical != nil {
		set["critical"] = true
	}
	if a.High != nil {
		set["high"] = true
	}
	if a.Medium != nil {
		set["medium"] = true
	}
	if a.Low != nil {
		set["low"] = true
	}
	return set
}

// reportFieldErrors returns a failing result for every assertion field that is
// set but not applicable to a.Format. Unknown formats are skipped (the format
// error itself is reported by assertReport).
func reportFieldErrors(basePath string, a *config.ReportAssert) []AssertResult {
	allowed, known := reportAllowedFields[a.Format]
	if !known {
		return nil
	}

	var results []AssertResult
	for _, name := range keysSorted(reportFieldsSet(a)) {
		if !allowed[name] {
			results = append(results, failResult(basePath+"."+name,
				"field not valid for format "+a.Format, "field is set"))
		}
	}
	return results
}

// ─── JUnit ───────────────────────────────────────────────────────────────────

type junitTestSuites struct {
	XMLName xml.Name         `xml:"testsuites"`
	Suites  []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	XMLName  xml.Name `xml:"testsuite"`
	Name     string   `xml:"name,attr"`
	Tests    *int     `xml:"tests,attr"`
	Failures *int     `xml:"failures,attr"`
	Errors   *int     `xml:"errors,attr"`
	Skipped  *int     `xml:"skipped,attr"`
	// Suites captures nested <testsuite> elements (suite-of-suites layouts).
	Suites    []junitTestSuite `xml:"testsuite"`
	TestCases []junitTestCase  `xml:"testcase"`
}

type junitTestCase struct {
	Failure []junitElement `xml:"failure"`
	Error   []junitElement `xml:"error"`
	Skipped []junitElement `xml:"skipped"`
}

type junitElement struct{}

type suiteCounts struct {
	name     string
	tests    int
	failures int
	errors   int
	skipped  int
}

// parseJUnit returns the aggregate counts (summed over the top-level suites) and
// a flat list of every suite node (at any nesting depth) for per-suite lookups.
func parseJUnit(data []byte) (suiteCounts, []suiteCounts, error) {
	var doc junitTestSuites
	_ = xml.Unmarshal(data, &doc)
	roots := doc.Suites
	if len(roots) == 0 {
		var single junitTestSuite
		if err := xml.Unmarshal(data, &single); err != nil {
			return suiteCounts{}, nil, fmt.Errorf("parse JUnit XML: %w", err)
		}
		roots = []junitTestSuite{single}
	}

	var all []suiteCounts
	var total suiteCounts
	for i := range roots {
		c := flattenSuite(roots[i], &all)
		total.tests += c.tests
		total.failures += c.failures
		total.errors += c.errors
		total.skipped += c.skipped
	}
	return total, all, nil
}

// flattenSuite computes the effective counts for s and appends s plus all its
// descendants to all. Counts are taken from the suite-level count attributes
// when present (authoritative for real-world producers), otherwise derived by
// counting child <testcase> elements and recursing into nested suites.
func flattenSuite(s junitTestSuite, all *[]suiteCounts) suiteCounts {
	c := suiteCounts{name: s.Name}
	for _, tc := range s.TestCases {
		c.tests++
		c.failures += len(tc.Failure)
		c.errors += len(tc.Error)
		c.skipped += len(tc.Skipped)
	}
	for i := range s.Suites {
		child := flattenSuite(s.Suites[i], all)
		c.tests += child.tests
		c.failures += child.failures
		c.errors += child.errors
		c.skipped += child.skipped
	}
	if s.Tests != nil {
		c.tests = *s.Tests
	}
	if s.Failures != nil {
		c.failures = *s.Failures
	}
	if s.Errors != nil {
		c.errors = *s.Errors
	}
	if s.Skipped != nil {
		c.skipped = *s.Skipped
	}
	*all = append(*all, c)
	return c
}

func assertJUnit(basePath string, data []byte, a *config.ReportAssert) []AssertResult {
	total, suites, err := parseJUnit(data)
	if err != nil {
		return []AssertResult{failResult(basePath, "valid JUnit XML", err)}
	}

	var results []AssertResult
	if a.Tests != nil {
		results = append(results, resultFromState(basePath+".tests", matchValue(a.Tests, total.tests)))
	}
	if a.Failures != nil {
		results = append(results, resultFromState(basePath+".failures", matchValue(a.Failures, total.failures)))
	}
	if a.Errors != nil {
		results = append(results, resultFromState(basePath+".errors", matchValue(a.Errors, total.errors)))
	}
	if a.Skipped != nil {
		results = append(results, resultFromState(basePath+".skipped", matchValue(a.Skipped, total.skipped)))
	}

	for i, sa := range a.Suites {
		suitePath := fmt.Sprintf("%s.suites[%d]", basePath, i)
		var found *suiteCounts
		for j := range suites {
			if suites[j].name == sa.Name {
				found = &suites[j]
				break
			}
		}
		if found == nil {
			results = append(results, failResult(suitePath+".name", sa.Name, "suite not found"))
			continue
		}
		if sa.Tests != nil {
			results = append(results, resultFromState(suitePath+".tests", matchValue(sa.Tests, found.tests)))
		}
		if sa.Failures != nil {
			results = append(results, resultFromState(suitePath+".failures", matchValue(sa.Failures, found.failures)))
		}
		if sa.Errors != nil {
			results = append(results, resultFromState(suitePath+".errors", matchValue(sa.Errors, found.errors)))
		}
		if sa.Skipped != nil {
			results = append(results, resultFromState(suitePath+".skipped", matchValue(sa.Skipped, found.skipped)))
		}
	}
	return results
}

// ─── dotenv ──────────────────────────────────────────────────────────────────

func parseDotenv(data []byte) map[string]string {
	result := make(map[string]string)
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// Strip optional surrounding quotes.
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') ||
			(val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		result[key] = val
	}
	return result
}

func assertDotenv(basePath string, data []byte, a *config.ReportAssert) []AssertResult {
	parsed := parseDotenv(data)
	var results []AssertResult
	for _, key := range keysSorted(a.Keys) {
		expected := a.Keys[key]
		keyPath := basePath + ".keys." + key
		_, exists := parsed[key]

		if expected == nil {
			results = append(results, resultFromBool(keyPath, exists, "key to exist", exists))
			continue
		}
		if m, ok := toStringMap(expected); ok {
			if existsVal, hasExists := m["exists"]; hasExists {
				wantExists, isBool := existsVal.(bool)
				if !isBool {
					results = append(results, failResult(keyPath+".exists", "boolean", existsVal))
					continue
				}
				results = append(results, resultFromBool(keyPath+".exists", exists == wantExists, wantExists, exists))
				// Honour any sibling value matcher, e.g. {exists: true, equal: X}.
				if rest := mapWithout(m, "exists"); len(rest) > 0 && wantExists && exists {
					results = append(results, resultFromState(keyPath, matchValue(rest, parsed[key])))
				}
				continue
			}
		}
		if !exists {
			results = append(results, failResult(keyPath, expected, "key not found in dotenv"))
			continue
		}
		results = append(results, resultFromState(keyPath, matchValue(dotenvExpected(expected), parsed[key])))
	}
	return results
}

// dotenvExpected coerces a bare scalar expectation to its string form because
// dotenv values are always strings. Without this, `KEY: 3` / `KEY: true`
// (decoded by YAML as int/bool) would be compared against the string "3" /
// "true" and fail on a type mismatch even though the value is correct. Matcher
// maps and string expectations pass through unchanged.
func dotenvExpected(expected any) any {
	switch expected.(type) {
	case bool, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprintf("%v", expected)
	}
	return expected
}

// mapWithout returns a copy of m with the given key removed.
func mapWithout(m map[string]any, key string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if k != key {
			out[k] = v
		}
	}
	return out
}

// ─── coverage (Cobertura) ────────────────────────────────────────────────────

type coberturaDoc struct {
	XMLName    xml.Name `xml:"coverage"`
	LineRate   string   `xml:"line-rate,attr"`
	BranchRate string   `xml:"branch-rate,attr"`
}

func assertCoverage(basePath string, data []byte, a *config.ReportAssert) []AssertResult {
	var doc coberturaDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return []AssertResult{failResult(basePath, "valid Cobertura XML", err)}
	}

	// Strict parse: reject trailing garbage and comma-decimals rather than
	// silently truncating them (fmt.Sscanf("%f") would accept "0.85abc").
	parseRate := func(s string) (float64, bool) {
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return f, err == nil
	}

	var results []AssertResult
	if a.LineRate != nil {
		if rate, ok := parseRate(doc.LineRate); ok {
			results = append(results, resultFromState(basePath+".line-rate", matchValue(a.LineRate, rate)))
		} else {
			results = append(results, failResult(basePath+".line-rate", a.LineRate, "line-rate attribute missing or invalid"))
		}
	}
	if a.BranchRate != nil {
		if rate, ok := parseRate(doc.BranchRate); ok {
			results = append(results, resultFromState(basePath+".branch-rate", matchValue(a.BranchRate, rate)))
		} else {
			results = append(results, failResult(basePath+".branch-rate", a.BranchRate, "branch-rate attribute missing or invalid"))
		}
	}
	return results
}

// ─── GitLab security reports ─────────────────────────────────────────────────
// Covers: sast, dast, dependency_scanning, container_scanning, secret_detection.
// All share the same top-level "vulnerabilities" array with a "severity" field.

type gitlabSecurityReport struct {
	// Pointer so a missing array is distinguishable from an empty one ([]).
	Vulnerabilities *[]struct {
		Severity string `json:"severity"`
	} `json:"vulnerabilities"`
}

func assertGitLabSecurity(basePath string, data []byte, a *config.ReportAssert) []AssertResult {
	var report gitlabSecurityReport
	if err := json.Unmarshal(data, &report); err != nil {
		return []AssertResult{failResult(basePath, "valid GitLab security JSON", err)}
	}
	if report.Vulnerabilities == nil {
		// A clean scan emits an empty array; a missing array means the file is
		// not a security report (or was never produced), so do not false-pass.
		return []AssertResult{failResult(basePath, "vulnerabilities array present", "missing from report")}
	}

	counts := map[string]int{
		"critical": 0,
		"high":     0,
		"medium":   0,
		"low":      0,
	}
	for _, v := range *report.Vulnerabilities {
		sev := strings.ToLower(v.Severity)
		if _, ok := counts[sev]; ok {
			counts[sev]++
		}
	}

	var results []AssertResult
	if a.Critical != nil {
		results = append(results, resultFromState(basePath+".critical", matchValue(a.Critical, counts["critical"])))
	}
	if a.High != nil {
		results = append(results, resultFromState(basePath+".high", matchValue(a.High, counts["high"])))
	}
	if a.Medium != nil {
		results = append(results, resultFromState(basePath+".medium", matchValue(a.Medium, counts["medium"])))
	}
	if a.Low != nil {
		results = append(results, resultFromState(basePath+".low", matchValue(a.Low, counts["low"])))
	}
	return results
}
