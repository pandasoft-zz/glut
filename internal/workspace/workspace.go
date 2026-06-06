package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/KarpelesLab/reflink"
	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/parser"
)

const tmpDirPrefix = ".glut-tmp-"

const (
	CopyStrategyAuto   = "auto"
	CopyStrategyRsync  = "rsync"
	CopyStrategyNative = "native"
)

type Options struct {
	CopyStrategy string // auto, rsync, native
	Include      []string
	Verbose      bool
	HostEnv      []string // nil = inherit process env for git/bash subprocesses
	TempDir      string   // base dir for os.MkdirTemp; empty = system default
}

type Workspace struct {
	Dir           string
	WorkspaceDir  string
	OriginRepo    string
	KeepWorkspace bool
	DefaultBranch string   // effective default branch for CI_DEFAULT_BRANCH
	hostEnv       []string // stored from Options.HostEnv for use in EnvVars
}

func New(cfg parser.SetupConfig, keepWorkspace bool, srcDir string, opts Options) (workspace *Workspace, err error) {
	srcDir, err = filepath.Abs(srcDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve srcDir: %v", err)
	}
	tmpWork, err := os.MkdirTemp(opts.TempDir, "glut-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp workspace: %v", err)
	}

	defaultBranch := resolveDefaultBranch(cfg, srcDir)

	gitBin := resolveExecutable("git", opts.HostEnv)
	runGit := func(dir string, args ...string) error {
		cmd := exec.Command(gitBin, args...)
		cmd.Dir = dir
		cmd.Env = opts.HostEnv // nil = inherit process env
		out, cmdErr := cmd.CombinedOutput()
		if cmdErr != nil {
			return fmt.Errorf("command git %v failed: %v, output: %s", args, cmdErr, string(out))
		}
		return nil
	}
	created := false
	defer func() {
		if created {
			return
		}
		if removeErr := os.RemoveAll(tmpWork); removeErr != nil && err != nil {
			err = fmt.Errorf("%w; failed to remove temp workspace %s: %v", err, tmpWork, removeErr)
		}
	}()

	workspaceDir := filepath.Join(tmpWork, "workspace")
	originRepo := filepath.Join(tmpWork, ".glut-origin.git")
	stagingDir := filepath.Join(tmpWork, "staging")

	if err := copyRepo(srcDir, stagingDir, opts); err != nil {
		return nil, fmt.Errorf("copy repository: %w", err)
	}

	if err := runGit(tmpWork, "init", "--bare", originRepo); err != nil {
		return nil, fmt.Errorf("failed to init bare origin: %v", err)
	}
	if err := runGit(stagingDir, "init"); err != nil {
		return nil, fmt.Errorf("failed to init staging repo: %v", err)
	}

	userName := config.DefaultUserName
	userEmail := config.DefaultUserEmail
	if cfg.Git != nil && cfg.Git.User.Name != "" {
		userName = cfg.Git.User.Name
	}
	if cfg.Git != nil && cfg.Git.User.Email != "" {
		userEmail = cfg.Git.User.Email
	}
	if err := runGit(stagingDir, "config", "user.email", userEmail); err != nil {
		return nil, fmt.Errorf("failed to configure staging git email: %v", err)
	}
	if err := runGit(stagingDir, "config", "user.name", userName); err != nil {
		return nil, fmt.Errorf("failed to configure staging git user: %v", err)
	}
	if err := runGit(stagingDir, "config", "commit.gpgSign", "false"); err != nil {
		return nil, fmt.Errorf("failed to disable staging git signing: %v", err)
	}

	if err := commitIfStaged(stagingDir, "glut: workspace snapshot", opts.HostEnv); err != nil {
		return nil, fmt.Errorf("failed to create workspace snapshot: %v", err)
	}

	if err := removeRemoteIfExists(stagingDir, "origin", opts.HostEnv); err != nil {
		return nil, fmt.Errorf("failed to remove existing origin remote: %v", err)
	}
	if err := runGit(stagingDir, "remote", "add", "origin", originRepo); err != nil {
		return nil, fmt.Errorf("failed to add origin remote: %v", err)
	}
	branch := config.DefaultBranchName
	if cfg.Git != nil && cfg.Git.Origin != nil && cfg.Git.Origin.Branch != "" {
		branch = cfg.Git.Origin.Branch
	}
	if err := runGit(stagingDir, "checkout", "-B", branch); err != nil {
		return nil, fmt.Errorf("failed to checkout origin branch %q: %v", branch, err)
	}
	if err := runGit(stagingDir, "push", "-f", "origin", branch); err != nil {
		return nil, fmt.Errorf("failed to push snapshot to bare origin: %v", err)
	}
	if err := runGit(tmpWork, "--git-dir", originRepo, "symbolic-ref", "HEAD", "refs/heads/"+branch); err != nil {
		return nil, fmt.Errorf("failed to set bare origin HEAD: %v", err)
	}
	if err := os.RemoveAll(stagingDir); err != nil {
		return nil, fmt.Errorf("failed to remove staging directory: %v", err)
	}

	w := &Workspace{
		Dir:           tmpWork,
		WorkspaceDir:  workspaceDir,
		OriginRepo:    originRepo,
		KeepWorkspace: keepWorkspace,
		DefaultBranch: defaultBranch,
		hostEnv:       opts.HostEnv,
	}

	if cfg.Git != nil && cfg.Git.Origin != nil {
		if err := w.setupGitOrigin(tmpWork, originRepo, branch, cfg.Git, opts.HostEnv); err != nil {
			return nil, err
		}
	}

	if err := runGit(tmpWork, "clone", originRepo, workspaceDir); err != nil {
		return nil, fmt.Errorf("failed to clone workspace: %v", err)
	}

	created = true
	return w, nil
}

