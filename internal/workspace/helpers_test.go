package workspace

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/parser"
)

func TestWorkspaceHelpers(t *testing.T) {
	t.Run("Destroy keep workspace", func(t *testing.T) {
		dir := t.TempDir()
		w := &Workspace{Dir: dir, KeepWorkspace: true}

		originalStdout := os.Stdout
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = writer
		defer func() { os.Stdout = originalStdout }()

		if err := w.Destroy(); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		if _, err := out.ReadFrom(reader); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Test workspace preserved at:") {
			t.Fatalf("Destroy output = %q", out.String())
		}
	})

	t.Run("runCmd failure", func(t *testing.T) {
		if err := runCmd(t.TempDir(), "git", "definitely-invalid-subcommand"); err == nil {
			t.Fatal("expected runCmd to fail")
		}
	})

	t.Run("getDefaultBranchFromRepo reads origin/HEAD", func(t *testing.T) {
		repo := initGitRepo(t)
		mustRunGitWorkspace(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
		if branch := getDefaultBranchFromRepo(repo, nil); branch != "main" {
			t.Fatalf("getDefaultBranchFromRepo(origin HEAD) = %q", branch)
		}

		if branch := getDefaultBranchFromRepo(t.TempDir(), nil); branch != "" {
			t.Fatalf("getDefaultBranchFromRepo(no origin) = %q, want empty", branch)
		}
	})

	t.Run("commitIfStaged", func(t *testing.T) {
		env := noSignGitEnv(t)
		repo := initGitRepo(t)
		if err := commitIfStaged(repo, "nothing to commit", env); err != nil {
			t.Fatalf("commitIfStaged no-op: %v", err)
		}

		path := filepath.Join(repo, "file.txt")
		if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := commitIfStaged(repo, "add file", env); err != nil {
			t.Fatalf("commitIfStaged commit: %v", err)
		}
		log := runGitOutput(t, repo, "log", "--oneline")
		if !strings.Contains(log, "add file") {
			t.Fatalf("git log = %q", log)
		}

		if err := commitIfStaged(t.TempDir(), "bad repo", nil); err == nil {
			t.Fatal("expected commitIfStaged to fail outside repo")
		}
	})

	t.Run("removeRemoteIfExists", func(t *testing.T) {
		repo := initGitRepo(t)
		remoteRepo := filepath.Join(t.TempDir(), "remote.git")
		mustRunGitWorkspace(t, t.TempDir(), "init", "--bare", remoteRepo)

		if err := removeRemoteIfExists(repo, "origin", nil); err != nil {
			t.Fatalf("removeRemoteIfExists missing remote: %v", err)
		}
		mustRunGitWorkspace(t, repo, "remote", "add", "origin", remoteRepo)
		if err := removeRemoteIfExists(repo, "origin", nil); err != nil {
			t.Fatalf("removeRemoteIfExists existing remote: %v", err)
		}
		out := runGitOutput(t, repo, "remote")
		if strings.Contains(out, "origin") {
			t.Fatalf("remote still present: %q", out)
		}
	})

	t.Run("copyDir", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "src")
		dst := filepath.Join(t.TempDir(), "dst")
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
			t.Fatal(err)
		}
		if string(data) != "data" {
			t.Fatalf("copied content = %q", string(data))
		}

		if err := copyDir(filepath.Join(src, "missing"), dst); err == nil {
			t.Fatal("expected copyDir to fail for missing source")
		}
	})
}

