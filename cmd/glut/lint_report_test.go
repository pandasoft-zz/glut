package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
  assert:
    job:
      missing_job: {}
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
		if issue.Category == "semantic" && issue.Path == ".glut.assert.job" {
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
