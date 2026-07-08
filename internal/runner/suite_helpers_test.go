package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/parser"
)

func TestNeedsDockerOrigin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		file parser.TestFile
		want bool
	}{
		{name: "no git asserts", file: parser.TestFile{}, want: false},
		{
			name: "git asserts without origin",
			file: parser.TestFile{Glut: parser.GlutSection{Assert: config.AssertConfig{
				Git: &config.GitAssert{Workspace: &config.GitRepoAssert{}},
			}}},
			want: false,
		},
		{
			name: "git origin asserts",
			file: parser.TestFile{Glut: parser.GlutSection{Assert: config.AssertConfig{
				Git: &config.GitAssert{Origin: &config.GitRepoAssert{}},
			}}},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := needsDockerOrigin(tt.file); got != tt.want {
				t.Fatalf("needsDockerOrigin() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDestroyPendingVolumesIsBestEffort exercises the suite-level bulk volume
// cleanup with a fake docker CLI: every collected volume gets a removal
// attempt and failures are intentionally ignored.
func TestDestroyPendingVolumesIsBestEffort(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake docker CLI is a POSIX shell script")
	}
	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "calls.log")
	script := "#!/bin/sh\necho \"$*\" >> \"" + logPath + "\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	suite := &suiteRun{pendingVolumes: []string{"glut-a", "glut-b"}}
	suite.destroyPendingVolumes() // must not panic or fail on docker errors

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("fake docker was never invoked: %v", err)
	}
	calls := string(data)
	for _, vol := range []string{"glut-a", "glut-b"} {
		if !strings.Contains(calls, "volume rm "+vol) {
			t.Fatalf("missing removal attempt for %s:\n%s", vol, calls)
		}
	}
}
