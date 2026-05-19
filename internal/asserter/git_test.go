package asserter

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
)

func TestRunGitAsserts(t *testing.T) {
	t.Parallel()
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
		OriginRepo:    NewFSOrigin(origin),
		WorkspacePath: workspace,
	})

	for _, result := range results {
		if !result.Passed {
			t.Fatalf("unexpected failure: %+v", result)
		}
	}
}

func noSignGitEnv() []string {
	filtered := make([]string, 0, len(os.Environ())+5)
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch {
		case key == "GIT_CONFIG_NOSYSTEM",
			key == "GIT_CONFIG_GLOBAL",
			key == "GIT_CONFIG_SYSTEM",
			key == "GIT_CONFIG_COUNT",
			key == "GIT_DIR",
			key == "GIT_WORK_TREE",
			key == "GIT_INDEX_FILE",
			strings.HasPrefix(key, "GIT_CONFIG_KEY_"),
			strings.HasPrefix(key, "GIT_CONFIG_VALUE_"):
			// filtered out; replaced below
		default:
			filtered = append(filtered, kv)
		}
	}
	return append(filtered,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=commit.gpgSign",
		"GIT_CONFIG_VALUE_0=false",
	)
}

func mustRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = noSignGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run git %s in %s: %v; output: %s", strings.Join(args, " "), dir, err, strings.TrimSpace(string(out)))
	}
}

func boolPtr(value bool) *bool {
	return &value
}
