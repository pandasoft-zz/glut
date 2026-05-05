package workspace

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/parser"
)

const (
	mockBinDirName  = "bin"
	mockRealDirName = "bin-real"
	mockLogDirName  = "mock-logs"
)

func SetupMockBinaries(tmpWork string, mocks parser.MocksConfig, glutBinPath string) error {
	if len(mocks.Binaries) == 0 {
		return nil
	}

	glutBinPath, err := filepath.Abs(glutBinPath)
	if err != nil {
		return fmt.Errorf("resolve glut binary path: %w", err)
	}
	if _, err := os.Stat(glutBinPath); err != nil {
		return fmt.Errorf("stat glut binary %s: %w", glutBinPath, err)
	}

	binDir := MockBinaryBinDir(tmpWork)
	realDir := MockBinaryRealDir(tmpWork)
	logDir := MockBinaryLogDir(tmpWork)

	for _, dir := range []string{binDir, realDir, logDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create mock binary directory %s: %w", dir, err)
		}
	}

	for name, mock := range mocks.Binaries {
		if err := validateMockBinaryName(name); err != nil {
			return err
		}

		realPath := filepath.Join(realDir, name)
		if err := os.WriteFile(realPath, []byte(shellScript(mock.Executable)), 0755); err != nil {
			return fmt.Errorf("write real mock binary %s: %w", name, err)
		}

		linkPath := filepath.Join(binDir, name)
		if err := os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("replace mock binary link %s: %w", name, err)
		}
		if err := linkMockBinary(linkPath, glutBinPath); err != nil {
			return fmt.Errorf("link mock binary %s: %w", name, err)
		}
	}

	return nil
}

func AddMockBinaryEnv(env map[string]string, tmpWork string) {
	env[config.EnvMockLogDir] = MockBinaryLogDir(tmpWork)
	env[config.EnvMockBinReal] = MockBinaryRealDir(tmpWork)
	env["PATH"] = PrependMockBinaryPath(tmpWork, env["PATH"])
}

func PrependMockBinaryPath(tmpWork string, currentPath string) string {
	binDir := MockBinaryBinDir(tmpWork)
	if currentPath == "" {
		return binDir
	}
	return binDir + string(os.PathListSeparator) + currentPath
}

func MockBinaryBinDir(tmpWork string) string {
	return filepath.Join(tmpWork, mockBinDirName)
}

func MockBinaryRealDir(tmpWork string) string {
	return filepath.Join(tmpWork, mockRealDirName)
}

func MockBinaryLogDir(tmpWork string) string {
	return filepath.Join(tmpWork, mockLogDirName)
}

func validateMockBinaryName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid mock binary name %q", name)
	}
	if filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("mock binary name %q must not contain path separators", name)
	}
	return nil
}

func shellScript(content string) string {
	if strings.HasPrefix(content, "#!") {
		if strings.HasSuffix(content, "\n") {
			return content
		}
		return content + "\n"
	}
	return "#!/bin/sh\n" + content + "\n"
}

func linkMockBinary(linkPath string, targetPath string) error {
	if err := os.Symlink(targetPath, linkPath); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return err
	}

	return copyExecutable(targetPath, linkPath)
}

func copyExecutable(src string, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
