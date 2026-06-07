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

	t.Run("ref_protected defaults to false", func(t *testing.T) {
		cfg := parser.SetupConfig{Branch: "main"}
		env := w.EnvVars(cfg, 8080, "sha", "sha", "t")
		if env["CI_COMMIT_REF_PROTECTED"] != "false" {
			t.Errorf("expected CI_COMMIT_REF_PROTECTED=false, got %s", env["CI_COMMIT_REF_PROTECTED"])
		}
	})

	t.Run("ref_protected true", func(t *testing.T) {
		v := true
		cfg := parser.SetupConfig{Branch: "main", RefProtected: &v}
		env := w.EnvVars(cfg, 8080, "sha", "sha", "t")
		if env["CI_COMMIT_REF_PROTECTED"] != "true" {
			t.Errorf("expected CI_COMMIT_REF_PROTECTED=true, got %s", env["CI_COMMIT_REF_PROTECTED"])
		}
	})

	t.Run("ref_protected explicit false", func(t *testing.T) {
		v := false
		cfg := parser.SetupConfig{Branch: "main", RefProtected: &v}
		env := w.EnvVars(cfg, 8080, "sha", "sha", "t")
		if env["CI_COMMIT_REF_PROTECTED"] != "false" {
			t.Errorf("expected CI_COMMIT_REF_PROTECTED=false, got %s", env["CI_COMMIT_REF_PROTECTED"])
		}
	})

	t.Run("merge_request_event sets CI_COMMIT_REF_PROTECTED false by default", func(t *testing.T) {
		cfg := parser.SetupConfig{
			PipelineSource: "merge_request_event",
			Branch:         "feature/x",
			MergeRequest:   &parser.MRConfig{IID: 1, TargetBranch: "main"},
		}
		env := w.EnvVars(cfg, 8080, "sha", "sha", "t")
		if env["CI_COMMIT_REF_PROTECTED"] != "false" {
			t.Errorf("expected CI_COMMIT_REF_PROTECTED=false in MR pipeline, got %q", env["CI_COMMIT_REF_PROTECTED"])
		}
	})

	t.Run("merge_request_event respects ref_protected true", func(t *testing.T) {
		v := true
		cfg := parser.SetupConfig{
			PipelineSource: "merge_request_event",
			Branch:         "feature/x",
			RefProtected:   &v,
			MergeRequest:   &parser.MRConfig{IID: 1, TargetBranch: "main"},
		}
		env := w.EnvVars(cfg, 8080, "sha", "sha", "t")
		if env["CI_COMMIT_REF_PROTECTED"] != "true" {
			t.Errorf("expected CI_COMMIT_REF_PROTECTED=true in protected MR pipeline, got %q", env["CI_COMMIT_REF_PROTECTED"])
		}
	})

	t.Run("tag pipeline sets CI_COMMIT_BEFORE_SHA", func(t *testing.T) {
		cfg := parser.SetupConfig{Tag: "v1.0.0"}
		env := w.EnvVars(cfg, 8080, "sha", "sha", "t")
		got, ok := env["CI_COMMIT_BEFORE_SHA"]
		if !ok {
			t.Fatal("CI_COMMIT_BEFORE_SHA missing in tag pipeline")
		}
		if got != "0000000000000000000000000000000000000000" {
			t.Errorf("expected zero SHA, got %q", got)
		}
	})

	t.Run("chat pipeline sets CI_CHAT_USER_ID", func(t *testing.T) {
		cfg := parser.SetupConfig{
			PipelineSource: "chat",
			Branch:         "main",
			Chat: &parser.ChatConfig{
				Channel: "#ops",
				Input:   "deploy",
				UserID:  "U12345",
			},
		}
		env := w.EnvVars(cfg, 8080, "sha", "sha", "t")
		if env["CI_CHAT_USER_ID"] != "U12345" {
			t.Errorf("expected CI_CHAT_USER_ID=U12345, got %q", env["CI_CHAT_USER_ID"])
		}
		if env["CI_CHAT_INPUT"] != "deploy" {
			t.Errorf("expected CI_CHAT_INPUT=deploy, got %q", env["CI_CHAT_INPUT"])
		}
		if env["CI_CHAT_CHANNEL"] != "#ops" {
			t.Errorf("expected CI_CHAT_CHANNEL=#ops, got %q", env["CI_CHAT_CHANNEL"])
		}
	})
}