func TestCIDefaultBranchResolution(t *testing.T) {
	newSrc := func(t *testing.T) string {
		t.Helper()
		src := t.TempDir()
		if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hello"), 0644); err != nil {
			t.Fatal(err)
		}
		return src
	}
	newWorkspace := func(t *testing.T, setup parser.SetupConfig, src string) *Workspace {
		t.Helper()
		w, err := New(setup, false, src, Options{HostEnv: noSignGitEnv(t)})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		t.Cleanup(func() { _ = w.Destroy() })
		return w
	}
	envVars := func(w *Workspace, setup parser.SetupConfig) map[string]string {
		return w.EnvVars(setup, 8080, "abc123abc123abc123abc123abc123abc123abc123", "abc123ab", "test")
	}

	t.Run("setup.default_branch takes priority", func(t *testing.T) {
		setup := parser.SetupConfig{DefaultBranch: "release"}
		w := newWorkspace(t, setup, newSrc(t))
		env := envVars(w, setup)
		if env["CI_DEFAULT_BRANCH"] != "release" {
			t.Errorf("CI_DEFAULT_BRANCH = %q, want release", env["CI_DEFAULT_BRANCH"])
		}
	})

	t.Run("setup.default_branch overrides api.project.default_branch", func(t *testing.T) {
		setup := parser.SetupConfig{
			DefaultBranch: "release",
			API: &parser.APISetupConfig{
				Project: &parser.ProjectConfig{DefaultBranch: "master"},
			},
		}
		w := newWorkspace(t, setup, newSrc(t))
		env := envVars(w, setup)
		if env["CI_DEFAULT_BRANCH"] != "release" {
			t.Errorf("CI_DEFAULT_BRANCH = %q, want release", env["CI_DEFAULT_BRANCH"])
		}
	})

	t.Run("api.project.default_branch used when setup.default_branch absent", func(t *testing.T) {
		setup := parser.SetupConfig{
			API: &parser.APISetupConfig{
				Project: &parser.ProjectConfig{DefaultBranch: "master"},
			},
		}
		w := newWorkspace(t, setup, newSrc(t))
		env := envVars(w, setup)
		if env["CI_DEFAULT_BRANCH"] != "master" {
			t.Errorf("CI_DEFAULT_BRANCH = %q, want master", env["CI_DEFAULT_BRANCH"])
		}
	})

	t.Run("git.origin.branch does not influence CI_DEFAULT_BRANCH", func(t *testing.T) {
		// Regression: previously setting git.origin.branch to a feature branch
		// caused CI_DEFAULT_BRANCH to equal the feature branch name.
		setup := parser.SetupConfig{
			Branch: "feature/my-feature",
			Git: &parser.GitSetupConfig{
				Origin: &parser.GitOriginConfig{Branch: "feature/my-feature"},
			},
		}
		w := newWorkspace(t, setup, newSrc(t))
		env := envVars(w, setup)
		if env["CI_DEFAULT_BRANCH"] != config.DefaultBranchName {
			t.Errorf("CI_DEFAULT_BRANCH = %q, want %q", env["CI_DEFAULT_BRANCH"], config.DefaultBranchName)
		}
		if env["CI_COMMIT_BRANCH"] != "feature/my-feature" {
			t.Errorf("CI_COMMIT_BRANCH = %q, want feature/my-feature", env["CI_COMMIT_BRANCH"])
		}
	})

	t.Run("auto-detected from source repo origin/HEAD", func(t *testing.T) {
		// Source repo has refs/remotes/origin/HEAD pointing to "develop".
		srcRoot := t.TempDir()
		src := filepath.Join(srcRoot, "project")
		remote := filepath.Join(srcRoot, "remote.git")
		if err := os.MkdirAll(src, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hello"), 0644); err != nil {
			t.Fatal(err)
		}
		mustRunGitWorkspace(t, srcRoot, "init", "--bare", remote)
		mustRunGitWorkspace(t, src, "init")
		mustRunGitWorkspace(t, src, "config", "user.email", "test@example.com")
		mustRunGitWorkspace(t, src, "config", "user.name", "Test")
		mustRunGitWorkspace(t, src, "add", "README.md")
		mustRunGitWorkspace(t, src, "commit", "-m", "init")
		mustRunGitWorkspace(t, src, "branch", "-M", "develop")
		mustRunGitWorkspace(t, src, "remote", "add", "origin", remote)
		mustRunGitWorkspace(t, src, "push", "-u", "origin", "develop")
		// Set refs/remotes/origin/HEAD in the source repo (simulates a cloned repo).
		mustRunGitWorkspace(t, src, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/develop")

		setup := parser.SetupConfig{}
		w := newWorkspace(t, setup, src)
		env := envVars(w, setup)
		if env["CI_DEFAULT_BRANCH"] != "develop" {
			t.Errorf("CI_DEFAULT_BRANCH = %q, want develop", env["CI_DEFAULT_BRANCH"])
		}
	})

	t.Run("fallback to main when no origin/HEAD in source repo", func(t *testing.T) {
		setup := parser.SetupConfig{}
		w := newWorkspace(t, setup, newSrc(t))
		env := envVars(w, setup)
		if env["CI_DEFAULT_BRANCH"] != config.DefaultBranchName {
			t.Errorf("CI_DEFAULT_BRANCH = %q, want %q", env["CI_DEFAULT_BRANCH"], config.DefaultBranchName)
		}
	})
}

func TestResolveDefaultBranch(t *testing.T) {
	t.Run("setup.default_branch wins", func(t *testing.T) {
		got := resolveDefaultBranch(parser.SetupConfig{
			DefaultBranch: "release",
			API:           &parser.APISetupConfig{Project: &parser.ProjectConfig{DefaultBranch: "master"}},
		}, t.TempDir(), nil)
		if got != "release" {
			t.Fatalf("got %q, want release", got)
		}
	})

	t.Run("api.project.default_branch used when setup.default_branch absent", func(t *testing.T) {
		got := resolveDefaultBranch(parser.SetupConfig{
			API: &parser.APISetupConfig{Project: &parser.ProjectConfig{DefaultBranch: "master"}},
		}, t.TempDir(), nil)
		if got != "master" {
			t.Fatalf("got %q, want master", got)
		}
	})

	t.Run("detects from source repo origin/HEAD", func(t *testing.T) {
		repo := initGitRepo(t)
		remote := filepath.Join(t.TempDir(), "remote.git")
		mustRunGitWorkspace(t, t.TempDir(), "init", "--bare", remote)
		mustRunGitWorkspace(t, repo, "remote", "add", "origin", remote)
		mustRunGitWorkspace(t, repo, "push", "-u", "origin", "main")
		// Set refs/remotes/origin/HEAD in the source repo itself (not the bare remote).
		mustRunGitWorkspace(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

		if got := getDefaultBranchFromRepo(repo, nil); got != "main" {
			t.Fatalf("getDefaultBranchFromRepo = %q, want main", got)
		}

		got := resolveDefaultBranch(parser.SetupConfig{}, repo, nil)
		if got != "main" {
			t.Fatalf("resolveDefaultBranch via srcDir = %q, want main", got)
		}
	})

	t.Run("fallback to main when no origin configured", func(t *testing.T) {
		got := resolveDefaultBranch(parser.SetupConfig{}, t.TempDir(), nil)
		if got != config.DefaultBranchName {
			t.Fatalf("got %q, want %q", got, config.DefaultBranchName)
		}
	})
}

// TestGetDefaultBranchFromRepoUsesHostEnv guards against getDefaultBranchFromRepo
// resolving "git" from the real process PATH/env instead of a caller-supplied
// hostEnv, like every other git call in this file does via resolveExecutable.
// With a custom PATH, this used to silently resolve a different git (or
// fall back to "main" if none was found) than the one gitlab-ci-local uses.
func TestGetDefaultBranchFromRepoUsesHostEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake git script targets POSIX sh")
	}
	binDir := t.TempDir()
	fakeGit := "#!/bin/sh\necho refs/remotes/origin/from-hostenv\n"
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(fakeGit), 0755); err != nil {
		t.Fatal(err)
	}
	hostEnv := []string{"PATH=" + binDir}

	got := getDefaultBranchFromRepo(t.TempDir(), hostEnv)
	if got != "from-hostenv" {
		t.Fatalf("getDefaultBranchFromRepo(hostEnv) = %q, want %q (the hostEnv's git, not the real PATH's)", got, "from-hostenv")
	}
}

