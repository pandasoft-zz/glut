package workspace

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/parser"
)

type Workspace struct {
	Dir           string
	WorkspaceDir  string
	OriginRepo    string
	KeepWorkspace bool
}

func New(cfg parser.SetupConfig, keepWorkspace bool, srcDir string) (workspace *Workspace, err error) {
	tmpWork, err := os.MkdirTemp("", "glut-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp workspace: %v", err)
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

	stagingDir := filepath.Join(tmpWork, "staging")
	workspaceDir := filepath.Join(tmpWork, "workspace")
	originRepo := filepath.Join(tmpWork, ".glut-origin.git")

	// 2. Copy host repository
	if err := copyRepo(srcDir, stagingDir); err != nil {
		return nil, fmt.Errorf("copy repository: %w", err)
	}

	// 3. Initialize bare git repo
	if err := runCmd(tmpWork, "git", "init", "--bare", originRepo); err != nil {
		return nil, fmt.Errorf("failed to init bare origin: %v", err)
	}

	if err := runCmd(stagingDir, "git", "init"); err != nil {
		return nil, fmt.Errorf("failed to init staging repo: %v", err)
	}

	// Configure git to avoid author missing errors in the copied repo
	userName := config.DefaultUserName
	userEmail := config.DefaultUserEmail
	if cfg.Git != nil && cfg.Git.User.Name != "" {
		userName = cfg.Git.User.Name
	}
	if cfg.Git != nil && cfg.Git.User.Email != "" {
		userEmail = cfg.Git.User.Email
	}
	if err := runCmd(stagingDir, "git", "config", "user.email", userEmail); err != nil {
		return nil, fmt.Errorf("failed to configure staging git email: %v", err)
	}
	if err := runCmd(stagingDir, "git", "config", "user.name", userName); err != nil {
		return nil, fmt.Errorf("failed to configure staging git user: %v", err)
	}

	// 4. Snapshot commit
	if err := commitIfStaged(stagingDir, "glut: workspace snapshot"); err != nil {
		return nil, fmt.Errorf("failed to create workspace snapshot: %v", err)
	}

	// 5. Set origin remote and push snapshot to bare repo FIRST
	if err := removeRemoteIfExists(stagingDir, "origin"); err != nil {
		return nil, fmt.Errorf("failed to remove existing origin remote: %v", err)
	}
	if err := runCmd(stagingDir, "git", "remote", "add", "origin", originRepo); err != nil {
		return nil, fmt.Errorf("failed to add origin remote: %v", err)
	}
	branch := config.DefaultBranchName
	if cfg.Git != nil && cfg.Git.Origin != nil && cfg.Git.Origin.Branch != "" {
		branch = cfg.Git.Origin.Branch
	}
	if err := runCmd(stagingDir, "git", "checkout", "-B", branch); err != nil {
		return nil, fmt.Errorf("failed to checkout origin branch %q: %v", branch, err)
	}
	if err := runCmd(stagingDir, "git", "push", "-f", "origin", branch); err != nil {
		return nil, fmt.Errorf("failed to push snapshot to bare origin: %v", err)
	}
	if err := runCmd(tmpWork, "git", "--git-dir", originRepo, "symbolic-ref", "HEAD", "refs/heads/"+branch); err != nil {
		return nil, fmt.Errorf("failed to set bare origin HEAD: %v", err)
	}

	w := &Workspace{
		Dir:           tmpWork,
		WorkspaceDir:  workspaceDir,
		OriginRepo:    originRepo,
		KeepWorkspace: keepWorkspace,
	}

	// 6. Process setup.git.origin ON TOP of the snapshot
	if cfg.Git != nil && cfg.Git.Origin != nil {
		if err := w.setupGitOrigin(tmpWork, originRepo, branch, cfg.Git); err != nil {
			return nil, err
		}
	}

	// 7. Clone workspace from bare repo
	if err := runCmd(tmpWork, "git", "clone", originRepo, workspaceDir); err != nil {
		return nil, fmt.Errorf("failed to clone workspace: %v", err)
	}

	// Clean up staging directory
	if err := os.RemoveAll(stagingDir); err != nil {
		return nil, fmt.Errorf("failed to remove staging directory: %v", err)
	}

	created = true
	return w, nil
}