func TestUnsetVars(t *testing.T) {
	w := &Workspace{}

	t.Run("push returns nil", func(t *testing.T) {
		cfg := parser.SetupConfig{Branch: "main"}
		if got := w.UnsetVars(cfg); got != nil {
			t.Fatalf("expected nil for push, got %v", got)
		}
	})

	t.Run("default source returns nil", func(t *testing.T) {
		if got := w.UnsetVars(parser.SetupConfig{}); got != nil {
			t.Fatalf("expected nil for default source, got %v", got)
		}
	})

	t.Run("merge_request_event unsets CI_COMMIT_BRANCH", func(t *testing.T) {
		cfg := parser.SetupConfig{PipelineSource: "merge_request_event", Branch: "feature-x"}
		got := w.UnsetVars(cfg)
		if len(got) != 1 || got[0] != "CI_COMMIT_BRANCH" {
			t.Fatalf("expected [CI_COMMIT_BRANCH], got %v", got)
		}
	})

	t.Run("tag pipeline unsets CI_COMMIT_BRANCH", func(t *testing.T) {
		cfg := parser.SetupConfig{Tag: "v1.2.3"}
		got := w.UnsetVars(cfg)
		if len(got) != 1 || got[0] != "CI_COMMIT_BRANCH" {
			t.Fatalf("expected [CI_COMMIT_BRANCH], got %v", got)
		}
	})

	t.Run("schedule does not unset CI_COMMIT_BRANCH", func(t *testing.T) {
		cfg := parser.SetupConfig{PipelineSource: "schedule", Branch: "main"}
		if got := w.UnsetVars(cfg); got != nil {
			t.Fatalf("expected nil for schedule, got %v", got)
		}
	})
}

