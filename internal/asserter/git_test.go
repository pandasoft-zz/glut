package asserter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
)

func TestRunGitAsserts(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	origin := filepath.Join(root, "origin.git")
	workspace := filepath.Join(root, "workspace")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, source, "init")
	mustRunGit(t, source, "config", "user.email", "alice@example.com")
	mustRunGit(t, source, "config", "user.name", "Alice Example")
	if err := os.WriteFile(filepath.Join(source, "config.txt"), []byte("version: 2.0.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, source, "add", "config.txt")
	mustRunGit(t, source, "commit", "-m", "chore: update config")
	mustRunGit(t, source, "init", "--bare", origin)
	mustRunGit(t, source, "remote", "add", "origin", origin)
	mustRunGit(t, source, "branch", "-M", "main")
	mustRunGit(t, source, "push", "-u", "origin", "main")
	mustRunGit(t, root, "--git-dir", origin, "symbolic-ref", "HEAD", "refs/heads/main")

	mustRunGit(t, root, "clone", origin, workspace)
	mustRunGit(t, workspace, "checkout", "-b", "feature/new-version")
	if err := os.WriteFile(filepath.Join(workspace, "local-only.tmp"), []byte("tmp"), 0644); err != nil {
		t.Fatal(err)
	}

	asserts := config.AssertConfig{
		Git: &config.GitAssert{
			Origin: &config.GitRepoAssert{
				Commits: 1,
				LastCommit: &config.GitLastCommitAssert{
					AuthorName:  "Alice Example",
					AuthorEmail: "alice@example.com",
					Message:     "/chore: update.*/",
				},
				File: map[string]config.ArtifactAssert{
					"config.txt": {
						Exists:   boolPtr(true),
						Contents: []any{"version: 2.0.0"},
					},
				},
			},
			Workspace: &config.GitRepoAssert{
				Branch: "feature/new-version",
				Clean:  boolPtr(false),
				File: map[string]config.ArtifactAssert{
					"local-only.tmp": {
						Exists: boolPtr(true),
					},
				},
			},
		},
	}

	results := Run(asserts, AssertContext{
		OriginRepoPath: origin,
		WorkspacePath:  workspace,
	})

	for _, result := range results {
		if !result.Passed {
			t.Fatalf("unexpected failure: %+v", result)
		}
	}
}

func mustRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := runGit(dir, args...); err != nil {
		t.Fatal(err)
	}
}

func boolPtr(value bool) *bool {
	return &value
}