func (w *Workspace) setupGitOrigin(tmpWork string, originRepo string, defaultBranch string, gitCfg *parser.GitSetupConfig) error {
	origin := gitCfg.Origin
	if len(origin.Files) == 0 && len(origin.Commands) == 0 {
		return nil
	}

	worktree := filepath.Join(tmpWork, "origin-worktree")
	if err := runCmd(tmpWork, "git", "clone", originRepo, worktree); err != nil {
		return fmt.Errorf("failed to clone worktree: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(worktree); err != nil {
			fmt.Printf("Failed to remove origin worktree: %v\n", err)
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
	if err := runCmd(worktree, "git", "config", "user.email", userEmail); err != nil {
		return fmt.Errorf("failed to configure origin worktree git email: %v", err)
	}
	if err := runCmd(worktree, "git", "config", "user.name", userName); err != nil {
		return fmt.Errorf("failed to configure origin worktree git user: %v", err)
	}
	if err := runCmd(worktree, "git", "remote", "set-url", "origin", originRepo); err != nil {
		return fmt.Errorf("failed to set origin worktree remote: %v", err)
	}

	if len(origin.Files) > 0 {
		for name, content := range origin.Files {
			path := filepath.Join(worktree, name)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return err
			}
		}
		if err := commitIfStaged(worktree, "seed commit from setup.git.origin.files"); err != nil {
			return fmt.Errorf("failed to commit setup.git.origin.files: %v", err)
		}

		branch := defaultBranch
		if gitCfg.Origin.Branch != "" {
			branch = gitCfg.Origin.Branch
		}
		if err := runCmd(worktree, "git", "branch", "-M", branch); err != nil {
			return fmt.Errorf("failed to rename origin worktree branch: %v", err)
		}
	}

	if len(origin.Commands) > 0 {
		for _, cmdStr := range origin.Commands {
			cmd := exec.Command("bash", "-c", cmdStr)
			cmd.Dir = worktree
			// empty env + GLUT_ORIGIN_REPO, HOME, PATH
			cmd.Env = []string{
				"GLUT_ORIGIN_REPO=" + originRepo,
				"HOME=" + os.Getenv("HOME"),
				"PATH=" + os.Getenv("PATH"),
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("origin command failed: %v, output: %s", err, string(out))
			}
		}
	}

	// Push everything to the bare origin repo
	if err := runCmd(worktree, "git", "push", "--all", "origin"); err != nil {
		return fmt.Errorf("failed to push origin branches: %v", err)
	}
	if err := runCmd(worktree, "git", "push", "--tags", "origin"); err != nil {
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
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command %s %v failed: %v, output: %s", name, args, err, string(out))
	}
	return nil
}

func getDefaultBranch(dir string) string {
	// Try to detect default branch from origin/HEAD
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(out))
		if strings.HasPrefix(ref, "refs/remotes/origin/") {
			return strings.TrimPrefix(ref, "refs/remotes/origin/")
		}
	}

	// Try to get default branch from git config
	cmd = exec.Command("git", "config", "--get", "init.defaultBranch")
	cmd.Dir = dir
	out, err = cmd.Output()
	if err == nil {
		cfgBranch := strings.TrimSpace(string(out))
		if cfgBranch != "" {
			return cfgBranch
		}
	}

	return config.DefaultBranchName
}

func commitIfStaged(dir string, message string) error {
	if err := runCmd(dir, "git", "add", "-A"); err != nil {
		return err
	}

	cmd := exec.Command("git", "diff", "--cached", "--quiet")
	cmd.Dir = dir
	if err := cmd.Run(); err == nil {
		return nil
	}

	return runCmd(dir, "git", "commit", "-m", message)
}

func removeRemoteIfExists(dir string, name string) error {
	cmd := exec.Command("git", "remote", "get-url", name)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return nil
	}
	return runCmd(dir, "git", "remote", "remove", name)
}

// copyRepo copies src into dst using rsync when available, falling back to
// copyDir only when rsync is not installed. If rsync is present but exits
// non-zero (e.g. intermittent WSL2 I/O error), we clean up any partially
// copied files and retry once before giving up.
func copyRepo(src, dst string) error {
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
		return nil
	}

	// If rsync is not installed, fall back to native copy.
	if isNotFound(err) {
		return copyDir(src, dst)
	}

	// rsync ran but failed (e.g. transient I/O error) — clean up partial
	// files and retry once before returning the error.
	_ = os.RemoveAll(dst)
	if retryErr := runRsync(); retryErr != nil {
		return retryErr
	}
	return nil
}

func isNotFound(err error) bool {
	var exitErr *exec.ExitError
	// exec.ErrNotFound or "no such file" means binary missing
	if !errors.As(err, &exitErr) {
		return true
	}
	return false
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

	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		pathAbs, err := filepath.Abs(path)
		if err != nil {
			return err
		}

		// Calculate relative path
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
			if relPath == ".git" {
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

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(dstPath, data, info.Mode())
	})
}

func isPathInside(path string, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