func (w *Workspace) setupGitOrigin(tmpWork string, originRepo string, defaultBranch string, gitCfg *parser.GitSetupConfig, hostEnv []string) error {
	origin := gitCfg.Origin
	if len(origin.Files) == 0 && len(origin.Commands) == 0 {
		return nil
	}

	gitBin := resolveExecutable("git", hostEnv)
	runGit := func(dir string, args ...string) error {
		cmd := exec.Command(gitBin, args...)
		cmd.Dir = dir
		cmd.Env = hostEnv
		out, cmdErr := cmd.CombinedOutput()
		if cmdErr != nil {
			return fmt.Errorf("command git %v failed: %v, output: %s", args, cmdErr, string(out))
		}
		return nil
	}

	worktree := filepath.Join(tmpWork, "origin-worktree")
	if err := runGit(tmpWork, "clone", originRepo, worktree); err != nil {
		return fmt.Errorf("failed to clone worktree: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(worktree); err != nil {
			fmt.Fprintf(os.Stderr, "failed to remove origin worktree: %v\n", err)
		}
	}()

	userName := config.DefaultUserName
	userEmail := config.DefaultUserEmail
	if gitCfg.User.Name != "" {
		userName = gitCfg.User.Name
	}
	if gitCfg.User.Email != "" {
		userEmail = gitCfg.User.Email
	}
	if err := runGit(worktree, "config", "user.email", userEmail); err != nil {
		return fmt.Errorf("failed to configure origin worktree git email: %v", err)
	}
	if err := runGit(worktree, "config", "user.name", userName); err != nil {
		return fmt.Errorf("failed to configure origin worktree git user: %v", err)
	}
	if err := runGit(worktree, "config", "commit.gpgSign", "false"); err != nil {
		return fmt.Errorf("failed to disable origin worktree git signing: %v", err)
	}
	if err := runGit(worktree, "remote", "set-url", "origin", originRepo); err != nil {
		return fmt.Errorf("failed to set origin worktree remote: %v", err)
	}

	if len(origin.Files) > 0 {
		for name, content := range origin.Files {
			path := filepath.Join(worktree, name)
			rel, relErr := filepath.Rel(worktree, path)
			if relErr != nil || strings.HasPrefix(rel, "..") {
				return fmt.Errorf("setup.git.origin.files key %q escapes workspace", name)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return err
			}
		}
		if err := commitIfStaged(worktree, "seed commit from setup.git.origin.files", hostEnv); err != nil {
			return fmt.Errorf("failed to commit setup.git.origin.files: %v", err)
		}

		branch := defaultBranch
		if gitCfg.Origin.Branch != "" {
			branch = gitCfg.Origin.Branch
		}
		if err := runGit(worktree, "branch", "-M", branch); err != nil {
			return fmt.Errorf("failed to rename origin worktree branch: %v", err)
		}
	}

	if len(origin.Commands) > 0 {
		effectiveEnv := hostEnv
		if effectiveEnv == nil {
			effectiveEnv = os.Environ()
		}
		host := envSliceToMap(effectiveEnv)
		for _, cmdStr := range origin.Commands {
			cmd := exec.Command("bash", "-c", cmdStr)
			cmd.Dir = worktree
			cmd.Env = []string{
				"GLUT_ORIGIN_REPO=" + originRepo,
				"HOME=" + host["HOME"],
				"PATH=" + host["PATH"],
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("origin command failed: %v, output: %s", err, string(out))
			}
		}
	}

	// Push everything to the bare origin repo
	if err := runGit(worktree, "push", "--all", "origin"); err != nil {
		return fmt.Errorf("failed to push origin branches: %v", err)
	}
	if err := runGit(worktree, "push", "--tags", "origin"); err != nil {
		return fmt.Errorf("failed to push origin tags: %v", err)
	}

	return nil
}

