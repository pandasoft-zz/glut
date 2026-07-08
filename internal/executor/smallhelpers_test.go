package executor

import (
	"strings"
	"testing"
)

func TestParseJobMarkerRejectsInvalidExit(t *testing.T) {
	t.Parallel()
	if _, ok := parseJobMarker("GLUT_JOB|name=build|exit=notanumber"); ok {
		t.Fatal("a marker with a non-numeric exit must be rejected")
	}
	job, ok := parseJobMarker("GLUT_JOB|name=build|exit=3|garbagewithoutequals")
	if !ok || job.ExitStatus != 3 || !job.StatusKnown {
		t.Fatalf("marker with a stray segment = %+v, %v", job, ok)
	}
}

func TestTailForErrorKeepsLastLines(t *testing.T) {
	t.Parallel()
	if got := tailForError("  short  "); got != "short" {
		t.Fatalf("tailForError(short) = %q", got)
	}
	if got := tailForError("   "); got != "" {
		t.Fatalf("tailForError(blank) = %q", got)
	}

	lines := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		lines = append(lines, "line")
	}
	lines[19] = "last"
	got := tailForError(strings.Join(lines, "\n"))
	if gotLines := strings.Split(got, "\n"); len(gotLines) != 8 || gotLines[7] != "last" {
		t.Fatalf("tailForError must keep the last 8 lines, got %q", got)
	}
}

func TestFirstLinePicksFirstNonEmpty(t *testing.T) {
	t.Parallel()
	if got := firstLine("", "second\nrest"); got != "second" {
		t.Fatalf("firstLine() = %q", got)
	}
	if got := firstLine("", ""); got != "" {
		t.Fatalf("firstLine(empties) = %q", got)
	}
}
