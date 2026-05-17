package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pandasoft-zz/glut/internal/parser"
)

func TestBuildLintReportSeparatesSchemaAndSemanticIssues(t *testing.T) {
	path := writeTempTest(t, `
test_job:
  script: echo ok
---
.glut:
  name: "bad"
  setup:
    pipeline_source: "manual"
    merge_request_event_also: true
`)

	report := buildLintReport([]string{path})
	if !report.HasErrors {
		t.Fatal("expected lint errors")
	}
	if len(report.Files) != 1 {
		t.Fatalf("files len = %d", len(report.Files))
	}

	var hasSchema bool
	var hasSemantic bool
	for _, issue := range report.Files[0].Issues {
		if issue.Category == "schema" && issue.Path == ".glut.setup.pipeline_source" {
			hasSchema = true
		}
		if issue.Category == "semantic" {
			hasSemantic = true
		}
	}
	if !hasSchema || !hasSemantic {
		t.Fatalf("schema = %v, semantic = %v, issues = %#v", hasSchema, hasSemantic, report.Files[0].Issues)
	}
}

func TestPrintLintReportJSON(t *testing.T) {
	path := writeTempTest(t, `
test_job:
  script: echo ok
---
.glut:
  name: "bad"
  setup:
    pipeline_source: "manual"
`)
	report := buildLintReport([]string{path})

	var out bytes.Buffer
	if err := printLintReport(&out, &bytes.Buffer{}, report, "json"); err != nil {
		t.Fatal(err)
	}

	var decoded lintReport
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out.String())
	}
	if !decoded.HasErrors {
		t.Fatal("decoded report should have errors")
	}
}

func TestBuildDoctorReportAddsAIHints(t *testing.T) {
	path := writeTempTest(t, `
stages:
  - release

release:
  stage: release
  script:
    - release-cli create --tag-name "$CI_COMMIT_TAG" --name "$CI_COMMIT_TAG"
---
.glut:
  name: "release"
  setup:
    tag: "v1.2.0"
    mocks:
      binaries:
        release-cli:
          executable: |
            #!/bin/sh
            echo "release-cli $*"
  assert:
    job:
      release:
        exit-status: 0
`)

	report := buildDoctorReport([]string{path})
	if len(report.Files) != 1 {
		t.Fatalf("files len = %d", len(report.Files))
	}
	var hasBinaryHint bool
	var hasExitOnlyHint bool
	for _, hint := range report.Files[0].Hints {
		if hint.Path == ".glut.assert.binary" {
			hasBinaryHint = true
		}
		if hint.Path == ".glut.assert" && strings.Contains(hint.Message, "exit status") {
			hasExitOnlyHint = true
		}
	}
	if !hasBinaryHint || !hasExitOnlyHint {
		t.Fatalf("binary hint = %v, exit hint = %v, hints = %#v", hasBinaryHint, hasExitOnlyHint, report.Files[0].Hints)
	}
}

// TestDoctorHintBinaryNamesIncluded verifies that the binary mock names appear
// in the hint message so the AI knows which tools to assert.
func TestDoctorHintBinaryNamesIncluded(t *testing.T) {
	path := writeTempTest(t, `
job:
  script: mytools
---
.glut:
  name: "bins"
  setup:
    mocks:
      binaries:
        mytool:
          executable: "#!/bin/sh\necho ok"
        othertool:
          executable: "#!/bin/sh\necho ok"
  assert:
    job:
      job:
        exit-status: 0
`)
	report := buildDoctorReport([]string{path})
	if len(report.Files) != 1 {
		t.Fatalf("files len = %d", len(report.Files))
	}
	var binaryHint *doctorHint
	for i, h := range report.Files[0].Hints {
		if h.Path == ".glut.assert.binary" {
			binaryHint = &report.Files[0].Hints[i]
		}
	}
	if binaryHint == nil {
		t.Fatal("expected binary hint")
	}
	if !strings.Contains(binaryHint.Message, "mytool") || !strings.Contains(binaryHint.Message, "othertool") {
		t.Fatalf("expected mock names in hint message, got: %s", binaryHint.Message)
	}
}

// TestDoctorHintTagTestWithGitAssertNoHint verifies that hint #2 does not fire
// when the test already has an assert.git section.
func TestDoctorHintTagTestWithGitAssertNoHint(t *testing.T) {
	path := writeTempTest(t, `
stages:
  - release

release:
  stage: release
  script:
    - git tag "$CI_COMMIT_TAG"
---
.glut:
  name: "tag-git"
  setup:
    tag: "v1.0.0"
  assert:
    job:
      release:
        exit-status: 0
    git:
      workspace:
        branch: main
`)
	report := buildDoctorReport([]string{path})
	if len(report.Files) != 1 {
		t.Fatalf("files len = %d", len(report.Files))
	}
	for _, hint := range report.Files[0].Hints {
		if hint.Path == ".glut.assert" && strings.Contains(hint.Message, "tag or release") {
			t.Fatalf("unexpected tag hint when assert.git is present: %s", hint.Message)
		}
	}
}