func TestWorkspaceEnvHelperBranches(t *testing.T) {
	t.Run("defaultBranch backward compat: api override on direct-constructed workspace", func(t *testing.T) {
		// Workspace created directly (not via New()) still honours
		// api.project.default_branch as fallback when w.DefaultBranch is empty.
		w := &Workspace{WorkspaceDir: t.TempDir()}
		if got := w.defaultBranch(parser.SetupConfig{
			API: &parser.APISetupConfig{Project: &parser.ProjectConfig{DefaultBranch: "release"}},
		}); got != "release" {
			t.Fatalf("defaultBranch override = %q", got)
		}

		w2 := &Workspace{WorkspaceDir: t.TempDir()}
		if got := w2.defaultBranch(parser.SetupConfig{}); got != config.DefaultBranchName {
			t.Fatalf("defaultBranch fallback = %q", got)
		}
	})

	t.Run("baseEnv workspace fallback", func(t *testing.T) {
		w := &Workspace{Dir: "/tmp/work", OriginRepo: "/tmp/origin.git"}
		env := w.baseEnv(8080, "sha", "short", "name", "main")
		if env["GLUT_WORKSPACE"] != "/tmp/work" {
			t.Fatalf("GLUT_WORKSPACE = %q", env["GLUT_WORKSPACE"])
		}
	})

	t.Run("applyPipelineEnv variants", func(t *testing.T) {
		env := map[string]string{"CI_PROJECT_PATH": "group/project"}
		applyPipelineEnv(env, parser.SetupConfig{
			PipelineSource: config.PipelineSourceSchedule,
			Schedule:       &parser.ScheduleConfig{Description: "nightly"},
		}, "main")
		if env["CI_PIPELINE_SCHEDULE"] != "true" || env["CI_SCHEDULE_DESCRIPTION"] != "nightly" {
			t.Fatalf("schedule env = %#v", env)
		}

		env = map[string]string{"CI_PROJECT_PATH": "group/project"}
		applyPipelineEnv(env, parser.SetupConfig{PipelineSource: config.PipelineSourceTrigger}, "main")
		if env["CI_PIPELINE_TRIGGERED"] != "true" || env["CI_TRIGGER_SHORT_TOKEN"] != "glut" {
			t.Fatalf("trigger env = %#v", env)
		}

		env = map[string]string{"CI_PROJECT_PATH": "group/project"}
		applyPipelineEnv(env, parser.SetupConfig{
			PipelineSource: config.PipelineSourceParent,
			Upstream: &parser.UpstreamConfig{
				PipelineID: 1,
				ProjectID:  2,
				JobID:      3,
			},
		}, "main")
		if env["CI_UPSTREAM_PIPELINE_ID"] != "1" || env["CI_UPSTREAM_PROJECT_ID"] != "2" || env["CI_UPSTREAM_JOB_ID"] != "3" {
			t.Fatalf("parent env = %#v", env)
		}

		env = map[string]string{"CI_PROJECT_PATH": "group/project"}
		applyPipelineEnv(env, parser.SetupConfig{
			PipelineSource: config.PipelineSourceChat,
			Chat:           &parser.ChatConfig{Input: "deploy", Channel: "ops"},
		}, "main")
		if env["CI_CHAT_INPUT"] != "deploy" || env["CI_CHAT_CHANNEL"] != "ops" {
			t.Fatalf("chat env = %#v", env)
		}
	})
}

