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

	t.Run("git branch helpers", func(t *testing.T) {
		repo := initGitRepo(t)
		mustRunGitWorkspace(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
		if branch := getDefaultBranch(repo); branch != "main" {
			t.Fatalf("getDefaultBranch(origin HEAD) = %q", branch)
		}

		other := initGitRepo(t)
		mustRunGitWorkspace(t, other, "config", "init.defaultBranch", "trunk")
		if branch := getDefaultBranch(other); branch != "trunk" {
			t.Fatalf("getDefaultBranch(config) = %q", branch)
		}

		if branch := getDefaultBranch(t.TempDir()); branch != config.DefaultBranchName {
			t.Fatalf("getDefaultBranch(fallback) = %q", branch)
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

func TestWorkspaceEnvHelperBranches(t *testing.T) {
	t.Run("defaultBranch api override and detached fallback", func(t *testing.T) {
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
		if err := SetupMockBinaries(t.TempDir(), parser.MocksConfig{}, "missing"); err != nil {
			t.Fatalf("expected no-op, got %v", err)
		}
	})

	t.Run("SetupMockBinaries invalid binary path", func(t *testing.T) {
		mocks := parser.MocksConfig{Binaries: map[string]parser.BinaryMockConfig{"tool": {Executable: "echo ok"}}}
		err := SetupMockBinaries(t.TempDir(), mocks, filepath.Join(t.TempDir(), "missing-glut"))
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
		err := SetupMockBinaries(tmpWork, mocks, glutBin)
		if err == nil || !strings.Contains(err.Error(), "create mock binary directory") {
			t.Fatalf("SetupMockBinaries() error = %v", err)
		}
	})

	t.Run("SetupMockBinaries invalid name", func(t *testing.T) {
		tmp := t.TempDir()
		glutBin := fakeGlutBinary(t, tmp)
		mocks := parser.MocksConfig{Binaries: map[string]parser.BinaryMockConfig{"../bad": {Executable: "echo ok"}}}
		err := SetupMockBinaries(tmp, mocks, glutBin)
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
	filtered := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "GIT_CONFIG_NOSYSTEM", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM":
			// replaced below
		default:
			filtered = append(filtered, kv)
		}
	}
	return append(filtered,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
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
