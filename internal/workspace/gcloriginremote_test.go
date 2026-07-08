package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pandasoft-zz/glut/internal/parser"
)

func initGitDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v (%s)", err, out)
	}
	return dir
}

func TestSetGCLOriginRemoteAddsAndRefreshes(t *testing.T) {
	t.Parallel()
	dir := initGitDir(t)
	w := &Workspace{WorkspaceDir: dir}

	if err := w.SetGCLOriginRemote("https://gitlab.example.com/group/project.git"); err != nil {
		t.Fatalf("SetGCLOriginRemote() error = %v", err)
	}
	// Refreshing must replace the stale remote, not fail on "already exists".
	if err := w.SetGCLOriginRemote("https://gitlab.example.com/group/other.git"); err != nil {
		t.Fatalf("second SetGCLOriginRemote() error = %v", err)
	}

	cmd := exec.Command("git", "remote", "get-url", "gcl-origin")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("read gcl-origin remote: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "https://gitlab.example.com/group/other.git" {
		t.Fatalf("gcl-origin URL = %q, want the refreshed URL", got)
	}
}

func TestSetGCLOriginRemoteFailsOutsideGitRepo(t *testing.T) {
	t.Parallel()
	w := &Workspace{WorkspaceDir: t.TempDir()}
	if err := w.SetGCLOriginRemote("https://gitlab.example.com/x.git"); err == nil {
		t.Fatal("SetGCLOriginRemote() outside a git repo must fail")
	}
}

// TestSetupMockBinariesDockerModeCopiesWrapperIntoWorkspace pins the Docker
// branch: the glut binary is copied inside the workspace (the container mount
// root) instead of being symlinked to a host path invisible to the job
// container.
func TestSetupMockBinariesDockerModeCopiesWrapperIntoWorkspace(t *testing.T) {
	t.Parallel()
	tmpWork := t.TempDir()
	glutBin := filepath.Join(t.TempDir(), "glut")
	if err := os.WriteFile(glutBin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	mocks := parser.MocksConfig{Binaries: map[string]parser.BinaryMockConfig{
		"release-cli": {Executable: "echo release"},
	}}
	if err := SetupMockBinaries(tmpWork, mocks, glutBin, true); err != nil {
		t.Fatalf("SetupMockBinaries(docker) error = %v", err)
	}

	wrapper := filepath.Join(tmpWork, "glut-wrapper")
	info, err := os.Stat(wrapper)
	if err != nil {
		t.Fatalf("glut wrapper not copied into workspace: %v", err)
	}
	if info.Mode()&0100 == 0 {
		t.Fatalf("glut wrapper mode = %v, want executable", info.Mode())
	}
	if _, err := os.Stat(filepath.Join(MockBinaryBinDir(tmpWork), "release-cli")); err != nil {
		t.Fatalf("mock link missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(MockBinaryRealDir(tmpWork), "release-cli")); err != nil {
		t.Fatalf("real mock script missing: %v", err)
	}
}

func TestSetupMockBinariesRejectsMissingGlutBinary(t *testing.T) {
	t.Parallel()
	mocks := parser.MocksConfig{Binaries: map[string]parser.BinaryMockConfig{"tool": {Executable: "echo"}}}
	missing := filepath.Join(t.TempDir(), "no-such-glut")
	if err := SetupMockBinaries(t.TempDir(), mocks, missing, false); err == nil {
		t.Fatal("SetupMockBinaries() with a missing glut binary must fail")
	}
}

func TestWriteExecutableFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tool")
	if err := writeExecutableFile(path, "#!/bin/sh\necho ok\n"); err != nil {
		t.Fatalf("writeExecutableFile() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat written file: %v", err)
	}
	if info.Mode()&0100 == 0 {
		t.Fatalf("file mode = %v, want executable", info.Mode())
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "echo ok") {
		t.Fatalf("content = %q, %v", data, err)
	}

	// Writing into a missing directory must fail at temp-file creation.
	if err := writeExecutableFile(filepath.Join(dir, "missing", "tool"), "x"); err == nil {
		t.Fatal("writeExecutableFile() into a missing dir must fail")
	}
}
