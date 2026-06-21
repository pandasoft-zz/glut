package workspace

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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

// InfraError indicates a Docker infrastructure failure (volume creation,
// daemon communication) rather than a test job failure. runner.Run uses
// this type to distinguish transient daemon errors from genuine test failures
// and retry only the former.
type InfraError struct{ Err error }

func (e *InfraError) Error() string { return e.Err.Error() }
func (e *InfraError) Unwrap() error { return e.Err }

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

# json_str escapes a value for use inside a JSON string literal (no surrounding
# quotes). Handles: backslash, double-quote, newline, carriage-return, tab.
json_str() {
    printf '%s' "$1" | awk '
        BEGIN { ORS = ""; cr = "\r" }
        NR > 1 { printf "\\n" }
        {
            gsub(/\\/, "\\\\")
            gsub(/"/, "\\\"")
            gsub(/\t/, "\\t")
            gsub(cr, "\\r")
            print
        }
    '
}

args_json="["
first=1
for a in "$@"; do
    esc=$(json_str "$a")
    if [ $first -eq 0 ]; then
        args_json="${args_json},"
    fi
    args_json="${args_json}\"${esc}\""
    first=0
done
args_json="${args_json}]"

if [ -n "$GLUT_MOCK_LOG_DIR" ]; then
    mkdir -p "$GLUT_MOCK_LOG_DIR" 2>/dev/null
    barrier="$GLUT_MOCK_LOG_DIR/.${name}.jsonl.${pid}"
    touch "$barrier" 2>/dev/null
    cwd_esc=$(json_str "$cwd")
    printf '{"ts":"%s","pid":%d,"ppid":%d,"cwd":"%s","name":"%s","args":%s,"stdin":""}\n' \
        "$ts" "$pid" "$ppid" "$cwd_esc" "$name" "$args_json" \
        >> "$GLUT_MOCK_LOG_DIR/${name}.jsonl"
    rm -f "$barrier" 2>/dev/null
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
		return "", &InfraError{Err: fmt.Errorf("docker volume create %s: %w (%s)", volName, err, strings.TrimSpace(string(out)))}
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
		// Use an explicit container name so we can docker rm it synchronously
		// after the run completes. This avoids the --rm async daemon cleanup
		// that kept the daemon busy between tests on Docker Desktop / WSL2.
		ctrName := fmt.Sprintf("glut-pop-%s-%d", volName, attempt)
		cmd := exec.Command("docker", "run", "--name", ctrName, "-i",
			"--volume", volName+":"+workDir,
			"alpine", "tar", "-xC", workDir)
		cmd.Stdin = archive
		out, err := cmd.CombinedOutput()
		_ = exec.Command("docker", "rm", ctrName).Run() // synchronous cleanup
		if err == nil {
			populateErr = nil
			break
		}
		populateErr = fmt.Errorf("populate docker volume: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if populateErr != nil {
		_ = exec.Command("docker", "volume", "rm", volName).Run()
		return "", &InfraError{Err: populateErr}
	}

	return volName, nil
}