func (w *Workspace) Destroy() error {
	if w.KeepWorkspace {
		fmt.Printf("Test workspace preserved at: %s\n", w.Dir)
		fmt.Println("To inspect:")
		fmt.Printf("  cd %s\n", w.Dir)
		fmt.Println("  ls -la bin/                          # mock binaries (symlinks)")
		fmt.Println("  ls -la mock-logs/                    # call logs")
		fmt.Println("  git log --all                        # workspace history")
		fmt.Println("  git --git-dir=.glut-origin.git log --all   # origin history")
		return nil
	}
	return os.RemoveAll(w.Dir)
}

func runCmd(dir string, name string, args ...string) error {
	return runCmdEnv(dir, nil, name, args...)
}

func runCmdEnv(dir string, env []string, name string, args ...string) error {
	bin := resolveExecutable(name, env)
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = env // nil = inherit process env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command %s %v failed: %v, output: %s", name, args, err, string(out))
	}
	return nil
}

// resolveExecutable looks up name in hostEnv's PATH when hostEnv is non-nil,
// falling back to exec.LookPath (process PATH) when hostEnv is nil.
func resolveExecutable(name string, hostEnv []string) string {
	if hostEnv == nil {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
		return name
	}
	host := envSliceToMap(hostEnv)
	for _, dir := range filepath.SplitList(host["PATH"]) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return name
}

func envSliceToMap(env []string) map[string]string {
	if env == nil {
		return map[string]string{}
	}
	m := make(map[string]string, len(env))
	for _, kv := range env {
		key, value, _ := strings.Cut(kv, "=")
		m[key] = value
	}
	return m
}

// resolveDefaultBranch returns the effective CI_DEFAULT_BRANCH value for a
// workspace. Priority: setup.default_branch > setup.api.project.default_branch
// (deprecated) > origin/HEAD detected from the source repo > "main".
func resolveDefaultBranch(cfg parser.SetupConfig, srcDir string) string {
	if cfg.DefaultBranch != "" {
		return cfg.DefaultBranch
	}
	if cfg.API != nil && cfg.API.Project != nil && cfg.API.Project.DefaultBranch != "" {
		return cfg.API.Project.DefaultBranch
	}
	detected := getDefaultBranchFromRepo(srcDir)
	if detected != "" && detected != DetachedHead {
		return detected
	}
	return config.DefaultBranchName
}

// getDefaultBranchFromRepo detects the default branch of a git repository by
// reading refs/remotes/origin/HEAD. This works on the real source repo, giving
// an accurate result regardless of what branch the test workspace is configured
// to use.
func getDefaultBranchFromRepo(dir string) string {
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(out))
		if strings.HasPrefix(ref, "refs/remotes/origin/") {
			return strings.TrimPrefix(ref, "refs/remotes/origin/")
		}
	}
	return ""
}

func commitIfStaged(dir string, message string, hostEnv []string) error {
	if err := runCmdEnv(dir, hostEnv, "git", "add", "-A"); err != nil {
		return err
	}

	gitBin := resolveExecutable("git", hostEnv)
	cmd := exec.Command(gitBin, "diff", "--cached", "--quiet")
	cmd.Dir = dir
	cmd.Env = hostEnv
	if err := cmd.Run(); err == nil {
		return nil
	}

	return runCmdEnv(dir, hostEnv, "git", "commit", "-m", message)
}

