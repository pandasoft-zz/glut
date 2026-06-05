package workspace

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pandasoft-zz/glut/internal/parser"
)

const (
	// volumePopulateAttempts is the number of times to retry the docker run
	// step that populates the volume. On WSL2/Docker Desktop the daemon may
	// still be cleaning up containers from the previous test when a new
	// populate starts, causing transient tar errors.
	volumePopulateAttempts   = 3
	volumePopulateRetryDelay = 500 * time.Millisecond
)

// shellMockWrapper is a busybox-sh-compatible script placed at bin/{name} inside
// the Docker volume. It logs the call to GLUT_MOCK_LOG_DIR as a JSON line and
// then execs the real script from GLUT_MOCK_BIN_REAL/{name}.
const shellMockWrapper = `#!/bin/sh
name=$(basename "$0")
ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "1970-01-01T00:00:00Z")
pid=$$
ppid=0
if [ -r /proc/$pid/status ]; then
    ppid=$(awk '/^PPid:/{print $2}' /proc/$pid/status 2>/dev/null || echo 0)
fi
cwd=$(pwd 2>/dev/null || echo "/")

args_json="["
first=1
for a in "$@"; do
    esc=$(printf '%s' "$a" | sed 's/\\/\\\\/g; s/"/\\"/g')
    if [ $first -eq 0 ]; then
        args_json="${args_json},"
    fi
    args_json="${args_json}\"${esc}\""
    first=0
done
args_json="${args_json}]"

if [ -n "$GLUT_MOCK_LOG_DIR" ]; then
    mkdir -p "$GLUT_MOCK_LOG_DIR" 2>/dev/null
    cwd_esc=$(printf '%s' "$cwd" | sed 's/\\/\\\\/g; s/"/\\"/g')
    printf '{"ts":"%s","pid":%d,"ppid":%d,"cwd":"%s","name":"%s","args":%s,"stdin":""}\n' \
        "$ts" "$pid" "$ppid" "$cwd_esc" "$name" "$args_json" \
        >> "$GLUT_MOCK_LOG_DIR/${name}.jsonl"
fi

if [ -z "$GLUT_MOCK_BIN_REAL" ]; then
    printf 'mock wrapper: GLUT_MOCK_BIN_REAL not set\n' >&2
    exit 127
fi
exec "$GLUT_MOCK_BIN_REAL/$name" "$@"
`

// CreateDockerVolume creates a Docker named volume and populates it with the
// mock binary wrappers, real scripts, an empty mock-logs directory, and a copy
// of the git bare origin — all at the paths that match workDir so that
// file:// URLs and GLUT_MOCK_* env vars work unchanged inside the container.
//
// All content is piped into the volume via "docker run -i … tar -x", which
// bypasses host-filesystem visibility issues (overlay FS, DinD bind-mount
// failures) that make plain --volume host:container mounts unreliable.
func CreateDockerVolume(workDir, originRepo string, mocks *parser.MocksConfig) (string, error) {
	volName := "glut-" + filepath.Base(workDir)

	if out, err := exec.Command("docker", "volume", "create", volName).CombinedOutput(); err != nil {
		return "", fmt.Errorf("docker volume create %s: %w (%s)", volName, err, strings.TrimSpace(string(out)))
	}

	archive, err := buildVolumeArchive(workDir, originRepo, mocks)
	if err != nil {
		_ = exec.Command("docker", "volume", "rm", volName).Run()
		return "", fmt.Errorf("build volume archive: %w", err)
	}

	delay := volumePopulateRetryDelay
	var populateErr error
	for attempt := 0; attempt < volumePopulateAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(delay)
			delay *= 2
			if _, err := archive.Seek(0, io.SeekStart); err != nil {
				_ = exec.Command("docker", "volume", "rm", volName).Run()
				return "", fmt.Errorf("reset volume archive for retry: %w", err)
			}
		}
		cmd := exec.Command("docker", "run", "--rm", "-i",
			"--volume", volName+":"+workDir,
			"alpine", "tar", "-xC", workDir)
		cmd.Stdin = archive
		out, err := cmd.CombinedOutput()
		if err == nil {
			populateErr = nil
			break
		}
		populateErr = fmt.Errorf("populate docker volume: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if populateErr != nil {
		_ = exec.Command("docker", "volume", "rm", volName).Run()
		return "", populateErr
	}

	return volName, nil
}

// ReadLogsFromDockerVolume copies mock-logs written inside the Docker volume
// back to the local mock-logs directory so the asserter can read them.
func ReadLogsFromDockerVolume(volName, workDir string) error {
	logDir := MockBinaryLogDir(workDir)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create local log dir: %w", err)
	}

	// Produce a tar of the mock-logs directory contents from inside the volume.
	tarCmd := exec.Command("docker", "run", "--rm",
		"--volume", volName+":"+workDir,
		"alpine", "tar", "-cC", MockBinaryLogDir(workDir), ".")
	tarData, err := tarCmd.Output()
	if err != nil {
		// No log files written is not an error.
		return nil
	}

	extractCmd := exec.Command("tar", "-xC", logDir)
	extractCmd.Stdin = bytes.NewReader(tarData)
	if out, err := extractCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extract mock logs: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// FetchGitOriginTar returns a tar archive of the .glut-origin.git directory