func TestMockBinaryHelpers(t *testing.T) {
	t.Run("SetupMockBinaries no mocks", func(t *testing.T) {
		if err := SetupMockBinaries(t.TempDir(), parser.MocksConfig{}, "missing", false); err != nil {
			t.Fatalf("expected no-op, got %v", err)
		}
	})

	t.Run("SetupMockBinaries invalid binary path", func(t *testing.T) {
		mocks := parser.MocksConfig{Binaries: map[string]parser.BinaryMockConfig{"tool": {Executable: "echo ok"}}}
		err := SetupMockBinaries(t.TempDir(), mocks, filepath.Join(t.TempDir(), "missing-glut"), false)
		if err == nil {
			t.Fatal("expected invalid glut path error")
		}
	})

	t.Run("SetupMockBinaries directory create failure", func(t *testing.T) {
		parent := t.TempDir()
		tmpWork := filepath.Join(parent, "file")
		if err := os.WriteFile(tmpWork, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		glutBin := fakeGlutBinary(t, parent)
		mocks := parser.MocksConfig{Binaries: map[string]parser.BinaryMockConfig{"tool": {Executable: "echo ok"}}}
		err := SetupMockBinaries(tmpWork, mocks, glutBin, false)
		if err == nil || !strings.Contains(err.Error(), "create mock binary directory") {
			t.Fatalf("SetupMockBinaries() error = %v", err)
		}
	})

	t.Run("SetupMockBinaries invalid name", func(t *testing.T) {
		tmp := t.TempDir()
		glutBin := fakeGlutBinary(t, tmp)
		mocks := parser.MocksConfig{Binaries: map[string]parser.BinaryMockConfig{"../bad": {Executable: "echo ok"}}}
		err := SetupMockBinaries(tmp, mocks, glutBin, false)
		if err == nil || !strings.Contains(err.Error(), "must not contain path separators") {
			t.Fatalf("SetupMockBinaries() error = %v", err)
		}
	})

	t.Run("validateMockBinaryName", func(t *testing.T) {
		for _, name := range []string{"", ".", "..", "a/b", `a\b`} {
			if err := validateMockBinaryName(name); err == nil {
				t.Fatalf("expected invalid name %q", name)
			}
		}
		if err := validateMockBinaryName("release-cli"); err != nil {
			t.Fatalf("valid name rejected: %v", err)
		}
	})

	t.Run("shellScript", func(t *testing.T) {
		if got := shellScript("#!/bin/bash\necho ok\n"); got != "#!/bin/bash\necho ok\n" {
			t.Fatalf("shellScript keep = %q", got)
		}
		if got := shellScript("#!/bin/bash\necho ok"); got != "#!/bin/bash\necho ok\n" {
			t.Fatalf("shellScript add newline = %q", got)
		}
		if got := shellScript("echo ok"); got != "#!/bin/sh\necho ok\n" {
			t.Fatalf("shellScript wrap = %q", got)
		}
	})

	t.Run("PrependMockBinaryPath empty", func(t *testing.T) {
		tmp := t.TempDir()
		if got := PrependMockBinaryPath(tmp, ""); got != MockBinaryBinDir(tmp) {
			t.Fatalf("PrependMockBinaryPath() = %q", got)
		}
	})

	t.Run("copyExecutable", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "src")
		dst := filepath.Join(t.TempDir(), "dst")
		if err := os.WriteFile(src, []byte("abc"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := copyExecutable(src, dst); err != nil {
			t.Fatalf("copyExecutable() error = %v", err)
		}
		data, err := os.ReadFile(dst)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "abc" {
			t.Fatalf("copied data = %q", string(data))
		}

		if err := copyExecutable(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "dst")); err == nil {
			t.Fatal("expected copyExecutable to fail for missing source")
		}
		if err := copyExecutable(src, filepath.Join(t.TempDir(), "missing", "dst")); err == nil {
			t.Fatal("expected copyExecutable to fail for missing destination parent")
		}
	})

	t.Run("linkMockBinary", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("non-windows symlink branch only")
		}
		src := filepath.Join(t.TempDir(), "src")
		dst := filepath.Join(t.TempDir(), "dst")
		if err := os.WriteFile(src, []byte("abc"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := linkMockBinary(dst, src); err != nil {
			t.Fatalf("linkMockBinary() error = %v", err)
		}
		target, err := os.Readlink(dst)
		if err != nil {
			t.Fatal(err)
		}
		if target != src {
			t.Fatalf("symlink target = %q", target)
		}

		if err := linkMockBinary(filepath.Join(t.TempDir(), "missing", "dst"), src); err == nil {
			t.Fatal("expected linkMockBinary to fail with missing parent")
		}
	})
}

