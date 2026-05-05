package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/parser"
)

func TestSetupMockBinariesCreatesWrapperLinksAndRealScripts(t *testing.T) {
	tmpWork := t.TempDir()
	glutBin := filepath.Join(tmpWork, "glut")
	if runtime.GOOS == "windows" {
		glutBin += ".exe"
	}
	if err := os.WriteFile(glutBin, []byte("fake glut"), 0755); err != nil {
		t.Fatal(err)
	}

	mocks := parser.MocksConfig{
		Binaries: map[string]parser.BinaryMockConfig{
			"release-cli": {
				Executable: "echo release",
			},
		},
	}

	if err := SetupMockBinaries(tmpWork, mocks, glutBin); err != nil {
		t.Fatalf("setup mock binaries: %v", err)
	}

	binPath := filepath.Join(MockBinaryBinDir(tmpWork), "release-cli")
	if _, err := os.Lstat(binPath); err != nil {
		t.Fatalf("mock binary link does not exist: %v", err)
	}
	if runtime.GOOS != "windows" {
		target, err := os.Readlink(binPath)
		if err != nil {
			t.Fatalf("mock binary is not a symlink: %v", err)
		}
		if target != glutBin {
			t.Fatalf("symlink target = %q, want %q", target, glutBin)
		}
	}

	realPath := filepath.Join(MockBinaryRealDir(tmpWork), "release-cli")
	data, err := os.ReadFile(realPath)
	if err != nil {
		t.Fatalf("real script does not exist: %v", err)
	}
	if !strings.Contains(string(data), "echo release") {
		t.Fatalf("real script content = %q", string(data))
	}
	info, err := os.Stat(realPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
		t.Fatalf("real script is not executable: %v", info.Mode())
	}
	if _, err := os.Stat(MockBinaryLogDir(tmpWork)); err != nil {
		t.Fatalf("mock log dir does not exist: %v", err)
	}
}

func TestAddMockBinaryEnvPrependsPath(t *testing.T) {
	tmpWork := t.TempDir()
	env := map[string]string{
		"PATH": "/usr/bin",
	}

	AddMockBinaryEnv(env, tmpWork)

	wantPrefix := MockBinaryBinDir(tmpWork) + string(os.PathListSeparator)
	if !strings.HasPrefix(env["PATH"], wantPrefix) {
		t.Fatalf("PATH = %q, want prefix %q", env["PATH"], wantPrefix)
	}
	if env[config.EnvMockLogDir] != MockBinaryLogDir(tmpWork) {
		t.Fatalf("log dir env = %q", env[config.EnvMockLogDir])
	}
	if env[config.EnvMockBinReal] != MockBinaryRealDir(tmpWork) {
		t.Fatalf("real dir env = %q", env[config.EnvMockBinReal])
	}
}

func TestEnvVarsAddsMockBinaryEnvironmentWhenMocksExist(t *testing.T) {
	tmpWork := t.TempDir()
	w := &Workspace{
		Dir:          tmpWork,
		WorkspaceDir: filepath.Join(tmpWork, "workspace"),
		OriginRepo:   filepath.Join(tmpWork, ".glut-origin.git"),
	}
	setup := parser.SetupConfig{
		Mocks: &parser.MocksConfig{
			Binaries: map[string]parser.BinaryMockConfig{
				"tool": {Executable: "echo ok"},
			},
		},
	}

	env := w.EnvVars(setup, 8080, "sha", "short", "test")

	if !strings.HasPrefix(env["PATH"], MockBinaryBinDir(tmpWork)) {
		t.Fatalf("PATH = %q", env["PATH"])
	}
	if env[config.EnvMockLogDir] != MockBinaryLogDir(tmpWork) {
		t.Fatalf("log dir env = %q", env[config.EnvMockLogDir])
	}
	if env[config.EnvMockBinReal] != MockBinaryRealDir(tmpWork) {
		t.Fatalf("real dir env = %q", env[config.EnvMockBinReal])
	}
}
