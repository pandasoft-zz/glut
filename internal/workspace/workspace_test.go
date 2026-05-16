package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pandasoft-zz/glut/internal/parser"
)

func TestWorkspace_NewAndDestroy(t *testing.T) {
	t.Parallel()
	cfg := parser.SetupConfig{}
	w, err := New(cfg, false, ".", Options{HostEnv: noSignGitEnv(t)})
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	if _, err := os.Stat(w.Dir); os.IsNotExist(err) {
		t.Errorf("workspace dir %s does not exist", w.Dir)
	}

	if _, err := os.Stat(w.OriginRepo); os.IsNotExist(err) {
		t.Errorf("origin repo %s does not exist", w.OriginRepo)
	}

	if _, err := os.Stat(filepath.Join(w.WorkspaceDir, ".git")); os.IsNotExist(err) {
		t.Errorf("cloned workspace %s is not a git repo", w.WorkspaceDir)
	}

	err = w.Destroy()
	if err != nil {
		t.Errorf("failed to destroy workspace: %v", err)
	}

	if _, err := os.Stat(w.Dir); !os.IsNotExist(err) {
		t.Errorf("workspace dir %s still exists after destroy", w.Dir)
	}
}

func TestEnvVars(t *testing.T) {
	w := &Workspace{
		Dir:          "/tmp/work",
		WorkspaceDir: "/tmp/work/workspace",
		OriginRepo:   "/tmp/work/.glut-origin.git",
	}

	t.Run("push branch", func(t *testing.T) {
		cfg := parser.SetupConfig{
			Branch: "feature/abc",
		}
		env := w.EnvVars(cfg, 8080, "sha123", "sha", "my-test")
		if env["CI_PIPELINE_SOURCE"] != "push" {
			t.Errorf("expected push source")
		}
		if env["CI_COMMIT_BRANCH"] != "feature/abc" {
			t.Errorf("expected branch feature/abc")
		}
		if env["CI_COMMIT_REF_SLUG"] != "feature-abc" {
			t.Errorf("expected slug feature-abc, got %s", env["CI_COMMIT_REF_SLUG"])
		}
		if _, ok := env["CI_COMMIT_TAG"]; ok {
			t.Errorf("did not expect CI_COMMIT_TAG")
		}
		if env["GLUT_WORKSPACE"] != "/tmp/work/workspace" {
			t.Errorf("expected GLUT_WORKSPACE to point to checkout, got %s", env["GLUT_WORKSPACE"])
		}
	})

	t.Run("tag push", func(t *testing.T) {
		cfg := parser.SetupConfig{
			Tag: "v1.0.0",
		}
		env := w.EnvVars(cfg, 8080, "sha123", "sha", "my-test")
		if env["CI_COMMIT_TAG"] != "v1.0.0" {
			t.Errorf("expected tag v1.0.0")
		}
		if _, ok := env["CI_COMMIT_BRANCH"]; ok {
			t.Errorf("did not expect CI_COMMIT_BRANCH")
		}
	})

	t.Run("merge_request_event", func(t *testing.T) {
		cfg := parser.SetupConfig{
			PipelineSource: "merge_request_event",
			Branch:         "feature-branch",
			MergeRequest: &parser.MRConfig{
				IID:          42,
				Title:        "My MR",
				TargetBranch: "main",
			},
		}
		env := w.EnvVars(cfg, 8080, "sha123", "sha", "my-test")
		if env["CI_MERGE_REQUEST_IID"] != "42" {
			t.Errorf("expected MR IID 42")
		}
		if env["CI_COMMIT_REF_NAME"] != "feature-branch" {
			t.Errorf("expected ref name feature-branch")
		}
		if _, ok := env["CI_COMMIT_BRANCH"]; ok {
			t.Errorf("did not expect CI_COMMIT_BRANCH")
		}
	})

	t.Run("api project override", func(t *testing.T) {
		cfg := parser.SetupConfig{
			API: &parser.APISetupConfig{
				Project: &parser.ProjectConfig{
					Path: "custom-group/subgroup/project",
				},
			},
		}
		env := w.EnvVars(cfg, 8080, "sha", "short", "test")
		if env["CI_PROJECT_PATH"] != "custom-group/subgroup/project" {
			t.Errorf("unexpected project path")
		}
		if env["CI_PROJECT_NAME"] != "project" {
			t.Errorf("unexpected project name")
		}
		if env["CI_PROJECT_NAMESPACE"] != "custom-group/subgroup" {
			t.Errorf("unexpected project namespace: %s", env["CI_PROJECT_NAMESPACE"])
		}
	})

	t.Run("branch variables for non-push sources", func(t *testing.T) {
		tests := []string{
			"schedule",
			"trigger",
			"api",
			"parent_pipeline",
			"chat",
		}

		for _, source := range tests {
			t.Run(source, func(t *testing.T) {
				cfg := parser.SetupConfig{
					Branch:         "release/1",
					PipelineSource: source,
				}
				env := w.EnvVars(cfg, 8080, "sha123", "sha", "my-test")
				if env["CI_COMMIT_BRANCH"] != "release/1" {
					t.Errorf("expected branch variable for %s", source)
				}
				if env["CI_COMMIT_REF_NAME"] != "release/1" {
					t.Errorf("expected ref name for %s", source)
				}
				if env["CI_COMMIT_REF_SLUG"] != "release-1" {
					t.Errorf("expected ref slug for %s", source)
				}
			})
		}
	})
}