func TestWorkspaceCreationErrorBranches(t *testing.T) {
	t.Run("New success with custom git config", func(t *testing.T) {
		src := t.TempDir()
		if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hello"), 0644); err != nil {
			t.Fatal(err)
		}

		w, err := New(parser.SetupConfig{
			Git: &parser.GitSetupConfig{
				User: parser.GitUserConfig{
					Name:  "CI Bot",
					Email: "ci@example.com",
				},
				Origin: &parser.GitOriginConfig{
					Branch: "release",
					Files: map[string]string{
						"docs/note.txt": "hello",
					},
				},
			},
		}, false, src, Options{HostEnv: noSignGitEnv(t)})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		t.Cleanup(func() {
			if err := w.Destroy(); err != nil {
				t.Fatalf("Destroy() error = %v", err)
			}
		})

		data, err := os.ReadFile(filepath.Join(w.WorkspaceDir, "docs", "note.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "hello" {
			t.Fatalf("workspace file = %q", string(data))
		}

		head := runGitOutput(t, w.WorkspaceDir, "branch", "--show-current")
		if strings.TrimSpace(head) != "release" {
			t.Fatalf("workspace branch = %q", head)
		}
	})

	t.Run("New missing source", func(t *testing.T) {
		_, err := New(parser.SetupConfig{}, false, filepath.Join(t.TempDir(), "missing-src"), Options{})
		if err == nil {
			t.Fatal("expected New to fail for missing source")
		}
	})

	t.Run("setupGitOrigin no-op", func(t *testing.T) {
		root := t.TempDir()
		origin := filepath.Join(root, "origin.git")
		mustRunGitWorkspace(t, root, "init", "--bare", origin)

		w := &Workspace{}
		err := w.setupGitOrigin(root, origin, "main", &parser.GitSetupConfig{
			Origin: &parser.GitOriginConfig{},
		}, nil)
		if err != nil {
			t.Fatalf("setupGitOrigin no-op error = %v", err)
		}
	})

	t.Run("setupGitOrigin command failure", func(t *testing.T) {
		root := t.TempDir()
		source := initGitRepo(t)
		origin := filepath.Join(root, "origin.git")
		mustRunGitWorkspace(t, root, "init", "--bare", origin)
		mustRunGitWorkspace(t, source, "remote", "add", "origin", origin)
		mustRunGitWorkspace(t, source, "push", "-u", "origin", "main")
		mustRunGitWorkspace(t, root, "--git-dir", origin, "symbolic-ref", "HEAD", "refs/heads/main")

		w := &Workspace{}
		err := w.setupGitOrigin(root, origin, "main", &parser.GitSetupConfig{
			Origin: &parser.GitOriginConfig{
				Commands: []string{"exit 7"},
			},
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "origin command failed") {
			t.Fatalf("setupGitOrigin() error = %v", err)
		}
	})

	t.Run("setupGitOrigin clone failure", func(t *testing.T) {
		w := &Workspace{}
		err := w.setupGitOrigin(t.TempDir(), filepath.Join(t.TempDir(), "missing.git"), "main", &parser.GitSetupConfig{
			Origin: &parser.GitOriginConfig{
				Files: map[string]string{"a.txt": "x"},
			},
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "failed to clone worktree") {
			t.Fatalf("setupGitOrigin() error = %v", err)
		}
	})

	t.Run("setupGitOrigin signing fails", func(t *testing.T) {
		realGit, err := exec.LookPath("git")
		if err != nil {
			t.Skip("git not available")
		}
		root := t.TempDir()
		source := initGitRepo(t)
		origin := filepath.Join(root, "origin.git")
		mustRunGitWorkspace(t, root, "init", "--bare", origin)
		mustRunGitWorkspace(t, source, "remote", "add", "origin", origin)
		mustRunGitWorkspace(t, source, "push", "-u", "origin", "main")
		mustRunGitWorkspace(t, root, "--git-dir", origin, "symbolic-ref", "HEAD", "refs/heads/main")

		binDir := t.TempDir()
		fakeGit := filepath.Join(binDir, "git")
		script := "#!/bin/sh\nfor a in \"$@\"; do [ \"$a\" = \"commit.gpgSign\" ] && exit 7; done\nexec " + realGit + " \"$@\"\n"
		if err := os.WriteFile(fakeGit, []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
		hostEnv := []string{
			"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_CONFIG_GLOBAL=/dev/null",
			"HOME=" + t.TempDir(),
		}

		w := &Workspace{}
		err = w.setupGitOrigin(root, origin, "main", &parser.GitSetupConfig{
			Origin: &parser.GitOriginConfig{
				Files: map[string]string{"seed.txt": "content"},
			},
		}, hostEnv)
		if err == nil || !strings.Contains(err.Error(), "failed to disable origin worktree git signing") {
			t.Fatalf("setupGitOrigin() error = %v", err)
		}
	})

	t.Run("setupGitOrigin invalid branch rename", func(t *testing.T) {
		root := t.TempDir()
		source := initGitRepo(t)
		origin := filepath.Join(root, "origin.git")
		mustRunGitWorkspace(t, root, "init", "--bare", origin)
		mustRunGitWorkspace(t, source, "remote", "add", "origin", origin)
		mustRunGitWorkspace(t, source, "push", "-u", "origin", "main")
		mustRunGitWorkspace(t, root, "--git-dir", origin, "symbolic-ref", "HEAD", "refs/heads/main")

		w := &Workspace{}
		err := w.setupGitOrigin(root, origin, "main", &parser.GitSetupConfig{
			Origin: &parser.GitOriginConfig{
				Branch: "bad branch name",
				Files:  map[string]string{"hello.txt": "world"},
			},
		}, noSignGitEnv(t))
		if err == nil || !strings.Contains(err.Error(), "failed to rename origin worktree branch") {
			t.Fatalf("setupGitOrigin() error = %v", err)
		}
	})

	t.Run("New git failure via PATH override", func(t *testing.T) {
		t.Parallel()
		binDir := t.TempDir()
		fakeGit := filepath.Join(binDir, "git")
		if err := os.WriteFile(fakeGit, []byte("#!/bin/sh\nexit 7\n"), 0755); err != nil {
			t.Fatal(err)
		}
		originalPath := os.Getenv("PATH")

		src := t.TempDir()
		if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hello"), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := New(parser.SetupConfig{}, false, src, Options{
			HostEnv: []string{"PATH=" + binDir + string(os.PathListSeparator) + originalPath},
		})
		if err == nil || !strings.Contains(err.Error(), "failed to init bare origin") {
			t.Fatalf("New() error = %v", err)
		}
	})

	t.Run("New git config signing fails", func(t *testing.T) {
		t.Parallel()
		realGit, err := exec.LookPath("git")
		if err != nil {
			t.Skip("git not available")
		}
		binDir := t.TempDir()
		// Fake git: fail when asked to set commit.gpgSign; delegate all other commands
		// to the real git binary using its absolute path to avoid PATH recursion.
		fakeGit := filepath.Join(binDir, "git")
		script := "#!/bin/sh\nfor a in \"$@\"; do [ \"$a\" = \"commit.gpgSign\" ] && exit 7; done\nexec " + realGit + " \"$@\"\n"
		if err := os.WriteFile(fakeGit, []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
		src := t.TempDir()
		if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hello"), 0644); err != nil {
			t.Fatal(err)
		}
		_, err = New(parser.SetupConfig{}, false, src, Options{
			HostEnv: []string{
				"PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
				"GIT_CONFIG_NOSYSTEM=1",
				"GIT_CONFIG_GLOBAL=/dev/null",
				"HOME=" + t.TempDir(),
			},
		})
		if err == nil || !strings.Contains(err.Error(), "failed to disable staging git signing") {
			t.Fatalf("New() error = %v", err)
		}
	})
}

func TestNewCIVariables(t *testing.T) {
	w := &Workspace{Dir: t.TempDir(), OriginRepo: t.TempDir()}

	t.Run("server info variables", func(t *testing.T) {
		env := w.baseEnv(9090, "sha", "short", "name", "main")
		cases := map[string]string{
			"CI_SERVER_HOST":     "127.0.0.1",
			"CI_SERVER_NAME":     "GitLab",
			"CI_SERVER_VERSION":  "16.11.0",
			"CI_SERVER_REVISION": "mock",
			"CI_SERVER_PROTOCOL": "http",
			"CI_SERVER_PORT":     "9090",
			"CI_SERVER_FQDN":     "127.0.0.1:9090",
		}
		for k, want := range cases {
			if env[k] != want {
				t.Errorf("%s = %q, want %q", k, env[k], want)
			}
		}
	})

	t.Run("ApplyServerBaseURL repoints server family and derived URLs", func(t *testing.T) {
		env := w.EnvVars(parser.SetupConfig{}, 8080, "sha", "short", "name")
		env["CI_MERGE_REQUEST_SOURCE_PROJECT_URL"] = "http://127.0.0.1:8080/test-group/test-project"
		ApplyServerBaseURL(env, "172.17.0.2", 8080)
		cases := map[string]string{
			"CI_SERVER_URL":                                 "http://172.17.0.2:8080",
			"CI_API_V4_URL":                                 "http://172.17.0.2:8080/api/v4",
			"CI_SERVER_HOST":                                "172.17.0.2",
			"CI_SERVER_PORT":                                "8080",
			"CI_SERVER_FQDN":                                "172.17.0.2:8080",
			"CI_PROJECT_URL":                                "http://172.17.0.2:8080/test-group/test-project",
			"CI_PIPELINE_URL":                               "http://172.17.0.2:8080/test-group/test-project/-/pipelines/1",
			"CI_JOB_URL":                                    "http://172.17.0.2:8080/test-group/test-project/-/jobs/1",
			"CI_MERGE_REQUEST_SOURCE_PROJECT_URL":           "http://172.17.0.2:8080/test-group/test-project",
			"CI_REPOSITORY_URL":                             "http://gitlab-ci-token:mock-job-token@172.17.0.2:8080/test-group/test-project.git",
			"CI_DEPENDENCY_PROXY_SERVER":                    "172.17.0.2:8080",
			"CI_DEPENDENCY_PROXY_GROUP_IMAGE_PREFIX":        "172.17.0.2:8080/test-group/dependency_proxy/containers",
			"CI_DEPENDENCY_PROXY_DIRECT_GROUP_IMAGE_PREFIX": "172.17.0.2:8080/test-group/dependency_proxy/containers",
		}
		for k, want := range cases {
			if env[k] != want {
				t.Errorf("%s = %q, want %q", k, env[k], want)
			}
		}
	})

	t.Run("project identity variables avoid gitlab-ci-local fallbacks", func(t *testing.T) {
		env := w.EnvVars(parser.SetupConfig{}, 8080, "sha", "short", "name")
		cases := map[string]string{
			"CI_PROJECT_PATH_SLUG":                   "test-group-test-project",
			"CI_PROJECT_ROOT_NAMESPACE":              "test-group",
			"CI_PROJECT_TITLE":                       "test-project",
			"CI_PIPELINE_IID":                        "1",
			"GITLAB_CI":                              "true",
			"CI_DEPENDENCY_PROXY_SERVER":             "127.0.0.1:8080",
			"CI_DEPENDENCY_PROXY_USER":               "gitlab-ci-token",
			"CI_DEPENDENCY_PROXY_PASSWORD":           "mock-job-token",
			"CI_DEPENDENCY_PROXY_GROUP_IMAGE_PREFIX": "127.0.0.1:8080/test-group/dependency_proxy/containers",
		}
		for k, want := range cases {
			if env[k] != want {
				t.Errorf("%s = %q, want %q", k, env[k], want)
			}
		}
	})

	t.Run("project identity variables use custom project path", func(t *testing.T) {
		setup := parser.SetupConfig{
			API: &parser.APISetupConfig{
				Project: &parser.ProjectConfig{Path: "acme/sub/backend"},
			},
		}
		env := w.EnvVars(setup, 8080, "sha", "short", "name")
		cases := map[string]string{
			"CI_PROJECT_PATH_SLUG":                   "acme-sub-backend",
			"CI_PROJECT_ROOT_NAMESPACE":              "acme",
			"CI_PROJECT_TITLE":                       "backend",
			"CI_DEPENDENCY_PROXY_GROUP_IMAGE_PREFIX": "127.0.0.1:8080/acme/dependency_proxy/containers",
		}
		for k, want := range cases {
			if env[k] != want {
				t.Errorf("%s = %q, want %q", k, env[k], want)
			}
		}
	})

	t.Run("explicit project title overrides derived one", func(t *testing.T) {
		setup := parser.SetupConfig{
			API: &parser.APISetupConfig{
				Project: &parser.ProjectConfig{Title: "My Fancy Project"},
			},
		}
		env := w.EnvVars(setup, 8080, "sha", "short", "name")
		if env["CI_PROJECT_TITLE"] != "My Fancy Project" {
			t.Errorf("CI_PROJECT_TITLE = %q, want My Fancy Project", env["CI_PROJECT_TITLE"])
		}
		// Title alone must not disturb path-derived variables.
		if env["CI_PROJECT_PATH_SLUG"] != "test-group-test-project" {
			t.Errorf("CI_PROJECT_PATH_SLUG = %q", env["CI_PROJECT_PATH_SLUG"])
		}

		setup.API.Project.Path = "acme/backend"
		env = w.EnvVars(setup, 8080, "sha", "short", "name")
		if env["CI_PROJECT_TITLE"] != "My Fancy Project" {
			t.Errorf("CI_PROJECT_TITLE with custom path = %q, want My Fancy Project", env["CI_PROJECT_TITLE"])
		}
	})

	t.Run("ApplyCommitEnv splits title and description", func(t *testing.T) {
		env := map[string]string{}
		ApplyCommitEnv(env, "feat: add thing\n\nLonger body\nsecond line", "2026-06-10T12:00:00+02:00")
		cases := map[string]string{
			"CI_COMMIT_MESSAGE":     "feat: add thing\n\nLonger body\nsecond line",
			"CI_COMMIT_TITLE":       "feat: add thing",
			"CI_COMMIT_DESCRIPTION": "Longer body\nsecond line",
			"CI_COMMIT_TIMESTAMP":   "2026-06-10T12:00:00+02:00",
		}
		for k, want := range cases {
			if env[k] != want {
				t.Errorf("%s = %q, want %q", k, env[k], want)
			}
		}
	})

	t.Run("ApplyCommitEnv with single-line message", func(t *testing.T) {
		env := map[string]string{}
		ApplyCommitEnv(env, "fix: one liner", "")
		if env["CI_COMMIT_TITLE"] != "fix: one liner" {
			t.Errorf("CI_COMMIT_TITLE = %q", env["CI_COMMIT_TITLE"])
		}
		if env["CI_COMMIT_DESCRIPTION"] != "" {
			t.Errorf("CI_COMMIT_DESCRIPTION = %q, want empty", env["CI_COMMIT_DESCRIPTION"])
		}
		if _, ok := env["CI_COMMIT_TIMESTAMP"]; ok {
			t.Error("CI_COMMIT_TIMESTAMP should not be set for empty timestamp")
		}
	})

	t.Run("job and user ID variables", func(t *testing.T) {
		env := w.baseEnv(9090, "sha", "short", "name", "main")
		if env["CI_JOB_ID"] != "1" {
			t.Errorf("CI_JOB_ID = %q, want 1", env["CI_JOB_ID"])
		}
		if env["GITLAB_USER_ID"] != "1" {
			t.Errorf("GITLAB_USER_ID = %q, want 1", env["GITLAB_USER_ID"])
		}
	})

	t.Run("derived URL variables are set after EnvVars", func(t *testing.T) {
		env := w.EnvVars(parser.SetupConfig{}, 8080, "sha", "short", "name")
		if env["CI_PROJECT_URL"] != "http://127.0.0.1:8080/test-group/test-project" {
			t.Errorf("CI_PROJECT_URL = %q", env["CI_PROJECT_URL"])
		}
		if env["CI_PIPELINE_URL"] != "http://127.0.0.1:8080/test-group/test-project/-/pipelines/1" {
			t.Errorf("CI_PIPELINE_URL = %q", env["CI_PIPELINE_URL"])
		}
		if env["CI_JOB_URL"] != "http://127.0.0.1:8080/test-group/test-project/-/jobs/1" {
			t.Errorf("CI_JOB_URL = %q", env["CI_JOB_URL"])
		}
		wantRepo := "http://gitlab-ci-token:mock-job-token@127.0.0.1:8080/test-group/test-project.git"
		if env["CI_REPOSITORY_URL"] != wantRepo {
			t.Errorf("CI_REPOSITORY_URL = %q, want %q", env["CI_REPOSITORY_URL"], wantRepo)
		}
	})

	t.Run("derived URL variables use custom project path", func(t *testing.T) {
		setup := parser.SetupConfig{
			API: &parser.APISetupConfig{
				Project: &parser.ProjectConfig{Path: "acme/backend"},
			},
		}
		env := w.EnvVars(setup, 8080, "sha", "short", "name")
		if env["CI_PROJECT_URL"] != "http://127.0.0.1:8080/acme/backend" {
			t.Errorf("CI_PROJECT_URL with custom path = %q", env["CI_PROJECT_URL"])
		}
		wantRepo := "http://gitlab-ci-token:mock-job-token@127.0.0.1:8080/acme/backend.git"
		if env["CI_REPOSITORY_URL"] != wantRepo {
			t.Errorf("CI_REPOSITORY_URL with custom path = %q, want %q", env["CI_REPOSITORY_URL"], wantRepo)
		}
	})

	t.Run("MR pipeline new variables", func(t *testing.T) {
		iid := 7
		env := map[string]string{
			"CI_SERVER_URL":   "http://127.0.0.1:8080",
			"CI_PROJECT_PATH": "myorg/myapp",
		}
		setup := parser.SetupConfig{
			Branch:         "feature/x",
			PipelineSource: config.PipelineSourceMR,
			MergeRequest: &parser.MRConfig{
				IID:         iid,
				Title:       "Add feature",
				Description: "implements X",
				Milestone:   "v2.0",
				Squash:      true,
			},
		}
		applyMergeRequestEnv(env, setup)

		cases := map[string]string{
			"CI_MERGE_REQUEST_DESCRIPTION":         "implements X",
			"CI_MERGE_REQUEST_MILESTONE":           "v2.0",
			"CI_MERGE_REQUEST_SQUASH":              "true",
			"CI_MERGE_REQUEST_APPROVED":            "false",
			"CI_MERGE_REQUEST_EVENT_TYPE":          "detached",
			"CI_MERGE_REQUEST_DIFF_BASE_SHA":       "0000000000000000000000000000000000000000",
			"CI_MERGE_REQUEST_SOURCE_PROJECT_ID":   "1",
			"CI_MERGE_REQUEST_SOURCE_PROJECT_PATH": "myorg/myapp",
			"CI_MERGE_REQUEST_SOURCE_PROJECT_URL":  "http://127.0.0.1:8080/myorg/myapp",
			// Real GitLab sets CI_COMMIT_BEFORE_SHA (zero SHA) in MR pipelines
			// too; applyBranchOrTagEnv (which normally sets it) is skipped
			// entirely for MR pipelines, so this must be set here instead.
			"CI_COMMIT_BEFORE_SHA": "0000000000000000000000000000000000000000",
		}
		for k, want := range cases {
			if env[k] != want {
				t.Errorf("%s = %q, want %q", k, env[k], want)
			}
		}
	})

	t.Run("MR squash defaults to false", func(t *testing.T) {
		env := map[string]string{
			"CI_SERVER_URL":   "http://127.0.0.1:8080",
			"CI_PROJECT_PATH": "g/p",
		}
		applyMergeRequestEnv(env, parser.SetupConfig{
			Branch:       "feature/y",
			MergeRequest: &parser.MRConfig{IID: 1},
		})
		if env["CI_MERGE_REQUEST_SQUASH"] != "false" {
			t.Errorf("CI_MERGE_REQUEST_SQUASH = %q, want false", env["CI_MERGE_REQUEST_SQUASH"])
		}
	})

	t.Run("CI_MERGE_REQUEST_APPROVED configurable", func(t *testing.T) {
		env := map[string]string{
			"CI_SERVER_URL":   "http://127.0.0.1:8080",
			"CI_PROJECT_PATH": "g/p",
		}
		applyMergeRequestEnv(env, parser.SetupConfig{
			Branch:       "feature/z",
			MergeRequest: &parser.MRConfig{IID: 1, Approved: true},
		})
		if env["CI_MERGE_REQUEST_APPROVED"] != "true" {
			t.Errorf("CI_MERGE_REQUEST_APPROVED = %q, want true", env["CI_MERGE_REQUEST_APPROVED"])
		}
	})

	t.Run("CI_MERGE_REQUEST_EVENT_TYPE configurable", func(t *testing.T) {
		env := map[string]string{
			"CI_SERVER_URL":   "http://127.0.0.1:8080",
			"CI_PROJECT_PATH": "g/p",
		}
		applyMergeRequestEnv(env, parser.SetupConfig{
			Branch:       "feature/z",
			MergeRequest: &parser.MRConfig{IID: 1, EventType: "merged_result"},
		})
		if env["CI_MERGE_REQUEST_EVENT_TYPE"] != "merged_result" {
			t.Errorf("CI_MERGE_REQUEST_EVENT_TYPE = %q, want merged_result", env["CI_MERGE_REQUEST_EVENT_TYPE"])
		}
	})

	t.Run("CI_MERGE_REQUEST_DIFF_BASE_SHA configurable", func(t *testing.T) {
		env := map[string]string{
			"CI_SERVER_URL":   "http://127.0.0.1:8080",
			"CI_PROJECT_PATH": "g/p",
		}
		applyMergeRequestEnv(env, parser.SetupConfig{
			Branch:       "feature/z",
			MergeRequest: &parser.MRConfig{IID: 1, DiffBaseSHA: "deadbeef1234"},
		})
		if env["CI_MERGE_REQUEST_DIFF_BASE_SHA"] != "deadbeef1234" {
			t.Errorf("CI_MERGE_REQUEST_DIFF_BASE_SHA = %q, want deadbeef1234", env["CI_MERGE_REQUEST_DIFF_BASE_SHA"])
		}
	})

	t.Run("MR fields default when merge_request is nil", func(t *testing.T) {
		env := map[string]string{
			"CI_SERVER_URL":   "http://127.0.0.1:8080",
			"CI_PROJECT_PATH": "g/p",
		}
		applyMergeRequestEnv(env, parser.SetupConfig{Branch: "feature/z"})
		if env["CI_MERGE_REQUEST_APPROVED"] != "false" {
			t.Errorf("CI_MERGE_REQUEST_APPROVED with nil MR = %q, want false", env["CI_MERGE_REQUEST_APPROVED"])
		}
		if env["CI_MERGE_REQUEST_EVENT_TYPE"] != "detached" {
			t.Errorf("CI_MERGE_REQUEST_EVENT_TYPE with nil MR = %q, want detached", env["CI_MERGE_REQUEST_EVENT_TYPE"])
		}
	})

	t.Run("tag pipeline CI_COMMIT_TAG_MESSAGE", func(t *testing.T) {
		env := map[string]string{}
		setup := parser.SetupConfig{
			Tag:        "v1.2.3",
			TagMessage: "Release 1.2.3",
		}
		applyBranchOrTagEnv(env, setup, "main")
		if env["CI_COMMIT_TAG"] != "v1.2.3" {
			t.Errorf("CI_COMMIT_TAG = %q, want v1.2.3", env["CI_COMMIT_TAG"])
		}
		if env["CI_COMMIT_TAG_MESSAGE"] != "Release 1.2.3" {
			t.Errorf("CI_COMMIT_TAG_MESSAGE = %q, want Release 1.2.3", env["CI_COMMIT_TAG_MESSAGE"])
		}
	})

	t.Run("tag pipeline empty tag message", func(t *testing.T) {
		env := map[string]string{}
		applyBranchOrTagEnv(env, parser.SetupConfig{Tag: "v1.0.0"}, "main")
		if v, ok := env["CI_COMMIT_TAG_MESSAGE"]; !ok || v != "" {
			t.Errorf("CI_COMMIT_TAG_MESSAGE = %q (ok=%v), want empty string", v, ok)
		}
	})
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	mustRunGitWorkspace(t, repo, "init")
	mustRunGitWorkspace(t, repo, "config", "user.email", "test@example.com")
	mustRunGitWorkspace(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	mustRunGitWorkspace(t, repo, "add", "README.md")
	mustRunGitWorkspace(t, repo, "commit", "-m", "init")
	mustRunGitWorkspace(t, repo, "branch", "-M", "main")
	return repo
}

// noSignGitEnv returns a process env slice with git commit signing disabled.
// This is needed in environments where a global gitconfig enables signing.
func noSignGitEnv(t *testing.T) []string {
	t.Helper()
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
	// Disable system/global config and explicitly turn off commit signing,
	// which CI environments may enforce via GIT_CONFIG_COUNT env-var config.
	return append(filtered,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=commit.gpgSign",
		"GIT_CONFIG_VALUE_0=false",
	)
}

func mustRunGitWorkspace(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = noSignGitEnv(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v, output: %s", args, err, string(out))
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = noSignGitEnv(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v, output: %s", args, err, string(out))
	}
	return string(out)
}

func fakeGlutBinary(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "glut")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.WriteFile(path, []byte("fake glut"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}
