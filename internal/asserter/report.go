package asserter

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"strings"

	"github.com/pandasoft-zz/glut/internal/config"
)

func assertReport(basePath, fullPath string, a *config.ReportAssert) []AssertResult {
	if a == nil {
		return nil
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return []AssertResult{failResult(basePath + ".report", "read file", err)}
	}
	switch a.Format {
	case "junit":
		return assertJUnit(basePath+".report", data, a)
	case "dotenv":
		return assertDotenv(basePath+".report", data, a)
	case "coverage":
		return assertCoverage(basePath+".report", data, a)
	case "gitlab-security":
		return assertGitLabSecurity(basePath+".report", data, a)
	default:
		return []AssertResult{failResult(basePath+".report.format",
			"known format (junit|dotenv|coverage|gitlab-security)", a.Format)}
	}
}

// ─── JUnit ───────────────────────────────────────────────────────────────────

type junitTestSuites struct {
	XMLName xml.Name         `xml:"testsuites"`
	Suites  []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	XMLName   xml.Name        `xml:"testsuite"`
	Name      string          `xml:"name,attr"`
	TestCases []junitTestCase `xml:"testcase"`
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

func parseJUnit(data []byte) ([]suiteCounts, error) {
	var doc junitTestSuites
	_ = xml.Unmarshal(data, &doc)
	suites := doc.Suites
	if len(suites) == 0 {
		var single junitTestSuite
		if err := xml.Unmarshal(data, &single); err != nil {
			return nil, fmt.Errorf("parse JUnit XML: %w", err)
		}
		suites = []junitTestSuite{single}
	}
	counts := make([]suiteCounts, 0, len(suites))
	for _, s := range suites {
		c := suiteCounts{name: s.Name}
		for _, tc := range s.TestCases {
			c.tests++
			c.failures += len(tc.Failure)
			c.errors += len(tc.Error)
			c.skipped += len(tc.Skipped)
		}
		counts = append(counts, c)
	}
	return counts, nil
}

func assertJUnit(basePath string, data []byte, a *config.ReportAssert) []AssertResult {
	suites, err := parseJUnit(data)
	if err != nil {
		return []AssertResult{failResult(basePath, "valid JUnit XML", err)}
	}

	// Aggregate totals across all suites.
	var total suiteCounts
	for _, s := range suites {
		total.tests += s.tests
		total.failures += s.failures
		total.errors += s.errors
		total.skipped += s.skipped
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
				wantExists, _ := existsVal.(bool)
				results = append(results, resultFromBool(keyPath+".exists", exists == wantExists, wantExists, exists))
				continue
			}
		}
		if !exists {
			results = append(results, failResult(keyPath, expected, "key not found in dotenv"))
			continue
		}
		results = append(results, resultFromState(keyPath, matchValue(expected, parsed[key])))
	}
	return results
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

	parseRate := func(s string) (float64, bool) {
		var f float64
		_, err := fmt.Sscanf(s, "%f", &f)
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
	Vulnerabilities []struct {
		Severity string `json:"severity"`
	} `json:"vulnerabilities"`
}

func assertGitLabSecurity(basePath string, data []byte, a *config.ReportAssert) []AssertResult {
	var report gitlabSecurityReport
	if err := json.Unmarshal(data, &report); err != nil {
		return []AssertResult{failResult(basePath, "valid GitLab security JSON", err)}
	}

	counts := map[string]int{
		"critical": 0,
		"high":     0,
		"medium":   0,
		"low":      0,
	}
	for _, v := range report.Vulnerabilities {
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
