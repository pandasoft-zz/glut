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
	File     string       `json:"file"`
	Issues   []lintIssue  `json:"issues"`
	Hints    []doctorHint `json:"hints"`
	Coverage *coverage    `json:"coverage,omitempty"`
}

type doctorHint struct {
	File    string `json:"file"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

type coverage struct {
	JobsTotal    int `json:"jobs_total"`
	JobsAsserted int `json:"jobs_asserted"`
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

func buildDoctorReport(paths []string) buildDoctorReportResult {
	return buildDoctorReportFiltered(paths, "")
}

type buildDoctorReportResult = doctorReport

func buildDoctorReportFiltered(paths []string, pattern string) doctorReport {
	files, parseIssues := collectLintFiles(paths)
	byFile := make(map[string]doctorFileReport)
	for _, issue := range parseIssues {
		report := byFile[issue.File]
		report.File = issue.File
		report.Issues = append(report.Issues, issue)
		byFile[issue.File] = report
	}
	for _, file := range files {
		if pattern != "" && !matchesPattern(file.Glut.Name, pattern) {
			continue
		}
		report := byFile[file.FilePath]
		report.File = file.FilePath
		for _, lint := range parser.Lint(file.FilePath) {
			report.Issues = append(report.Issues, lintIssueFromLint(lint))
		}
		report.Hints = append(report.Hints, doctorHintsForFile(file)...)
		report.Coverage = coverageForFile(file)
		byFile[file.FilePath] = report
	}
	return doctorReportFromMap(byFile)
}

func matchesPattern(name, pattern string) bool {
	return strings.Contains(name, pattern)
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

func coverageForFile(file *parser.TestFile) *coverage {
	pipelineJobs := pipelineJobNames(file)
	if len(pipelineJobs) == 0 {
		return nil
	}
	asserted := 0
	for _, name := range pipelineJobs {
		if _, ok := file.Glut.Assert.Job[name]; ok {
			asserted++
		}
	}
	return &coverage{
		JobsTotal:    len(pipelineJobs),
		JobsAsserted: asserted,
	}
}

// pipelineJobNames returns the job names inferred from the parsed pipeline YAML.
// It uses the raw GlutRaw map is not the pipeline; we derive jobs from what
// assert.job references plus any jobs found via the pipeline text. Since
// TestFile does not expose a parsed pipeline map directly, we use the jobs
// that appear in assert.job as a minimum set and complement with the pipeline
// YAML when parseable.
func pipelineJobNames(file *parser.TestFile) []string {
	seen := make(map[string]struct{})
	// Jobs we already know about from assert.job
	for name := range file.Glut.Assert.Job {
		seen[name] = struct{}{}
	}
	// Walk the raw pipeline YAML for job keys (lines that look like top-level
	// map keys followed by "script:" or "stage:").
	if file.PipelineYAML != "" {
		for _, name := range extractPipelineJobNames(file.PipelineYAML) {
			seen[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var gitlabTopLevelKeywords = map[string]bool{
	"stages":        true,
	"variables":     true,
	"image":         true,
	"before_script": true,
	"after_script":  true,
	"cache":         true,
	"services":      true,
	"workflow":      true,
	"include":       true,
	"default":       true,
	"pages":         true,
}

func extractPipelineJobNames(pipelineYAML string) []string {
	var names []string
	for _, line := range strings.Split(pipelineYAML, "\n") {
		if len(line) == 0 || line[0] == ' ' || line[0] == '\t' || line[0] == '#' || line[0] == '-' {
			continue
		}
		colon := strings.Index(line, ":")
		if colon <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		if key == "" || strings.HasPrefix(key, ".") || gitlabTopLevelKeywords[key] {
			continue
		}
		names = append(names, key)
	}
	return names
}

func doctorHintsForFile(file *parser.TestFile) []doctorHint {
	var hints []doctorHint

	// Upstream trigger setup — assert coverage may be insufficient.
	// Checked before the empty-assert early return so it fires independently.
	if file.Glut.Setup.Upstream != nil && len(file.Glut.Assert.Job) == 0 {
		hints = append(hints, doctorHint{
			File:    file.FilePath,
			Path:    ".glut.assert.job",
			Message: "This is an upstream-triggered pipeline test with no job asserts. Add assert.job checks for the jobs that should run.",
		})
	}

	// Empty assert block — no asserts at all.
	if hasNoAsserts(file) {
		hints = append(hints, doctorHint{
			File:    file.FilePath,
			Path:    ".glut.assert",
			Message: "No asserts are defined. Add at least one job, artifact, git, API, or binary assert.",
		})
		return hints
	}

	// Most jobs check only exit status.
	if mostlyJobExitStatus(file) {
		hints = append(hints, doctorHint{
			File:    file.FilePath,
			Path:    ".glut.assert",
			Message: "Most job asserts only check exit status. Add an artifact, git, API, or binary assert for the behavior that matters.",
		})
	}

	// Tag/release test without meaningful asserts.
	if file.Glut.Setup.Tag != "" && len(file.Glut.Assert.Binary) == 0 && len(file.Glut.Assert.API) == 0 && file.Glut.Assert.Git == nil {
		hints = append(hints, doctorHint{
			File:    file.FilePath,
			Path:    ".glut.assert",
			Message: "This looks like a tag or release test. Consider asserting the release API call, the release binary call, or the git state after the job.",
		})
	}

	// Merge request test without API asserts.
	if file.Glut.Setup.PipelineSource == "merge_request_event" && file.Glut.Setup.MergeRequest != nil && len(file.Glut.Assert.API) == 0 {
		hints = append(hints, doctorHint{
			File:    file.FilePath,
			Path:    ".glut.assert.api",
			Message: "This is a merge request test. Consider asserting merge request API calls if the component should comment, approve, or read merge request data.",
		})
	}

	// Mock binaries defined but not asserted.
	if file.Glut.Setup.Mocks != nil && len(file.Glut.Setup.Mocks.Binaries) > 0 && len(file.Glut.Assert.Binary) == 0 {
		names := sortedKeys(file.Glut.Setup.Mocks.Binaries)
		hints = append(hints, doctorHint{
			File:    file.FilePath,
			Path:    ".glut.assert.binary",
			Message: fmt.Sprintf("Mock binaries are configured (%s), but no binary asserts exist. Add assert.binary checks for called tools and arguments.", strings.Join(names, ", ")),
		})
	}

	// Mock API setup but no API asserts.
	if file.Glut.Setup.API != nil && len(file.Glut.Assert.API) == 0 {
		hints = append(hints, doctorHint{
			File:    file.FilePath,
			Path:    ".glut.assert.api",
			Message: "Mock API setup exists, but no API asserts exist. Add assert.api checks for important GitLab API calls.",
		})
	}

	// API seed data but no API asserts.
	if file.Glut.Setup.API != nil && file.Glut.Setup.API.Seed != nil && len(file.Glut.Assert.API) == 0 {
		entities := seedEntitySummary(file.Glut.Setup.API.Seed)
		hints = append(hints, doctorHint{
			File:    file.FilePath,
			Path:    ".glut.assert.api",
			Message: fmt.Sprintf("API seed data is configured (%s), but no API asserts exist. The component may be reading this data — assert the API calls it makes.", entities),
		})
	}

	// Git setup without git asserts.
	if file.Glut.Setup.Git != nil && file.Glut.Assert.Git == nil {
		hints = append(hints, doctorHint{
			File:    file.FilePath,
			Path:    ".glut.assert.git",
			Message: "Git setup is configured, but no git asserts exist. Consider asserting commits, branch state, or file contents after the pipeline runs.",
		})
	}

	// Schedule setup — no schedule-specific hints possible yet, but flag it.
	if file.Glut.Setup.Schedule != nil {
		hints = append(hints, doctorHint{
			File:    file.FilePath,
			Path:    ".glut.setup.schedule",
			Message: "This is a scheduled pipeline test. Make sure asserts cover the behavior that is unique to the scheduled trigger.",
		})
	}

	return hints
}

func hasNoAsserts(file *parser.TestFile) bool {
	a := file.Glut.Assert
	return len(a.Job) == 0 && len(a.Artifacts) == 0 && a.Git == nil && len(a.API) == 0 && len(a.Binary) == 0
}

// mostlyJobExitStatus returns true when more than half of the job asserts check
// only exit status (no stdout, stderr, or present check), and no other assert
// types are present.
func mostlyJobExitStatus(file *parser.TestFile) bool {
	if len(file.Glut.Assert.Job) == 0 {
		return false
	}
	if len(file.Glut.Assert.Artifacts) > 0 || file.Glut.Assert.Git != nil || len(file.Glut.Assert.API) > 0 || len(file.Glut.Assert.Binary) > 0 {
		return false
	}
	exitOnlyCount := 0
	for _, job := range file.Glut.Assert.Job {
		if job.Present == nil && job.Stdout == nil && job.Stderr == nil && job.ExitStatus != nil {
			exitOnlyCount++
		}
	}
	return exitOnlyCount > len(file.Glut.Assert.Job)/2
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func seedEntitySummary(seed *parser.APISeedConfig) string {
	var parts []string
	if len(seed.Releases) > 0 {
		parts = append(parts, fmt.Sprintf("%d release(s)", len(seed.Releases)))
	}
	if len(seed.MergeRequests) > 0 {
		parts = append(parts, fmt.Sprintf("%d merge request(s)", len(seed.MergeRequests)))
	}
	if len(seed.Labels) > 0 {
		parts = append(parts, fmt.Sprintf("%d label(s)", len(seed.Labels)))
	}
	if len(parts) == 0 {
		return "seeded data"
	}
	return strings.Join(parts, ", ")
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
		printDoctorText(stdout, report)
		printDoctorIssuesStderr(stderr, report)
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

// printDoctorText writes all doctor output to stdout only: lint issues, hints,
// and coverage summary. Parse errors go to stderr via printDoctorIssuesStderr.
func printDoctorText(stdout io.Writer, report doctorReport) {
	for _, file := range report.Files {
		for _, issue := range file.Issues {
			if issue.Category == "parse" {
				continue
			}
			prefix := "WARNING"
			if issue.Level == "error" {
				prefix = "ERROR"
			}
			_, _ = fmt.Fprintf(stdout, "[%s] %s: %s\n", prefix, file.File, issue.Message)
		}
		for _, hint := range file.Hints {
			path := ""
			if hint.Path != "" {
				path = " " + hint.Path + ":"
			}
			_, _ = fmt.Fprintf(stdout, "[HINT] %s:%s %s\n", hint.File, path, hint.Message)
		}
		if file.Coverage != nil {
			_, _ = fmt.Fprintf(stdout, "[COVERAGE] %s: %d/%d jobs asserted\n", file.File, file.Coverage.JobsAsserted, file.Coverage.JobsTotal)
		}
	}
}

func printDoctorIssuesStderr(stderr io.Writer, report doctorReport) {
	for _, file := range report.Files {
		for _, issue := range file.Issues {
			if issue.Category == "parse" {
				_, _ = fmt.Fprintf(stderr, "[ERROR] %s: %s\n", file.File, issue.Message)
			}
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
