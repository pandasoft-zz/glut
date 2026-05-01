package workspace

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pandasoft-zz/glut/internal/parser"
)

type Workspace struct {
	Dir           string
	WorkspaceDir  string
	OriginRepo    string
	KeepWorkspace bool
}

func New(cfg parser.SetupConfig, keepWorkspace bool, srcDir string) (*Workspace, error) {
	tmpWork, err := ioutil.TempDir("", "glut-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp workspace: %v", err)
	}

	stagingDir := filepath.Join(tmpWork, "staging")
	workspaceDir := filepath.Join(tmpWork, "workspace")
	originRepo := filepath.Join(tmpWork, ".glut-origin.git")

	// 2. Copy host repository
	srcDirSlash := filepath.ToSlash(srcDir)
	stagingDirSlash := filepath.ToSlash(stagingDir)
	rsyncCmd := exec.Command("sh", "-c", fmt.Sprintf("rsync -a '%s/' '%s/'", srcDirSlash, stagingDirSlash))
	if err := rsyncCmd.Run(); err != nil {
		// Fallback to native Go copy to support running tests on Windows
		if cpErr := copyDir(srcDir, stagingDir); cpErr != nil {
			return nil, fmt.Errorf("failed to copy repository natively after rsync failed: %v", cpErr)
		}
	}

	// 3. Initialize bare git repo
	if err := runCmd(tmpWork, "git", "init", "--bare", originRepo); err != nil {
		return nil, fmt.Errorf("failed to init bare origin: %v", err)
	}

	// Ensure staging is a git repo if not already
	runCmd(stagingDir, "git", "init")

	// Configure git to avoid author missing errors in the copied repo
	userName := DefaultUserName
	userEmail := DefaultUserEmail
	if cfg.Git != nil && cfg.Git.User.Name != "" {
		userName = cfg.Git.User.Name
	}
	if cfg.Git != nil && cfg.Git.User.Email != "" {
		userEmail = cfg.Git.User.Email
	}
	runCmd(stagingDir, "git", "config", "user.email", userEmail)
	runCmd(stagingDir, "git", "config", "user.name", userName)

	// 4. Snapshot commit
	runCmd(stagingDir, "git", "add", "-A")
	runCmd(stagingDir, "git", "commit", "-m", "glut: workspace snapshot")

	// 5. Set origin remote and push snapshot to bare repo FIRST
	runCmd(stagingDir, "git", "remote", "remove", "origin")
	if err := runCmd(stagingDir, "git", "remote", "add", "origin", originRepo); err != nil {
		return nil, fmt.Errorf("failed to add origin remote: %v", err)
	}
	branch := getCurrentBranch(stagingDir)
	if branch == "" || branch == DetachedHead {
		branch = getDefaultBranch(stagingDir)
	}

	if cfg.Git != nil && cfg.Git.Origin != nil && cfg.Git.Origin.Branch != "" {
		branch = cfg.Git.Origin.Branch
	}
	runCmd(stagingDir, "git", "checkout", "-b", branch)
	if err := runCmd(stagingDir, "git", "push", "-f", "origin", branch); err != nil {
		return nil, fmt.Errorf("failed to push snapshot to bare origin: %v", err)
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
	os.RemoveAll(stagingDir)

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
	defer os.RemoveAll(worktree)

	userName := DefaultUserName
	userEmail := DefaultUserEmail
	if gitCfg.User.Name != "" {
		userName = gitCfg.User.Name
	}
	if gitCfg.User.Email != "" {
		userEmail = gitCfg.User.Email
	}
	runCmd(worktree, "git", "config", "user.email", userEmail)
	runCmd(worktree, "git", "config", "user.name", userName)
	runCmd(worktree, "git", "remote", "add", "origin", originRepo)

	if len(origin.Files) > 0 {
		for name, content := range origin.Files {
			path := filepath.Join(worktree, name)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			if err := ioutil.WriteFile(path, []byte(content), 0644); err != nil {
				return err
			}
		}
		runCmd(worktree, "git", "add", "-A")
		runCmd(worktree, "git", "commit", "-m", "seed commit from setup.git.origin.files")

		branch := defaultBranch
		if gitCfg.Origin.Branch != "" {
			branch = gitCfg.Origin.Branch
		}
		runCmd(worktree, "git", "branch", "-M", branch)
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
	runCmd(worktree, "git", "push", "--all", "origin")
	runCmd(worktree, "git", "push", "--tags", "origin")

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

func getCurrentBranch(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err == nil {
		branch := strings.TrimSpace(string(out))
		if branch != "" {
			return branch
		}
	}
	return DetachedHead
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

	return DefaultBranchName
}

func copyDir(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Calculate relative path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// Don't copy the destination directory into itself if src is '.'
		if strings.HasPrefix(dstPath, dst) && relPath != "." && strings.HasPrefix(path, dst) {
			return filepath.SkipDir
		}

		data, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}

		return ioutil.WriteFile(dstPath, data, info.Mode())
	})
}