// TestDoctorHintMostlyExitStatus verifies the updated hint fires when MOST
// (not necessarily all) jobs check only exit status.
func TestDoctorHintMostlyExitStatus(t *testing.T) {
	path := writeTempTest(t, `
job_a:
  script: echo a
job_b:
  script: echo b
job_c:
  script: echo c
---
.glut:
  name: "mostly-exit"
  assert:
    job:
      job_a:
        exit-status: 0
      job_b:
        exit-status: 0
      job_c:
        stdout: "c"
`)
	report := buildDoctorReport([]string{path})
	if len(report.Files) != 1 {
		t.Fatalf("files len = %d", len(report.Files))
	}
	var found bool
	for _, hint := range report.Files[0].Hints {
		if hint.Path == ".glut.assert" && strings.Contains(hint.Message, "exit status") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected mostly-exit-status hint, hints = %#v", report.Files[0].Hints)
	}
}

// TestDoctorHintAllJobsHaveRichAsserts verifies that the exit-status hint does
// NOT fire when all jobs have at least stdout/stderr/present.
func TestDoctorHintAllJobsHaveRichAsserts(t *testing.T) {
	path := writeTempTest(t, `
job_a:
  script: echo a
job_b:
  script: echo b
---
.glut:
  name: "rich"
  assert:
    job:
      job_a:
        stdout: "a"
      job_b:
        stdout: "b"
`)
	report := buildDoctorReport([]string{path})
	if len(report.Files) != 1 {
		t.Fatalf("files len = %d", len(report.Files))
	}
	for _, hint := range report.Files[0].Hints {
		if hint.Path == ".glut.assert" && strings.Contains(hint.Message, "exit status") {
			t.Fatalf("unexpected exit-status hint when all jobs have rich asserts: %s", hint.Message)
		}
	}
}

// TestDoctorHintEmptyAssert verifies a hint fires when assert is missing entirely.
func TestDoctorHintEmptyAssert(t *testing.T) {
	path := writeTempTest(t, `
job:
  script: echo ok
---
.glut:
  name: "empty"
`)
	report := buildDoctorReport([]string{path})
	if len(report.Files) != 1 {
		t.Fatalf("files len = %d", len(report.Files))
	}
	var found bool
	for _, hint := range report.Files[0].Hints {
		if hint.Path == ".glut.assert" && strings.Contains(hint.Message, "No asserts") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected empty-assert hint, hints = %#v", report.Files[0].Hints)
	}
}

// TestDoctorHintGitSetupWithoutGitAssert verifies hint fires for git setup
// without git asserts.
func TestDoctorHintGitSetupWithoutGitAssert(t *testing.T) {
	path := writeTempTest(t, `
job:
  script: git commit -m "change"
---
.glut:
  name: "git-setup"
  setup:
    git:
      user:
        name: "ci"
        email: "ci@example.com"
  assert:
    job:
      job:
        exit-status: 0
`)
	report := buildDoctorReport([]string{path})
	if len(report.Files) != 1 {
		t.Fatalf("files len = %d", len(report.Files))
	}
	var found bool
	for _, hint := range report.Files[0].Hints {
		if hint.Path == ".glut.assert.git" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected git assert hint, hints = %#v", report.Files[0].Hints)
	}
}

// TestDoctorHintScheduleSetup verifies hint fires for scheduled pipeline tests.
func TestDoctorHintScheduleSetup(t *testing.T) {
	path := writeTempTest(t, `
job:
  script: echo nightly
---
.glut:
  name: "nightly"
  setup:
    pipeline_source: schedule
    schedule:
      description: "nightly"
  assert:
    job:
      job:
        exit-status: 0
`)
	report := buildDoctorReport([]string{path})
	if len(report.Files) != 1 {
		t.Fatalf("files len = %d", len(report.Files))
	}
	var found bool
	for _, hint := range report.Files[0].Hints {
		if hint.Path == ".glut.setup.schedule" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected schedule hint, hints = %#v", report.Files[0].Hints)
	}
}

