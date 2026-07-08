package asserter

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/mockwrapper"
)

func TestFileTypeOf(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	file := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileTypeOf(fileInfo); got != "file" {
		t.Fatalf("fileTypeOf(regular) = %q", got)
	}

	dirInfo, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileTypeOf(dirInfo); got != "directory" {
		t.Fatalf("fileTypeOf(dir) = %q", got)
	}

	if runtime.GOOS != "windows" {
		link := filepath.Join(dir, "link")
		if err := os.Symlink(file, link); err != nil {
			t.Skipf("symlinks unsupported: %v", err)
		}
		linkInfo, err := os.Lstat(link)
		if err != nil {
			t.Fatal(err)
		}
		if got := fileTypeOf(linkInfo); got != "symlink" {
			t.Fatalf("fileTypeOf(symlink) = %q", got)
		}
	}
}

func TestJoinWorkspacePathRejectsEscapes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if _, err := joinWorkspacePath(root, "../outside.txt"); err == nil {
		t.Fatal("parent traversal must be rejected")
	}
	if _, err := joinWorkspacePath(root, "/etc/passwd"); err == nil {
		t.Fatal("absolute paths must be rejected")
	}
	got, err := joinWorkspacePath(root, filepath.Join("sub", "file.txt"))
	if err != nil || !strings.HasPrefix(got, root) {
		t.Fatalf("joinWorkspacePath() = %q, %v", got, err)
	}
}

func TestBinaryCallMatchesFieldByField(t *testing.T) {
	t.Parallel()
	call := mockwrapper.BinaryCall{
		Name:  "release-cli",
		Args:  []string{"create", "--tag-name", "v1"},
		CWD:   "/builds/project",
		Stdin: "payload",
	}

	tests := []struct {
		name   string
		assert config.BinaryCallAssert
		want   bool
	}{
		{name: "empty assert matches anything", assert: config.BinaryCallAssert{}, want: true},
		{name: "all fields match", assert: config.BinaryCallAssert{
			Args:  map[string]any{"contain-element": "create"},
			CWD:   map[string]any{"have-suffix": "/project"},
			Stdin: map[string]any{"contain-substring": "pay"},
		}, want: true},
		{name: "args mismatch", assert: config.BinaryCallAssert{
			Args: map[string]any{"contain-element": "delete"},
		}, want: false},
		{name: "cwd mismatch", assert: config.BinaryCallAssert{
			CWD: map[string]any{"have-prefix": "/home"},
		}, want: false},
		{name: "stdin mismatch", assert: config.BinaryCallAssert{
			Stdin: map[string]any{"contain-substring": "nope"},
		}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := binaryCallMatches(tt.assert, call); got != tt.want {
				t.Fatalf("binaryCallMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}