func removeRemoteIfExists(dir string, name string, hostEnv []string) error {
	gitBin := resolveExecutable("git", hostEnv)
	cmd := exec.Command(gitBin, "remote", "get-url", name)
	cmd.Dir = dir
	cmd.Env = hostEnv
	if err := cmd.Run(); err != nil {
		return nil
	}
	return runCmdEnv(dir, hostEnv, "git", "remote", "remove", name)
}

// copyRepo copies src into dst, skipping .git. Strategy is selected by opts:
// auto (default) tries rsync first and falls back to native Go copy; rsync and
// native force the respective method. In verbose mode the chosen method is logged.
func copyRepo(src, dst string, opts Options) error {
	if len(opts.Include) > 0 {
		return copyRepoIncludes(src, dst, opts)
	}
	return copyRepoAll(src, dst, opts)
}

// copyRepoIncludes copies only the listed subdirectories from src into dst.
func copyRepoIncludes(src, dst string, opts Options) error {
	for _, inc := range opts.Include {
		incSrc := filepath.Join(src, inc)
		incDst := filepath.Join(dst, inc)
		if err := os.MkdirAll(filepath.Dir(incDst), 0755); err != nil {
			return err
		}
		if err := copyRepoAll(incSrc, incDst, opts); err != nil {
			return fmt.Errorf("include %q: %w", inc, err)
		}
	}
	return nil
}

func copyRepoAll(src, dst string, opts Options) error {
	strategy := opts.CopyStrategy
	if strategy == "" {
		strategy = CopyStrategyAuto
	}

	if strategy == CopyStrategyNative {
		if opts.Verbose {
			fmt.Println("[glut] copy strategy: native")
		}
		return copyDir(src, dst)
	}

	srcSlash := filepath.ToSlash(filepath.Clean(src)) + "/"
	dstSlash := filepath.ToSlash(filepath.Clean(dst)) + "/"

	runRsync := func() error {
		var stderr strings.Builder
		cmd := exec.Command("rsync", "-a", "--no-owner", "--no-group", "--exclude=.git", srcSlash, dstSlash)
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				return fmt.Errorf("rsync: %w\n%s", err, msg)
			}
			return fmt.Errorf("rsync: %w", err)
		}
		return nil
	}

	err := runRsync()
	if err == nil {
		if opts.Verbose {
			fmt.Println("[glut] copy strategy: rsync")
		}
		return nil
	}

	if strategy == CopyStrategyRsync {
		return err
	}

	// auto: rsync not installed — fall back to native.
	if isNotFound(err) {
		if opts.Verbose {
			fmt.Println("[glut] copy strategy: native (rsync not found)")
		}
		return copyDir(src, dst)
	}

	// rsync ran but failed (e.g. transient WSL2 I/O error) — retry once.
	if removeErr := os.RemoveAll(dst); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}
	if retryErr := runRsync(); retryErr != nil {
		return retryErr
	}
	if opts.Verbose {
		fmt.Println("[glut] copy strategy: rsync (retried)")
	}
	return nil
}

func isNotFound(err error) bool {
	var exitErr *exec.ExitError
	return !errors.As(err, &exitErr)
}

func copyDir(src string, dst string) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return err
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		pathAbs, err := filepath.Abs(path)
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)
		dstPathAbs, err := filepath.Abs(dstPath)
		if err != nil {
			return err
		}
		if isPathInside(dstPathAbs, srcAbs) && isPathInside(pathAbs, dstAbs) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			base := filepath.Base(relPath)
			if base == ".git" || strings.HasPrefix(base, tmpDirPrefix) {
				return filepath.SkipDir
			}
			return os.MkdirAll(dstPath, info.Mode())
		}

		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(target, dstPath)
		}

		return copyFile(path, dstPath, info.Mode())
	})
}

// copyFile copies a single file using the fastest method available:
// hardlink (instant, same inode) → reflink/copy_file_range/io.Copy.
func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	if err := reflink.Auto(src, dst); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}


// writeExecutableFile writes content to path using a temp+rename pattern to
// avoid ETXTBSY on overlayfs (Docker) when the file is immediately exec'd.
func writeExecutableFile(path string, content string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-exec-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(0755); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

func isPathInside(path string, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
