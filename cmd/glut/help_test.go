package main

import (
	"bytes"
	"strings"
	"testing"
)

// commandHelp renders help for a (sub)command of a fresh root command tree.
// Subcommands are looked up through the root so they inherit the styled help
// function the same way they do in a real invocation.
func commandHelp(t *testing.T, name ...string) string {
	t.Helper()
	root := newRootCmd()
	cmd := root
	if len(name) > 0 {
		found, _, err := root.Find(name)
		if err != nil {
			t.Fatalf("find command %v: %v", name, err)
		}
		cmd = found
	}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Help(); err != nil {
		t.Fatalf("help for %v failed: %v", name, err)
	}
	return out.String()
}

func TestRootHelpContainsCommandsAndPurpose(t *testing.T) {
	t.Parallel()
	help := commandHelp(t)
	for _, want := range []string{
		"GLUT runs GitLab CI component tests locally.",
		"run",
		"lint",
		"list",
		"doctor",
		"version",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("root help missing %q:\n%s", want, help)
		}
	}
}

func TestRunHelpDocumentsReportsAndDebug(t *testing.T) {
	t.Parallel()
	help := commandHelp(t, "run")
	for _, want := range []string{
		"Run GLUT tests from one or more paths.",
		"--report",
		"--debug",
		"--keep-workspace",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("run help missing %q:\n%s", want, help)
		}
	}
}

func TestDoctorHelpDocumentsFilterAndFormat(t *testing.T) {
	t.Parallel()
	help := commandHelp(t, "doctor")
	for _, want := range []string{
		"--run",
		"--format",
		"coverage",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("doctor help missing %q:\n%s", want, help)
		}
	}
}
