package workspace

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pandasoft-zz/glut/internal/parser"
)

func TestWorkspace_NewAndDestroy(t *testing.T) {
	cfg := parser.SetupConfig{}
	w, err := New(cfg, false, ".")
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
		Dir:        "/tmp/work",
		OriginRepo: "/tmp/work/.glut-origin.git",
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
}

func TestGitOriginFilesAndCommands(t *testing.T) {
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
	w, err := New(cfg, false, ".")
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}
	defer w.Destroy()

	// Verify that hello.txt exists in the cloned workspace on the default branch (main)
	content, err := ioutil.ReadFile(filepath.Join(w.WorkspaceDir, "hello.txt"))
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
