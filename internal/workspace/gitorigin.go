package workspace

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// LazyTarOrigin is a GitOriginSource backed by a tar archive fetched from a
// Docker volume. The archive is extracted to a temporary directory on the first
// call to Path(); subsequent calls return the same directory. Call Close() to
// remove the temporary directory after assertions complete.
type LazyTarOrigin struct {
	tarData []byte
	mu      sync.Mutex
	dir     string
}

// NewLazyTarOrigin creates a LazyTarOrigin from a tar archive produced by
// FetchGitOriginTar. The archive is not extracted until Path() is called.
func NewLazyTarOrigin(tarData []byte) *LazyTarOrigin {
	return &LazyTarOrigin{tarData: tarData}
}

// Path extracts the tar archive on the first call and returns the path to
// .glut-origin.git inside the temporary directory. Subsequent calls are cheap.
func (l *LazyTarOrigin) Path() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.dir != "" {
		return filepath.Join(l.dir, ".glut-origin.git"), nil
	}
	tmpDir, err := os.MkdirTemp("", "glut-origin-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir for git origin: %w", err)
	}
	cmd := exec.Command("tar", "-xC", tmpDir)
	cmd.Stdin = bytes.NewReader(l.tarData)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("extract git origin: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	l.dir = tmpDir
	return filepath.Join(tmpDir, ".glut-origin.git"), nil
}

// Close removes the temporary directory created by Path. It is a no-op if
// Path was never called.
func (l *LazyTarOrigin) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.dir == "" {
		return nil
	}
	err := os.RemoveAll(l.dir)
	l.dir = ""
	return err
}
