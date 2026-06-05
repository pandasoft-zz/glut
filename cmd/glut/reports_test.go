package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pandasoft-zz/glut/internal/runner"
)

func TestParseReportTargets(t *testing.T) {
	targets, err := parseReportTargets([]string{" junit: report.xml ", "tap:out.tap"})
	if err != nil {
		t.Fatalf("parseReportTargets() error = %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets len = %d", len(targets))
	}
	if targets[0].format != "junit" || targets[0].path != "report.xml" {
		t.Fatalf("first target = %#v", targets[0])
	}

	for _, value := range []string{"bad", ":path", "junit:"} {
		if _, err := parseReportTargets([]string{value}); err == nil {
			t.Fatalf("parseReportTargets(%q) error = nil", value)
		}
	}
}

func TestNewFileReport(t *testing.T) {
	for _, format := range []string{"junit", "tap"} {
		report, err := newFileReport(format)
		if err != nil {
			t.Fatalf("newFileReport(%q) error = %v", format, err)
		}
		if report == nil {
			t.Fatalf("newFileReport(%q) returned nil", format)
		}
	}

	if _, err := newFileReport("unknown"); err == nil {
		t.Fatal("expected unsupported report format error")
	}
}

func TestBuildProgressSinksIncludesConsoleAndFileReports(t *testing.T) {
	var out bytes.Buffer
	sinks, fileReports, err := buildProgressSinks(RunOptions{
		Reports: []string{"junit:report.xml", "tap:report.tap"},
	}, &out)
	if err != nil {
		t.Fatalf("buildProgressSinks() error = %v", err)
	}
	if len(sinks) != 3 {
		t.Fatalf("sinks len = %d, want 3", len(sinks))
	}
	if len(fileReports) != 2 {
		t.Fatalf("file reports len = %d, want 2", len(fileReports))
	}
	if fileReports[0].path != "report.xml" || fileReports[1].path != "report.tap" {
		t.Fatalf("file report paths = %#v", fileReports)
	}

	if _, _, err := buildProgressSinks(RunOptions{Format: "bad"}, &out); err == nil {
		t.Fatal("expected invalid console format error")
	}
	if _, _, err := buildProgressSinks(RunOptions{Reports: []string{"bad"}}, &out); err == nil {
		t.Fatal("expected invalid report target error")
	}
	if _, _, err := buildProgressSinks(RunOptions{Reports: []string{"xml:report.xml"}}, &out); err == nil {
		t.Fatal("expected unsupported file report error")
	}
}

func TestWriteFileReports(t *testing.T) {
	dir := t.TempDir()
	okPath := filepath.Join(dir, "ok.txt")
	okReport := &fakeFileReport{}

	if err := writeFileReports([]fileReportTarget{{path: okPath, report: okReport}}); err != nil {
		t.Fatalf("writeFileReports() error = %v", err)
	}
	if okReport.path != okPath {
		t.Fatalf("fake report path = %q", okReport.path)
	}

	failReport := &fakeFileReport{err: errors.New("write failed")}
	err := writeFileReports([]fileReportTarget{{path: filepath.Join(dir, "bad.txt"), report: failReport}})
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("writeFileReports() error = %v", err)
	}
}

type fakeFileReport struct {
	path string
	err  error
}

func (f *fakeFileReport) Start(_ int)                       {}
func (f *fakeFileReport) TestRetry(_ string, _ error)       {}
func (f *fakeFileReport) TestDone(_ runner.TestResult)      {}
func (f *fakeFileReport) Summary(_ runner.RunResult)        {}

func (f *fakeFileReport) WriteFile(path string) error {
	f.path = path
	return f.err
}
