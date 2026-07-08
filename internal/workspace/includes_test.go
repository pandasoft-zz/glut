package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pandasoft-zz/glut/internal/parser"
)

// TestNewWithIncludesCopiesOnlyListedDirs pins the include-filtered copy
// path: only the listed subdirectories reach the workspace snapshot.
// (setup.branch affects CI variables, not the git checkout, so the branch
// name is not asserted here.)
func TestNewWithIncludesCopiesOnlyListedDirs(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "app"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app", "main.txt"), []byte("app"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docs", "guide.md"), []byte("doc"), 0644); err != nil {
		t.Fatal(err)
	}

	work, err := New(parser.SetupConfig{Branch: "feature"}, false, repo, Options{Include: []string{"app"}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = work.Destroy() })

	if _, err := os.Stat(filepath.Join(work.WorkspaceDir, "app", "main.txt")); err != nil {
		t.Fatalf("included dir missing from workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work.WorkspaceDir, "docs", "guide.md")); !os.IsNotExist(err) {
		t.Fatalf("docs must be excluded by Include, stat err = %v", err)
	}
}