// from inside a Docker volume. The archive can be passed to NewLazyTarOrigin
// to provide lazy access to the origin repo without immediately extracting it.
func FetchGitOriginTar(volName, workDir string) ([]byte, error) {
	tarCmd := exec.Command("docker", "run", "--rm",
		"--volume", volName+":"+workDir,
		"alpine", "tar", "-cC", workDir, ".glut-origin.git")
	tarData, err := tarCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tar git origin from docker volume: %w", err)
	}
	return tarData, nil
}

// DestroyDockerVolume removes the named Docker volume created by CreateDockerVolume.
// It first force-removes any containers still referencing the volume. On
// WSL2/Docker Desktop, containers spawned with --rm may still be registered
// in the daemon when this function runs, causing docker volume rm to fail with
// "volume is in use". Force-removing them first avoids leaving orphaned
// volumes that keep the daemon busy during the next test.
func DestroyDockerVolume(volName string) error {
	removeVolumeContainers(volName)
	out, err := exec.Command("docker", "volume", "rm", volName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume rm %s: %w (%s)", volName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// removeVolumeContainers removes stopped containers that still reference
// volName. docker rm (without -f) refuses to remove running containers, so
// there is no risk of killing a legitimately active container. Errors are
// intentionally ignored: if a container was already removed the call is a
// no-op, and if one is still running we leave it alone.
func removeVolumeContainers(volName string) {
	out, err := exec.Command("docker", "ps", "-a",
		"--filter", "volume="+volName, "--format", "{{.ID}}").Output()
	if err != nil {
		return
	}
	for _, id := range strings.Fields(string(out)) {
		_ = exec.Command("docker", "rm", id).Run()
	}
}

// buildVolumeArchive constructs an in-memory tar archive containing:
//   - bin/{name}        shell mock wrapper for each mock binary
//   - bin-real/{name}   real executable script for each mock binary
//   - mock-logs/        empty directory for call logs
//   - .glut-origin.git/ full copy of the bare git origin
func buildVolumeArchive(workDir, originRepo string, mocks *parser.MocksConfig) (*bytes.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	dirs := []string{mockBinDirName, mockRealDirName, mockLogDirName}
	for _, dir := range dirs {
		if err := tw.WriteHeader(&tar.Header{
			Name:     dir + "/",
			Mode:     0755,
			Typeflag: tar.TypeDir,
		}); err != nil {
			return nil, err
		}
	}

	if mocks != nil {
		for name, mock := range mocks.Binaries {
			if err := validateMockBinaryName(name); err != nil {
				return nil, err
			}

			wrapper := []byte(shellMockWrapper)
			if err := tw.WriteHeader(&tar.Header{
				Name:     mockBinDirName + "/" + name,
				Mode:     0755,
				Size:     int64(len(wrapper)),
				Typeflag: tar.TypeReg,
			}); err != nil {
				return nil, err
			}
			if _, err := tw.Write(wrapper); err != nil {
				return nil, err
			}

			script := []byte(shellScript(mock.Executable))
			if err := tw.WriteHeader(&tar.Header{
				Name:     mockRealDirName + "/" + name,
				Mode:     0755,
				Size:     int64(len(script)),
				Typeflag: tar.TypeReg,
			}); err != nil {
				return nil, err
			}
			if _, err := tw.Write(script); err != nil {
				return nil, err
			}
		}
	}

	if err := addDirToArchive(tw, originRepo, ".glut-origin.git"); err != nil {
		return nil, fmt.Errorf("archive git origin: %w", err)
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	return bytes.NewReader(buf.Bytes()), nil
}

// addDirToArchive walks srcDir and writes all files into tw under the given
// prefix. Symlinks are dereferenced (their content is written as regular files)
// so Alpine's tar can extract them without a symlink target on the host.
func addDirToArchive(tw *tar.Writer, srcDir, prefix string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		archivePath := prefix + "/" + filepath.ToSlash(rel)

		if d.IsDir() {
			return tw.WriteHeader(&tar.Header{
				Name:     archivePath + "/",
				Mode:     0755,
				Typeflag: tar.TypeDir,
			})
		}

		// Dereference symlinks: read the actual file content.
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mode := int64(info.Mode().Perm())
		if mode == 0 {
			mode = 0644
		}
		if err := tw.WriteHeader(&tar.Header{
			Name:     archivePath,
			Mode:     mode,
			Size:     int64(len(data)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
}