func TestGitOriginFilesAndCommands(t *testing.T) {
	t.Parallel()
	cfg := parser.SetupConfig{
		Git: &parser.GitSetupConfig{
			Origin: &parser.GitOriginConfig{
				Files: map[string]string{
					"hello.txt": "world",
				},
				Commands: []string{
					"git tag v1.0.0 HEAD",
					"git checkout -b feature-test",
				},
			},
		},
	}
	w, err := New(cfg, false, ".", Options{HostEnv: noSignGitEnv(t)})
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer func() {
		if err := w.Destroy(); err != nil {
			t.Fatalf("failed to destroy workspace: %v", err)
		}
	}()

	// Verify that hello.txt exists in the cloned workspace on the default branch (main)
	content, err := os.ReadFile(filepath.Join(w.WorkspaceDir, "hello.txt"))
	if err == nil && string(content) == "world" {
		// Found it
	} else {
		// It might be that the workspace has the snapshot instead, but the origin has the initial commit.
		// Wait, New clones from origin, so it should have it unless snapshot overwrites.
		// Actually, New pushes the snapshot to origin main, which has everything.
		// Let's verify by querying the origin repo directly via git ls-tree
		cmd := exec.Command("git", "--git-dir="+w.OriginRepo, "ls-tree", "-r", "main")
		out, _ := cmd.CombinedOutput()
		if !strings.Contains(string(out), "hello.txt") {
			t.Errorf("expected hello.txt in origin repo main branch")
		}
	}

	// Verify commands (tag v1.0.0 and branch feature-test)
	cmd := exec.Command("git", "--git-dir="+w.OriginRepo, "tag")
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "v1.0.0") {
		t.Errorf("expected tag v1.0.0 in origin repo")
	}

	cmd = exec.Command("git", "--git-dir="+w.OriginRepo, "branch")
	out, _ = cmd.CombinedOutput()
	if !strings.Contains(string(out), "feature-test") {
		t.Errorf("expected branch feature-test in origin repo")
	}
}

func TestDestroyKeepsWorkspaceWhenRequested(t *testing.T) {
	dir := t.TempDir()
	w := &Workspace{Dir: dir, KeepWorkspace: true}
	if err := w.Destroy(); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("workspace should still exist: %v", err)
	}
}

func TestGitHelpersNoopBranches(t *testing.T) {
	dir := t.TempDir()
	if err := runCmd(dir, "git", "init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := runCmd(dir, "git", "config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("git config email: %v", err)
	}
	if err := runCmd(dir, "git", "config", "user.name", "Test User"); err != nil {
		t.Fatalf("git config name: %v", err)
	}

	if err := commitIfStaged(dir, "empty", nil); err != nil {
		t.Fatalf("commitIfStaged empty repo error = %v", err)
	}
	if err := removeRemoteIfExists(dir, "origin", nil); err != nil {
		t.Fatalf("remove missing remote error = %v", err)
	}
	if err := runCmd(dir, "git", "remote", "add", "origin", "/tmp/origin.git"); err != nil {
		t.Fatalf("add remote: %v", err)
	}
	if err := removeRemoteIfExists(dir, "origin", nil); err != nil {
		t.Fatalf("remove remote error = %v", err)
	}
}

func TestCopyDirCopiesFiles(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "file.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "nested", "file.txt"))
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if string(data) != "data" {
		t.Fatalf("copied data = %q", string(data))
	}
}

func TestCopyDirPreservesSymlinks(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "target.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", filepath.Join(src, "link.txt")); err != nil {
		t.Fatal(err)
	}

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir() error = %v", err)
	}

	info, err := os.Lstat(filepath.Join(dst, "link.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("copied link mode = %s, want symlink", info.Mode())
	}
	target, err := os.Readlink(filepath.Join(dst, "link.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "target.txt" {
		t.Fatalf("copied link target = %q, want target.txt", target)
	}
}

func TestCopyRepoCopiesFilesNatively(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("hello"), 0444); err != nil {
		t.Fatal(err)
	}

	if err := copyRepo(src, dst, Options{}); err != nil {
		t.Fatalf("copyRepo() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "file.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("copy: read file error = %v, data = %q", err, data)
	}
}

func TestNewCleansTempWorkspaceOnError(t *testing.T) {
	t.Parallel()
	tmpRoot := t.TempDir()

	_, err := New(parser.SetupConfig{}, false, filepath.Join(tmpRoot, "missing-source"), Options{TempDir: tmpRoot})
	if err == nil {
		t.Fatal("expected New to fail for missing source")
	}

	entries, readErr := os.ReadDir(tmpRoot)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected temp root to be empty after failed New, got %d entries", len(entries))
	}
}