// TestDoctorHintUpstreamWithoutJobAsserts verifies hint fires for upstream
// triggered tests with no job asserts.
func TestDoctorHintUpstreamWithoutJobAsserts(t *testing.T) {
	path := writeTempTest(t, `
job:
  script: echo downstream
---
.glut:
  name: "downstream"
  setup:
    upstream:
      pipeline_id: 123
      project_id: 456
      job_id: 789
`)
	report := buildDoctorReport([]string{path})
	if len(report.Files) != 1 {
		t.Fatalf("files len = %d", len(report.Files))
	}
	var found bool
	for _, hint := range report.Files[0].Hints {
		if hint.Path == ".glut.assert.job" && strings.Contains(hint.Message, "upstream") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected upstream hint, hints = %#v", report.Files[0].Hints)
	}
}

// TestDoctorCoverageSummary verifies the coverage field is populated.
func TestDoctorCoverageSummary(t *testing.T) {
	path := writeTempTest(t, `
stages:
  - build
  - test

build:
  stage: build
  script: make

unit:
  stage: test
  script: make test
---
.glut:
  name: "partial"
  assert:
    job:
      build:
        exit-status: 0
`)
	report := buildDoctorReport([]string{path})
	if len(report.Files) != 1 {
		t.Fatalf("files len = %d", len(report.Files))
	}
	cov := report.Files[0].Coverage
	if cov == nil {
		t.Fatal("expected coverage, got nil")
	}
	if cov.JobsTotal < 2 {
		t.Fatalf("expected at least 2 total jobs, got %d", cov.JobsTotal)
	}
	if cov.JobsAsserted != 1 {
		t.Fatalf("expected 1 asserted job, got %d", cov.JobsAsserted)
	}
}

// TestDoctorCoverageSummaryInTextOutput verifies that coverage is printed in
// text mode.
func TestDoctorCoverageSummaryInTextOutput(t *testing.T) {
	path := writeTempTest(t, `
build:
  script: make
---
.glut:
  name: "cov-text"
  assert:
    job:
      build:
        exit-status: 0
`)
	report := buildDoctorReport([]string{path})
	var out bytes.Buffer
	if err := printDoctorReport(&out, &bytes.Buffer{}, report, "text"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[COVERAGE]") {
		t.Fatalf("expected [COVERAGE] in output, got:\n%s", out.String())
	}
}

// TestDoctorParseErrorsGoToStderr verifies that parse errors go to stderr and
// not to stdout in text mode.
func TestDoctorParseErrorsGoToStderr(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(path, []byte(": invalid: yaml: {\n"), 0644); err != nil {
		t.Fatal(err)
	}
	report := buildDoctorReport([]string{path})
	var stdout, stderr bytes.Buffer
	if err := printDoctorReport(&stdout, &stderr, report, "text"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "ERROR") {
		t.Fatalf("parse ERROR should not appear on stdout, got: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "ERROR") {
		t.Fatalf("expected parse ERROR on stderr, got: %s", stderr.String())
	}
}

// TestDoctorRunFilter verifies that -k only returns matching tests.
func TestDoctorRunFilter(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, dir, "alpha.yml", `
job:
  script: echo alpha
---
.glut:
  name: "alpha test"
  assert:
    job:
      job:
        exit-status: 0
`)
	writeTestFile(t, dir, "beta.yml", `
job:
  script: echo beta
---
.glut:
  name: "beta test"
  assert:
    job:
      job:
        exit-status: 0
`)

	report := buildDoctorReportFiltered([]string{dir}, "alpha")
	if len(report.Files) != 1 {
		t.Fatalf("expected 1 file after filter, got %d", len(report.Files))
	}
	if !strings.Contains(report.Files[0].File, "alpha") {
		t.Fatalf("expected alpha file, got %s", report.Files[0].File)
	}
}