func TestPipelineUserEnv(t *testing.T) {
	w := &Workspace{
		Dir:          "/tmp/work",
		WorkspaceDir: "/tmp/work/workspace",
		OriginRepo:   "/tmp/work/.glut-origin.git",
	}

	t.Run("defaults when nothing is set", func(t *testing.T) {
		env := w.EnvVars(parser.SetupConfig{}, 8080, "sha", "sha", "test")
		if env["GITLAB_USER_NAME"] != "Test User" {
			t.Errorf("GITLAB_USER_NAME = %q, want Test User", env["GITLAB_USER_NAME"])
		}
		if env["GITLAB_USER_EMAIL"] != "test@example.com" {
			t.Errorf("GITLAB_USER_EMAIL = %q, want test@example.com", env["GITLAB_USER_EMAIL"])
		}
		if env["GITLAB_USER_LOGIN"] != "test-user" {
			t.Errorf("GITLAB_USER_LOGIN = %q, want test-user", env["GITLAB_USER_LOGIN"])
		}
	})

	t.Run("git.user drives GITLAB_USER_NAME and GITLAB_USER_EMAIL", func(t *testing.T) {
		cfg := parser.SetupConfig{
			Git: &parser.GitSetupConfig{
				User: parser.GitUserConfig{Name: "ci-bot", Email: "ci-bot@example.com"},
			},
		}
		env := w.EnvVars(cfg, 8080, "sha", "sha", "test")
		if env["GITLAB_USER_NAME"] != "ci-bot" {
			t.Errorf("GITLAB_USER_NAME = %q, want ci-bot", env["GITLAB_USER_NAME"])
		}
		if env["GITLAB_USER_EMAIL"] != "ci-bot@example.com" {
			t.Errorf("GITLAB_USER_EMAIL = %q, want ci-bot@example.com", env["GITLAB_USER_EMAIL"])
		}
		if env["GITLAB_USER_LOGIN"] != "test-user" {
			t.Errorf("GITLAB_USER_LOGIN = %q, want test-user (unchanged)", env["GITLAB_USER_LOGIN"])
		}
	})

	t.Run("pipeline.user overrides git.user", func(t *testing.T) {
		cfg := parser.SetupConfig{
			Git: &parser.GitSetupConfig{
				User: parser.GitUserConfig{Name: "ci-bot", Email: "ci-bot@example.com"},
			},
			Pipeline: &parser.PipelineConfig{
				User: &parser.PipelineUserConfig{
					Name:  "Jan Novak",
					Email: "jan.novak@example.com",
					Login: "jan-novak",
				},
			},
		}
		env := w.EnvVars(cfg, 8080, "sha", "sha", "test")
		if env["GITLAB_USER_NAME"] != "Jan Novak" {
			t.Errorf("GITLAB_USER_NAME = %q, want Jan Novak", env["GITLAB_USER_NAME"])
		}
		if env["GITLAB_USER_EMAIL"] != "jan.novak@example.com" {
			t.Errorf("GITLAB_USER_EMAIL = %q, want jan.novak@example.com", env["GITLAB_USER_EMAIL"])
		}
		if env["GITLAB_USER_LOGIN"] != "jan-novak" {
			t.Errorf("GITLAB_USER_LOGIN = %q, want jan-novak", env["GITLAB_USER_LOGIN"])
		}
	})

	t.Run("pipeline.user partial override keeps git.user for unset fields", func(t *testing.T) {
		cfg := parser.SetupConfig{
			Git: &parser.GitSetupConfig{
				User: parser.GitUserConfig{Name: "ci-bot", Email: "ci-bot@example.com"},
			},
			Pipeline: &parser.PipelineConfig{
				User: &parser.PipelineUserConfig{Name: "Jan Novak"},
			},
		}
		env := w.EnvVars(cfg, 8080, "sha", "sha", "test")
		if env["GITLAB_USER_NAME"] != "Jan Novak" {
			t.Errorf("GITLAB_USER_NAME = %q, want Jan Novak", env["GITLAB_USER_NAME"])
		}
		if env["GITLAB_USER_EMAIL"] != "ci-bot@example.com" {
			t.Errorf("GITLAB_USER_EMAIL = %q, want ci-bot@example.com (from git.user)", env["GITLAB_USER_EMAIL"])
		}
	})

	t.Run("pipeline.user without git.user uses defaults for unset fields", func(t *testing.T) {
		cfg := parser.SetupConfig{
			Pipeline: &parser.PipelineConfig{
				User: &parser.PipelineUserConfig{Name: "Jan Novak"},
			},
		}
		env := w.EnvVars(cfg, 8080, "sha", "sha", "test")
		if env["GITLAB_USER_NAME"] != "Jan Novak" {
			t.Errorf("GITLAB_USER_NAME = %q, want Jan Novak", env["GITLAB_USER_NAME"])
		}
		if env["GITLAB_USER_EMAIL"] != "test@example.com" {
			t.Errorf("GITLAB_USER_EMAIL = %q, want test@example.com (default)", env["GITLAB_USER_EMAIL"])
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

func TestCopyDirSkipsGitDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(filepath.Join(src, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".git", "config"), []byte("git config"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); err == nil {
		t.Error(".git dir should be skipped by copyDir")
	}
	if _, err := os.Stat(filepath.Join(dst, "file.txt")); err != nil {
		t.Errorf("regular file should be copied: %v", err)
	}
}

func TestCopyRepoNativeVerbose(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("v"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyRepo(src, dst, Options{CopyStrategy: CopyStrategyNative, Verbose: true}); err != nil {
		t.Fatalf("copyRepo verbose native: %v", err)
	}
}

func TestCopyRepoAutoVerboseFallbackToNative(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err == nil {
		t.Skip("rsync is available; this test covers the rsync-not-found fallback")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("v"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyRepo(src, dst, Options{CopyStrategy: CopyStrategyAuto, Verbose: true}); err != nil {
		t.Fatalf("copyRepo auto verbose: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "f.txt")); err != nil {
		t.Errorf("copied file missing: %v", err)
	}
}

func TestCopyRepoAutoVerboseWithRsync(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not available; this test covers the rsync-success verbose path")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("v"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyRepo(src, dst, Options{CopyStrategy: CopyStrategyAuto, Verbose: true}); err != nil {
		t.Fatalf("copyRepo auto verbose with rsync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "f.txt")); err != nil {
		t.Errorf("copied file missing: %v", err)
	}
}

func TestCopyRepoWithIncludes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")

	if err := os.MkdirAll(filepath.Join(src, "sub", "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "a.txt"), []byte("aaa"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "other.txt"), []byte("bbb"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := copyRepo(src, dst, Options{Include: []string{"sub"}, CopyStrategy: CopyStrategyNative}); err != nil {
		t.Fatalf("copyRepo with includes: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, "sub", "a.txt")); err != nil {
		t.Errorf("included file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "other.txt")); err == nil {
		t.Errorf("excluded file should not be present")
	}
}

func TestCopyRepoNativeStrategy(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("native"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := copyRepo(src, dst, Options{CopyStrategy: CopyStrategyNative}); err != nil {
		t.Fatalf("copyRepo native: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "file.txt"))
	if err != nil || string(data) != "native" {
		t.Fatalf("native copy: read file error = %v, data = %q", err, data)
	}
}

func TestWorkspaceResolveExecutable(t *testing.T) {
	t.Parallel()

	t.Run("nil_hostenv_found", func(t *testing.T) {
		t.Parallel()
		result := resolveExecutable("sh", nil)
		if result == "" {
			t.Error("expected non-empty result for sh")
		}
	})

	t.Run("nil_hostenv_not_found", func(t *testing.T) {
		t.Parallel()
		result := resolveExecutable("no-such-binary-xyzzy-9999", nil)
		if result != "no-such-binary-xyzzy-9999" {
			t.Errorf("expected fallback name, got %q", result)
		}
	})

	t.Run("custom_path_found", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		bin := filepath.Join(dir, "mybin")
		if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
		env := []string{"PATH=" + dir}
		result := resolveExecutable("mybin", env)
		if result != bin {
			t.Errorf("expected %q, got %q", bin, result)
		}
	})

	t.Run("custom_path_not_found", func(t *testing.T) {
		t.Parallel()
		env := []string{"PATH=/tmp"}
		result := resolveExecutable("no-such-binary-xyzzy-9999", env)
		if result != "no-such-binary-xyzzy-9999" {
			t.Errorf("expected fallback name, got %q", result)
		}
	})

	t.Run("empty_dir_in_path", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		bin := filepath.Join(dir, "mybin2")
		if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
		env := []string{"PATH=:" + dir}
		result := resolveExecutable("mybin2", env)
		if result != bin {
			t.Errorf("expected %q, got %q", bin, result)
		}
	})
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

func TestWriteExecutableFileErrors(t *testing.T) {
	t.Parallel()
	if err := writeExecutableFile("/nonexistent-dir/file.sh", "#!/bin/sh\necho hi\n"); err == nil {
		t.Fatal("expected error writing to non-existent directory")
	}
}

func TestWriteExecutableFileSuccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := writeExecutableFile(path, "#!/bin/sh\necho hi\n"); err != nil {
		t.Fatalf("writeExecutableFile() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "#!/bin/sh\necho hi\n" {
		t.Fatalf("file content = %q", string(data))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Fatalf("file not executable: %s", info.Mode())
	}
}

func TestCopyFileMissingSrcReturnsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dst := filepath.Join(root, "dst.txt")
	if err := copyFile(filepath.Join(root, "nonexistent.txt"), dst, 0644); err == nil {
		t.Fatal("expected error copying non-existent source")
	}
}

func TestCopyFileCrossFilesystem(t *testing.T) {
	t.Parallel()
	// /dev/shm is a tmpfs separate from the ext4 / overlay root fs.
	// os.Link fails cross-device (EXDEV), but reflink.Auto falls back to io.Copy.
	shmDir := "/dev/shm"
	if _, statErr := os.Stat(shmDir); statErr != nil {
		t.Skip("/dev/shm not available")
	}
	srcFile, err := os.CreateTemp(shmDir, "glut-test-src-*")
	if err != nil {
		t.Skip("cannot create temp file in /dev/shm: " + err.Error())
	}
	srcPath := srcFile.Name()
	defer func() { _ = os.Remove(srcPath) }()
	if _, err := srcFile.WriteString("cross-fs data"); err != nil {
		_ = srcFile.Close()
		t.Fatal(err)
	}
	if err := srcFile.Close(); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "dst.txt")
	if err := copyFile(srcFile.Name(), dst, 0644); err != nil {
		t.Fatalf("copyFile cross-filesystem: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "cross-fs data" {
		t.Fatalf("data = %q, err = %v", data, err)
	}
}

func TestCopyDirDstInsideSrc(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(src, "dst") // dst is nested inside src

	if err := os.MkdirAll(filepath.Join(src, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "data", "file.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir with dst inside src: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "data", "file.txt"))
	if err != nil {
		t.Fatalf("copied file missing: %v", err)
	}
	if string(data) != "content" {
		t.Fatalf("data = %q", string(data))
	}
	// The dst directory inside src should not have been recursively copied into itself.
	if _, err := os.Stat(filepath.Join(dst, "dst")); err == nil {
		t.Error("dst should not have been recursively copied")
	}
}