// SyncDockerVolume runs sync(1) inside the Docker volume so that all
// filesystem writes made by the pipeline container are committed before the
// logs are copied back to the host. Call this after the pipeline container
// exits and before ReadLogsFromDockerVolume.
func SyncDockerVolume(volName, workDir string) error {
	ctrName := "glut-sync-" + volName
	out, err := exec.Command("docker", "run", "--name", ctrName,
		"--volume", volName+":"+workDir,
		"alpine", "sync").CombinedOutput()
	_ = exec.Command("docker", "rm", ctrName).Run()
	if err != nil {
		return fmt.Errorf("sync docker volume %s: %w (%s)", volName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ReadLogsFromDockerVolume copies mock-logs written inside the Docker volume
// back to the local mock-logs directory so the asserter can read them.
func ReadLogsFromDockerVolume(volName, workDir string) error {
	logDir := MockBinaryLogDir(workDir)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create local log dir: %w", err)
	}

	// Produce a tar of the mock-logs directory contents from inside the volume.
	ctrName := "glut-logs-" + volName
	tarCmd := exec.Command("docker", "run", "--name", ctrName,
		"--volume", volName+":"+workDir,
		"alpine", "tar", "-cC", MockBinaryLogDir(workDir), ".")
	var stdout, stderr bytes.Buffer
	tarCmd.Stdout = &stdout
	tarCmd.Stderr = &stderr
	runErr := tarCmd.Run()
	_ = exec.Command("docker", "rm", ctrName).Run() // synchronous cleanup
	if runErr != nil {
		// Any non-empty stderr indicates a real Docker or tar error; propagate
		// it so the caller can distinguish an infrastructure failure from an
		// intentionally empty log directory (tar exits non-zero but stderr is
		// empty in that case).
		if se := bytes.TrimSpace(stderr.Bytes()); len(se) > 0 {
			return fmt.Errorf("read mock logs from volume: %w (%s)", runErr, se)
		}
		return nil
	}

	extractCmd := exec.Command("tar", "-xC", logDir)
	extractCmd.Stdin = &stdout
	if out, err := extractCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extract mock logs: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// FetchGitOriginTar returns a tar archive of the .glut-origin.git directory
// from inside a Docker volume. The archive can be passed to NewLazyTarOrigin
// to provide lazy access to the origin repo without immediately extracting it.
func FetchGitOriginTar(volName, workDir string) ([]byte, error) {
	ctrName := "glut-orig-" + volName
	tarCmd := exec.Command("docker", "run", "--name", ctrName,
		"--volume", volName+":"+workDir,
		"alpine", "tar", "-cC", workDir, ".glut-origin.git")
	var tarData bytes.Buffer
	var stderr bytes.Buffer
	tarCmd.Stdout = &tarData
	tarCmd.Stderr = &stderr
	err := tarCmd.Run()
	_ = exec.Command("docker", "rm", ctrName).Run() // synchronous cleanup
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("tar git origin from docker volume: %w: %s", err, stderr.String())
		}
		return nil, fmt.Errorf("tar git origin from docker volume: %w", err)
	}
	return tarData.Bytes(), nil
}

// ListGCLVolumes returns the names of all Docker volumes whose names begin
// with "gcl-". Call this before running a pipeline to snapshot the pre-run
// state; pass the result to FetchArtifactsFromGCLVolumes afterward.
func ListGCLVolumes() []string {
	out, err := exec.Command("docker", "volume", "ls", "-q", "--filter", "name=gcl-").Output()
	if err != nil {
		return nil
	}
	return strings.Fields(string(out))
}

// FetchArtifactsFromGCLVolumes copies job-produced files from the gcl-*-build
// volumes this run created and then removes them. It is the counterpart to
// running gitlab-ci-local with --cleanup=false: the caller keeps those volumes
// alive so they survive past executor.Run(), then calls this function.
//
// preRunVolumes is the snapshot taken before the pipeline ran; jobNames are the
// jobs in this pipeline. A volume is harvested only when it is new since the
// snapshot AND its job-name segment matches one of jobNames — this prevents a
// concurrent glut/gitlab-ci-local run on the same daemon from having its
// volumes extracted and destroyed. When jobNames is empty the nominal filter is
// skipped (temporal-only fallback).
//
// The extraction excludes .git and .gitlab-ci-local trees to avoid overwriting
// the workspace's own git state.
func FetchArtifactsFromGCLVolumes(preRunVolumes, jobNames []string, workspaceDir string) error {
	volumes := selectGCLArtifactVolumes(preRunVolumes, ListGCLVolumes(), jobNames)

	var firstErr error
	for _, vol := range volumes {
		if extractErr := extractGCLBuildVolume(vol, workspaceDir); extractErr != nil && firstErr == nil {
			firstErr = extractErr
		}
		removeVolumeContainers(vol)
		_ = exec.Command("docker", "volume", "rm", vol).Run()

		tmpVol := strings.TrimSuffix(vol, "-build") + "-tmp"
		removeVolumeContainers(tmpVol)
		_ = exec.Command("docker", "volume", "rm", tmpVol).Run()
	}
	return firstErr
}

// gclBuildVolumeRE matches gitlab-ci-local's build-volume naming pattern,
// gcl-<encodedJobName>-<jobId>-build, where jobId is a random number. This is
// the single source of truth for parsing those names; the executor's log-capture
// path reuses it by calling the exported GCLJobName below.
var gclBuildVolumeRE = regexp.MustCompile(`^gcl-(.+)-\d+-build$`)

// GCLJobName extracts the (URL-decoded) job name from a gcl-*-build volume name.
// gitlab-ci-local URL-encodes characters outside [\w-] into the segment, so we
// decode it to recover the original job name. Returns ok=false when the name
// does not match the build-volume shape.
func GCLJobName(vol string) (string, bool) {
	m := gclBuildVolumeRE.FindStringSubmatch(vol)
	if len(m) != 2 {
		return "", false
	}
	if decoded, err := url.PathUnescape(m[1]); err == nil {
		return decoded, true
	}
	return m[1], true
}

// selectGCLArtifactVolumes picks the gcl build volumes belonging to this run.
// Because the jobId in the name is random, volume names cannot be predicted up
// front. We instead keep volumes that are new since preRun, end in "-build", and
// whose decoded job-name segment matches one of jobNames — this prevents a
// concurrent glut/gitlab-ci-local run on the same daemon from having its volumes
// extracted and destroyed. When jobNames is empty (e.g. the pipeline failed
// before producing any job output) the nominal filter is skipped (temporal-only
// fallback). Results are sorted so multi-job extraction order is deterministic.
func selectGCLArtifactVolumes(preRun, current, jobNames []string) []string {
	preRunSet := make(map[string]struct{}, len(preRun))
	for _, v := range preRun {
		preRunSet[v] = struct{}{}
	}

	scoped := len(jobNames) > 0
	want := make(map[string]struct{}, len(jobNames))
	for _, name := range jobNames {
		want[name] = struct{}{}
	}

	var out []string
	for _, vol := range current {
		if _, existed := preRunSet[vol]; existed {
			continue
		}
		if !strings.HasSuffix(vol, "-build") {
			continue
		}
		if scoped {
			seg, ok := GCLJobName(vol)
			if !ok {
				continue
			}
			if _, ok := want[seg]; !ok {
				continue
			}
		}
		out = append(out, vol)
	}
	sort.Strings(out)
	return out
}

// extractGCLBuildVolume tars the contents of a gcl-*-build volume (excluding
// .git and .gitlab-ci-local) and extracts them into workspaceDir.
func extractGCLBuildVolume(volName, workspaceDir string) error {
	ctrName := "glut-gcl-" + volName
	tarCmd := exec.Command("docker", "run", "--name", ctrName,
		"--volume", volName+":/vol",
		"alpine", "tar", "-cC", "/vol",
		"--exclude=./.git",
		"--exclude=./.gitlab-ci-local",
		".")
	var stdout, stderr bytes.Buffer
	tarCmd.Stdout = &stdout
	tarCmd.Stderr = &stderr
	runErr := tarCmd.Run()
	_ = exec.Command("docker", "rm", ctrName).Run()
	if runErr != nil {
		// tar of an existing volume always succeeds (an empty volume yields an
		// empty archive), so a non-zero exit is a real failure — surface it
		// rather than silently leaving the artifacts un-extracted.
		if se := bytes.TrimSpace(stderr.Bytes()); len(se) > 0 {
			return fmt.Errorf("read gcl volume artifacts: %w (%s)", runErr, se)
		}
		return fmt.Errorf("read gcl volume artifacts: %w", runErr)
	}

	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return fmt.Errorf("create workspace dir: %w", err)
	}
	extractCmd := exec.Command("tar", "-xC", workspaceDir)
	extractCmd.Stdin = &stdout
	if out, err := extractCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extract gcl volume artifacts: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
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