func TestPrintLintTextFormat(t *testing.T) {
	report := lintReport{
		Files: []lintFileReport{
			{
				File: "tests/foo.yml",
				Issues: []lintIssue{
					{File: "tests/foo.yml", Level: "error", Category: "schema", Message: "schema error"},
					{File: "tests/foo.yml", Level: "warning", Category: "semantic", Message: "semantic warning"},
					{File: "tests/foo.yml", Level: "error", Category: "parse", Message: "parse error"},
				},
			},
		},
	}

	var stdout, stderr bytes.Buffer
	if err := printLintReport(&stdout, &stderr, report, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "[ERROR]") {
		t.Errorf("stdout should contain [ERROR], got: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "[WARNING]") {
		t.Errorf("stdout should contain [WARNING], got: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "parse error") {
		t.Errorf("stderr should contain parse error, got: %s", stderr.String())
	}
}

func TestSeedEntitySummary(t *testing.T) {
	empty := seedEntitySummary(&parser.APISeedConfig{})
	if empty != "seeded data" {
		t.Errorf("empty seed = %q, want 'seeded data'", empty)
	}

	relOnly := seedEntitySummary(&parser.APISeedConfig{
		Releases: []map[string]interface{}{{"tag_name": "v1"}},
	})
	if !strings.Contains(relOnly, "1 release(s)") {
		t.Errorf("releases only = %q", relOnly)
	}

	mrOnly := seedEntitySummary(&parser.APISeedConfig{
		MergeRequests: []map[string]interface{}{{"iid": 1}},
	})
	if !strings.Contains(mrOnly, "1 merge request(s)") {
		t.Errorf("MRs only = %q", mrOnly)
	}

	all := seedEntitySummary(&parser.APISeedConfig{
		Releases:      []map[string]interface{}{{"tag_name": "v1"}},
		MergeRequests: []map[string]interface{}{{"iid": 1}},
		Labels:        []map[string]interface{}{{"name": "bug"}},
	})
	if !strings.Contains(all, "release") || !strings.Contains(all, "merge") || !strings.Contains(all, "label") {
		t.Errorf("all seeds = %q", all)
	}
}

func TestLintPathCoverage(t *testing.T) {
	tests := []struct {
		msg  string
		want string
	}{
		{"glut schema: setup.branch: must be string", ".glut.setup.branch"},
		{"missing .glut.name in file", ".glut.name"},
		{"empty .glut.assert block", ".glut.assert"},
		{"assert.job references missing job", ".glut.assert.job"},
		{"setup.pipeline_source invalid", ".glut.setup"},
		{"some other message", ""},
	}
	for _, tt := range tests {
		got := lintPath(tt.msg)
		if got != tt.want {
			t.Errorf("lintPath(%q) = %q, want %q", tt.msg, got, tt.want)
		}
	}
}

func TestLintCategoryParse(t *testing.T) {
	if got := lintCategory("invalid yaml: something"); got != "parse" {
		t.Errorf("lintCategory parse = %q", got)
	}
	if got := lintCategory("invalid pipeline yaml: x"); got != "parse" {
		t.Errorf("lintCategory pipeline yaml = %q", got)
	}
	if got := lintCategory("cannot read file: x"); got != "parse" {
		t.Errorf("lintCategory read file = %q", got)
	}
}

func TestDoctorHintForMergeRequest(t *testing.T) {
	path := writeTempTest(t, `
job:
  script: echo ok
---
.glut:
  name: "mr-test"
  setup:
    pipeline_source: "merge_request_event"
    merge_request:
      iid: 1
      title: "My MR"
      target_branch: "main"
  assert:
    job:
      job:
        exit-status: 0
`)
	report := buildDoctorReport([]string{path})
	var hasMRHint bool
	for _, hint := range report.Files[0].Hints {
		if hint.Path == ".glut.assert.api" && strings.Contains(hint.Message, "merge request") {
			hasMRHint = true
		}
	}
	if !hasMRHint {
		t.Fatalf("expected merge request API hint, hints = %#v", report.Files[0].Hints)
	}
}

func TestBuildLintReportWithParseError(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "broken.yml", "job: [broken")
	writeTestFile(t, dir, "no-glut.yml", "job:\n  script: echo ok\n")

	report := buildLintReport([]string{dir})
	var hasParseError bool
	for _, f := range report.Files {
		for _, issue := range f.Issues {
			if issue.Category == "parse" {
				hasParseError = true
			}
		}
	}
	if !hasParseError {
		t.Fatal("expected parse error in lint report")
	}
}

func TestCoverageForFileNoJobs(t *testing.T) {
	tf := &parser.TestFile{
		FilePath: "test.yml",
		Glut: parser.GlutSection{
			Name: "no-jobs",
		},
	}
	if cov := coverageForFile(tf); cov != nil {
		t.Errorf("expected nil coverage for file with no pipeline jobs, got %+v", cov)
	}
}

func TestUnsupportedLintAndDoctorFormatsFail(t *testing.T) {
	if err := printLintReport(&bytes.Buffer{}, &bytes.Buffer{}, lintReport{}, "xml"); err == nil {
		t.Fatal("expected unsupported lint format error")
	}
	if err := printDoctorReport(&bytes.Buffer{}, &bytes.Buffer{}, doctorReport{}, "xml"); err == nil {
		t.Fatal("expected unsupported doctor format error")
	}
}

func writeTempTest(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.yml")
	if err := os.WriteFile(path, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSortLintIssuesSameLevelDifferentCategory(t *testing.T) {
	t.Parallel()
	issues := []lintIssue{
		{Level: "error", Category: "semantic", Message: "z"},
		{Level: "error", Category: "schema", Message: "a"},
	}
	sortLintIssues(issues)
	if issues[0].Category != "schema" || issues[1].Category != "semantic" {
		t.Fatalf("expected schema before semantic, got %s and %s", issues[0].Category, issues[1].Category)
	}
}
