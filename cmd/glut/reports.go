package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pandasoft-zz/glut/internal/reporter"
	"github.com/pandasoft-zz/glut/internal/runner"
)

type reportTarget struct {
	format string
	path   string
}

type fileReportTarget struct {
	path   string
	report reporter.FileReport
}

func buildProgressSinks(opts RunOptions, writer io.Writer) ([]runner.ProgressSink, []fileReportTarget, error) {
	console, err := reporter.NewConsole(reporter.ConsoleOptions{
		Format:  opts.Format,
		Quiet:   opts.Quiet,
		Verbose: opts.Verbose,
		Debug:   opts.Debug,
		Version: version,
		Writer:  writer,
	})
	if err != nil {
		return nil, nil, err
	}

	sinks := []runner.ProgressSink{console}
	targets, err := parseReportTargets(opts.Reports)
	if err != nil {
		return nil, nil, err
	}

	fileReports := make([]fileReportTarget, 0, len(targets))
	for _, target := range targets {
		fileReport, err := newFileReport(target.format)
		if err != nil {
			return nil, nil, err
		}
		sinks = append(sinks, fileReport)
		fileReports = append(fileReports, fileReportTarget{
			path:   target.path,
			report: fileReport,
		})
	}

	return sinks, fileReports, nil
}

func parseReportTargets(values []string) ([]reportTarget, error) {
	targets := make([]reportTarget, 0, len(values))
	for _, value := range values {
		format, path, ok := strings.Cut(value, ":")
		format = strings.TrimSpace(format)
		path = strings.TrimSpace(path)
		if !ok || format == "" || path == "" {
			return nil, fmt.Errorf("invalid report target %q, want <format>:<path>", value)
		}
		targets = append(targets, reportTarget{
			format: format,
			path:   path,
		})
	}
	return targets, nil
}

func newFileReport(format string) (reporter.FileReport, error) {
	switch strings.TrimSpace(format) {
	case "junit":
		return reporter.NewJUnit(), nil
	case "tap":
		return reporter.NewTAP(), nil
	default:
		return nil, fmt.Errorf("unsupported report format %q", format)
	}
}

func writeFileReports(targets []fileReportTarget) error {
	for _, target := range targets {
		if err := target.report.WriteFile(target.path); err != nil {
			return err
		}
	}
	return nil
}

func stderrWriter() io.Writer {
	return os.Stderr
}
