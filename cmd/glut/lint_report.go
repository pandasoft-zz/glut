package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/pandasoft-zz/glut/internal/parser"
)

type lintReport struct {
	Files     []lintFileReport `json:"files"`
	HasErrors bool             `json:"has_errors"`
}

type lintFileReport struct {
	File   string      `json:"file"`
	Issues []lintIssue `json:"issues"`
}

type lintIssue struct {
	File     string `json:"file"`
	Line     int    `json:"line,omitempty"`
	Level    string `json:"level"`
	Category string `json:"category"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
}

type doctorReport struct {
	Files     []doctorFileReport `json:"files"`
	HasErrors bool               `json:"has_errors"`
}

type doctorFileReport struct {
	File   string       `json:"file"`
	Issues []lintIssue  `json:"issues"`
	Hints  []doctorHint `json:"hints"`
}

type doctorHint struct {
	File    string `json:"file"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

func buildLintReport(paths []string) lintReport {
	files, parseIssues := collectLintFiles(paths)
	byFile := make(map[string][]lintIssue)
	for _, issue := range parseIssues {
		byFile[issue.File] = append(byFile[issue.File], issue)
	}
	for _, file := range files {
		for _, lint := range parser.Lint(file.FilePath) {
			issue := lintIssueFromLint(lint)
			byFile[issue.File] = append(byFile[issue.File], issue)
		}
		if _, ok := byFile[file.FilePath]; !ok {
			byFile[file.FilePath] = nil
		}
	}
	return lintReportFromMap(byFile)
}

func buildDoctorReport(paths []string) doctorReport {
	files, parseIssues := collectLintFiles(paths)
	byFile := make(map[string]doctorFileReport)
	for _, issue := range parseIssues {
		report := byFile[issue.File]
		report.File = issue.File
		report.Issues = append(report.Issues, issue)
		byFile[issue.File] = report
	}
	for _, file := range files {
		report := byFile[file.FilePath]
		report.File = file.FilePath
		for _, lint := range parser.Lint(file.FilePath) {
			report.Issues = append(report.Issues, lintIssueFromLint(lint))
		}
		report.Hints = append(report.Hints, doctorHintsForFile(file)...)
		byFile[file.FilePath] = report
	}
	return doctorReportFromMap(byFile)
}

func collectLintFiles(paths []string) ([]*parser.TestFile, []lintIssue) {
	var files []*parser.TestFile
	var issues []lintIssue
	for _, path := range paths {
		parsed, errs := parser.ParseDir(path)
		files = append(files, parsed...)
		for _, err := range errs {
			issues = append(issues, lintIssue{
				File:     path,
				Level:    "error",
				Category: "parse",
				Message:  err.Error(),
			})
		}
	}
	return files, issues
}

func lintReportFromMap(byFile map[string][]lintIssue) lintReport {
	files := make([]string, 0, len(byFile))
	for file := range byFile {
		files = append(files, file)
	}
	sort.Strings(files)

	report := lintReport{Files: make([]lintFileReport, 0, len(files))}
	for _, file := range files {
		issues := byFile[file]
		if issues == nil {
			issues = []lintIssue{}
		}
		sortLintIssues(issues)
		if hasErrorIssue(issues) {
			report.HasErrors = true
		}
		report.Files = append(report.Files, lintFileReport{File: file, Issues: issues})
	}
	return report
}

func doctorReportFromMap(byFile map[string]doctorFileReport) doctorReport {
	files := make([]string, 0, len(byFile))
	for file := range byFile {
		files = append(files, file)
	}
	sort.Strings(files)

	report := doctorReport{Files: make([]doctorFileReport, 0, len(files))}
	for _, file := range files {
		fileReport := byFile[file]
		if fileReport.Issues == nil {
			fileReport.Issues = []lintIssue{}
		}
		if fileReport.Hints == nil {
			fileReport.Hints = []doctorHint{}
		}
		sortLintIssues(fileReport.Issues)
		sortDoctorHints(fileReport.Hints)
		if hasErrorIssue(fileReport.Issues) {
			report.HasErrors = true
		}
		report.Files = append(report.Files, fileReport)
	}
	return report
}

func lintIssueFromLint(lint parser.LintError) lintIssue {
	return lintIssue{
		File:     lint.File,
		Line:     lint.Line,
		Level:    lintLevelName(lint.Level),
		Category: lintCategory(lint.Message),
		Path:     lintPath(lint.Message),
		Message:  lint.Message,
	}
}

func lintLevelName(level parser.LintLevel) string {
	if level == parser.LevelError {
		return "error"
	}
	return "warning"
}

func lintCategory(message string) string {
	if strings.HasPrefix(message, "glut schema:") {
		return "schema"
	}
	if strings.HasPrefix(message, "invalid yaml:") || strings.HasPrefix(message, "invalid pipeline yaml:") || strings.HasPrefix(message, "cannot read file:") {
		return "parse"
	}
	return "semantic"
}

func lintPath(message string) string {
	trimmed := strings.TrimPrefix(message, "glut schema: ")
	if strings.Contains(trimmed, ":") {
		field := strings.TrimSpace(strings.SplitN(trimmed, ":", 2)[0])
		if field != "" && !strings.Contains(field, " ") {
			return ".glut." + strings.TrimPrefix(field, ".")
		}
	}
	switch {
	case strings.Contains(message, "missing .glut.name"):
		return ".glut.name"
	case strings.Contains(message, ".glut.assert"):
		return ".glut.assert"
	case strings.Contains(message, "assert.job"):
		return ".glut.assert.job"
	case strings.Contains(message, "setup."):
		return ".glut.setup"
	default:
		return ""
	}
}

func doctorHintsForFile(file *parser.TestFile) []doctorHint {
	var hints []doctorHint
	if len(file.Glut.Assert.Job) > 0 && onlyJobExitStatus(file) {
		hints = append(hints, doctorHint{
			File:    file.FilePath,
			Path:    ".glut.assert",
			Message: "This test mostly checks job exit status. Add an artifact, git, API, or binary assert for the behavior that matters.",
		})
	}
	if file.Glut.Setup.Tag != "" && len(file.Glut.Assert.Binary) == 0 && len(file.Glut.Assert.API) == 0 {
		hints = append(hints, doctorHint{
			File:    file.FilePath,
			Path:    ".glut.assert",
			Message: "This looks like a tag or release test. Consider asserting the release API call or the release binary call.",
		})
	}
	if file.Glut.Setup.PipelineSource == "merge_request_event" && file.Glut.Setup.MergeRequest != nil && len(file.Glut.Assert.API) == 0 {
		hints = append(hints, doctorHint{
			File:    file.FilePath,
			Path:    ".glut.assert.api",
			Message: "This is a merge request test. Consider asserting merge request API calls if the component should comment, approve, or read merge request data.",
		})
	}
	if file.Glut.Setup.Mocks != nil && len(file.Glut.Setup.Mocks.Binaries) > 0 && len(file.Glut.Assert.Binary) == 0 {
		hints = append(hints, doctorHint{
			File:    file.FilePath,
			Path:    ".glut.assert.binary",
			Message: "Mock binaries are configured, but no binary asserts exist. Add assert.binary checks for called tools and arguments.",
		})
	}
	if file.Glut.Setup.API != nil && len(file.Glut.Assert.API) == 0 {
		hints = append(hints, doctorHint{
			File:    file.FilePath,
			Path:    ".glut.assert.api",
			Message: "Mock API setup exists, but no API asserts exist. Add assert.api checks for important GitLab API calls.",
		})
	}
	return hints
}

func onlyJobExitStatus(file *parser.TestFile) bool {
	if len(file.Glut.Assert.Artifacts) > 0 || file.Glut.Assert.Git != nil || len(file.Glut.Assert.API) > 0 || len(file.Glut.Assert.Binary) > 0 {
		return false
	}
	for _, job := range file.Glut.Assert.Job {
		if job.Present != nil || job.Stdout != nil || job.Stderr != nil {
			return false
		}
		if job.ExitStatus == nil {
			return false
		}
	}
	return len(file.Glut.Assert.Job) > 0
}

func printLintReport(stdout io.Writer, stderr io.Writer, report lintReport, format string) error {
	switch format {
	case "", "text":
		printLintText(stdout, stderr, report)
	case "json":
		return writeJSON(stdout, report)
	default:
		return fmt.Errorf("unsupported lint format %q", format)
	}
	return nil
}

func printDoctorReport(stdout io.Writer, stderr io.Writer, report doctorReport, format string) error {
	switch format {
	case "", "text":
		printDoctorText(stdout, stderr, report)
	case "json":
		return writeJSON(stdout, report)
	default:
		return fmt.Errorf("unsupported doctor format %q", format)
	}
	return nil
}

func printLintText(stdout io.Writer, stderr io.Writer, report lintReport) {
	for _, file := range report.Files {
		for _, issue := range file.Issues {
			prefix := "WARNING"
			if issue.Level == "error" {
				prefix = "ERROR"
			}
			out := stdout
			if issue.Category == "parse" {
				out = stderr
			}
			_, _ = fmt.Fprintf(out, "[%s] %s: %s\n", prefix, issue.File, issue.Message)
		}
	}
}

func printDoctorText(stdout io.Writer, stderr io.Writer, report doctorReport) {
	lint := lintReport{HasErrors: report.HasErrors}
	for _, file := range report.Files {
		lint.Files = append(lint.Files, lintFileReport{File: file.File, Issues: file.Issues})
	}
	printLintText(stdout, stderr, lint)
	for _, file := range report.Files {
		for _, hint := range file.Hints {
			path := ""
			if hint.Path != "" {
				path = " " + hint.Path + ":"
			}
			_, _ = fmt.Fprintf(stdout, "[HINT] %s:%s %s\n", hint.File, path, hint.Message)
		}
	}
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func hasErrorIssue(issues []lintIssue) bool {
	for _, issue := range issues {
		if issue.Level == "error" {
			return true
		}
	}
	return false
}

func sortLintIssues(issues []lintIssue) {
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Level != issues[j].Level {
			return issues[i].Level < issues[j].Level
		}
		if issues[i].Category != issues[j].Category {
			return issues[i].Category < issues[j].Category
		}
		return issues[i].Message < issues[j].Message
	})
}

func sortDoctorHints(hints []doctorHint) {
	sort.SliceStable(hints, func(i, j int) bool {
		if hints[i].Path != hints[j].Path {
			return hints[i].Path < hints[j].Path
		}
		return hints[i].Message < hints[j].Message
	})
}
