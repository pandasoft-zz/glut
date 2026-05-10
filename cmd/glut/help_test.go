package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRootHelpContainsCommandsAndPurpose(t *testing.T) {
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	t.Cleanup(func() {
		rootCmd.SetOut(os.Stdout)
		rootCmd.SetErr(os.Stderr)
	})

	if err := rootCmd.Help(); err != nil {
		t.Fatalf("root help failed: %v", err)
	}

	help := out.String()
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
	var out bytes.Buffer
	runCmd.SetOut(&out)
	runCmd.SetErr(&out)
	t.Cleanup(func() {
		runCmd.SetOut(os.Stdout)
		runCmd.SetErr(os.Stderr)
	})

	if err := runCmd.Help(); err != nil {
		t.Fatalf("run help failed: %v", err)
	}

	help := out.String()
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
