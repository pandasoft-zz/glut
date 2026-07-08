package workspace

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pandasoft-zz/glut/internal/parser"
)

// fakeDockerCLI installs a fake `docker` script at the front of PATH so the
// docker-volume plumbing can be exercised without a daemon. Behaviour is
// steered through FAKE_* environment variables; every invocation is appended
// to the returned log file. t.Setenv makes the test non-parallel, which also
// keeps the PATH mutation safe.
func fakeDockerCLI(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake docker CLI is a POSIX shell script")
	}

	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "calls.log")
	script := `#!/bin/sh
echo "$*" >> "$FAKE_DOCKER_LOG"
case "$*" in
  "volume create "*) exit "${FAKE_VOLUME_CREATE_EXIT:-0}" ;;
  "volume rm "*) exit 0 ;;
  "volume ls "*)
    if [ -n "$FAKE_GCL_VOLUMES" ]; then printf '%s\n' $FAKE_GCL_VOLUMES; fi
    exit "${FAKE_VOLUME_LS_EXIT:-0}" ;;
  "ps "*)
    if [ -n "$FAKE_PS_IDS" ]; then printf '%s\n' $FAKE_PS_IDS; fi
    exit 0 ;;
  "rm "*) exit 0 ;;
  *" sync") exit "${FAKE_SYNC_EXIT:-0}" ;;
  *" tar -xC "*) cat >/dev/null; exit "${FAKE_POPULATE_EXIT:-0}" ;;
  *" tar -cC "*)
    if [ "${FAKE_TAR_EXIT:-0}" != 0 ]; then echo "tar blew up" >&2; exit "$FAKE_TAR_EXIT"; fi
    tar -cC "$FAKE_TAR_SRC" .
    exit 0 ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DOCKER_LOG", logPath)
	return logPath
}

func fakeDockerCalls(t *testing.T, logPath string) string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("read fake docker log: %v", err)
	}
	return string(data)
}

func TestInfraErrorWrapsUnderlying(t *testing.T) {
	t.Parallel()
	underlying := errors.New("daemon fell over")
	infra := &InfraError{Err: fmt.Errorf("populate: %w", underlying)}
	if infra.Error() != "populate: daemon fell over" {
		t.Fatalf("Error() = %q", infra.Error())
	}
	if !errors.Is(infra, underlying) {
		t.Fatal("errors.Is must reach the underlying error through Unwrap")
	}
}

func TestCreateDockerVolumePopulatesViaFakeDocker(t *testing.T) {
	logPath := fakeDockerCLI(t)

	workDir := t.TempDir()
	originRepo := filepath.Join(t.TempDir(), "origin.git")
	if err := os.MkdirAll(originRepo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(originRepo, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mocks := &parser.MocksConfig{Binaries: map[string]parser.BinaryMockConfig{
		"release-cli": {Executable: "echo release"},
	}}

	volName, err := CreateDockerVolume(workDir, originRepo, mocks)
	if err != nil {
		t.Fatalf("CreateDockerVolume() error = %v", err)
	}
	want := "glut-" + filepath.Base(workDir)
	if volName != want {
		t.Fatalf("volume name = %q, want %q", volName, want)
	}

	calls := fakeDockerCalls(t, logPath)
	if !strings.Contains(calls, "volume create "+want) {
		t.Fatalf("missing volume create call:\n%s", calls)
	}
	if !strings.Contains(calls, "tar -xC "+workDir) {
		t.Fatalf("missing populate call:\n%s", calls)
	}
}

func TestCreateDockerVolumeCreateFailureIsInfraError(t *testing.T) {
	fakeDockerCLI(t)
	t.Setenv("FAKE_VOLUME_CREATE_EXIT", "1")

	origin := t.TempDir()
	_, err := CreateDockerVolume(t.TempDir(), origin, nil)
	var infra *InfraError
	if !errors.As(err, &infra) {
		t.Fatalf("CreateDockerVolume() error = %v, want *InfraError", err)
	}
}

func TestCreateDockerVolumePopulateFailureRetriesThenInfraError(t *testing.T) {
	logPath := fakeDockerCLI(t)
	t.Setenv("FAKE_POPULATE_EXIT", "1")

	origin := t.TempDir()
	_, err := CreateDockerVolume(t.TempDir(), origin, nil)
	var infra *InfraError
	if !errors.As(err, &infra) {
		t.Fatalf("CreateDockerVolume() error = %v, want *InfraError", err)
	}
	calls := fakeDockerCalls(t, logPath)
	if got := strings.Count(calls, "tar -xC"); got != volumePopulateAttempts {
		t.Fatalf("populate attempts = %d, want %d:\n%s", got, volumePopulateAttempts, calls)
	}
	if !strings.Contains(calls, "volume rm") {
		t.Fatalf("failed create must remove the volume:\n%s", calls)
	}
}

func TestSyncDockerVolume(t *testing.T) {
	fakeDockerCLI(t)
	if err := SyncDockerVolume("glut-abc", "/work"); err != nil {
		t.Fatalf("SyncDockerVolume() error = %v", err)
	}

	t.Setenv("FAKE_SYNC_EXIT", "1")
	if err := SyncDockerVolume("glut-abc", "/work"); err == nil {
		t.Fatal("SyncDockerVolume() error = nil, want failure")
	}
}

func TestReadLogsFromDockerVolumeExtractsTar(t *testing.T) {
	fakeDockerCLI(t)

	// The fake `docker run ... tar -cC` emits a tar of FAKE_TAR_SRC.
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "release-cli.jsonl"), []byte("{\"name\":\"release-cli\"}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TAR_SRC", src)

	workDir := t.TempDir()
	if err := ReadLogsFromDockerVolume("glut-abc", workDir); err != nil {
		t.Fatalf("ReadLogsFromDockerVolume() error = %v", err)
	}
	extracted := filepath.Join(MockBinaryLogDir(workDir), "release-cli.jsonl")
	if _, err := os.Stat(extracted); err != nil {
		t.Fatalf("expected extracted log file: %v", err)
	}
}

func TestFetchGitOriginTarReturnsArchiveBytes(t *testing.T) {
	fakeDockerCLI(t)

	src := t.TempDir()
	originDir := filepath.Join(src, ".glut-origin.git")
	if err := os.MkdirAll(originDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(originDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TAR_SRC", src)

	data, err := FetchGitOriginTar("glut-abc", "/work")
	if err != nil {
		t.Fatalf("FetchGitOriginTar() error = %v", err)
	}
	if len(data) == 0 {
		t.Fatal("FetchGitOriginTar() returned empty archive")
	}

	t.Setenv("FAKE_TAR_EXIT", "1")
	if _, err := FetchGitOriginTar("glut-abc", "/work"); err == nil || !strings.Contains(err.Error(), "tar blew up") {
		t.Fatalf("FetchGitOriginTar() error = %v, want stderr in message", err)
	}
}

func TestListGCLVolumes(t *testing.T) {
	fakeDockerCLI(t)
	t.Setenv("FAKE_GCL_VOLUMES", "gcl-build-1-build gcl-build-1-tmp")

	got := ListGCLVolumes()
	if len(got) != 2 || got[0] != "gcl-build-1-build" {
		t.Fatalf("ListGCLVolumes() = %v", got)
	}

	t.Setenv("FAKE_VOLUME_LS_EXIT", "1")
	if got := ListGCLVolumes(); got != nil {
		t.Fatalf("ListGCLVolumes() on error = %v, want nil", got)
	}
}

func TestDestroyDockerVolumeRemovesReferencingContainers(t *testing.T) {
	logPath := fakeDockerCLI(t)
	t.Setenv("FAKE_PS_IDS", "c0ffee c0de")

	if err := DestroyDockerVolume("glut-abc"); err != nil {
		t.Fatalf("DestroyDockerVolume() error = %v", err)
	}
	calls := fakeDockerCalls(t, logPath)
	for _, want := range []string{"ps -a --filter volume=glut-abc", "rm c0ffee", "rm c0de", "volume rm glut-abc"} {
		if !strings.Contains(calls, want) {
			t.Fatalf("missing %q in fake docker calls:\n%s", want, calls)
		}
	}
}

func TestFetchArtifactsFromGCLVolumesExtractsAndRemoves(t *testing.T) {
	logPath := fakeDockerCLI(t)

	// One new build volume for our job appears after the run.
	t.Setenv("FAKE_GCL_VOLUMES", "gcl-build-77-build")
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "artifact.txt"), []byte("payload"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TAR_SRC", src)

	workspaceDir := t.TempDir()
	if err := FetchArtifactsFromGCLVolumes(nil, []string{"build"}, workspaceDir); err != nil {
		t.Fatalf("FetchArtifactsFromGCLVolumes() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(workspaceDir, "artifact.txt")); err != nil {
		t.Fatalf("expected extracted artifact: %v", err)
	}
	calls := fakeDockerCalls(t, logPath)
	if !strings.Contains(calls, "volume rm gcl-build-77-build") {
		t.Fatalf("build volume must be removed:\n%s", calls)
	}
	if !strings.Contains(calls, "volume rm gcl-build-77-tmp") {
		t.Fatalf("companion tmp volume must be removed:\n%s", calls)
	}
}

func TestExtractGCLBuildVolumeSurfacesTarFailure(t *testing.T) {
	fakeDockerCLI(t)
	t.Setenv("FAKE_TAR_EXIT", "1")

	err := extractGCLBuildVolume("gcl-build-1-build", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "tar blew up") {
		t.Fatalf("extractGCLBuildVolume() error = %v, want stderr in message", err)
	}
}

func TestBuildVolumeArchiveContainsWrappersScriptsAndOrigin(t *testing.T) {
	t.Parallel()

	originRepo := t.TempDir()
	if err := os.WriteFile(filepath.Join(originRepo, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(originRepo, "refs", "heads"), 0755); err != nil {
		t.Fatal(err)
	}

	mocks := &parser.MocksConfig{Binaries: map[string]parser.BinaryMockConfig{
		"docker": {Executable: "echo built"},
	}}
	reader, err := buildVolumeArchive("/work", originRepo, mocks)
	if err != nil {
		t.Fatalf("buildVolumeArchive() error = %v", err)
	}

	entries := map[string]string{}
	tr := tar.NewReader(reader)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read archive: %v", err)
		}
		var body bytes.Buffer
		if _, err := io.Copy(&body, tr); err != nil { //nolint:gosec // bounded test archive
			t.Fatalf("read entry %s: %v", hdr.Name, err)
		}
		entries[hdr.Name] = body.String()
	}

	if _, ok := entries["mock-logs/"]; !ok {
		t.Fatalf("archive missing mock-logs dir; entries: %v", keys(entries))
	}
	if wrapper := entries["bin/docker"]; !strings.Contains(wrapper, "GLUT_MOCK_BIN_REAL") {
		t.Fatalf("bin/docker must contain the shell mock wrapper, got %q", wrapper)
	}
	if script := entries["bin-real/docker"]; !strings.Contains(script, "echo built") {
		t.Fatalf("bin-real/docker must contain the mock executable, got %q", script)
	}
	if head := entries[".glut-origin.git/HEAD"]; !strings.Contains(head, "refs/heads/main") {
		t.Fatalf("origin HEAD not archived, got %q", head)
	}
}

func TestBuildVolumeArchiveRejectsInvalidMockName(t *testing.T) {
	t.Parallel()
	mocks := &parser.MocksConfig{Binaries: map[string]parser.BinaryMockConfig{
		"bad/name": {Executable: "echo"},
	}}
	if _, err := buildVolumeArchive("/work", t.TempDir(), mocks); err == nil {
		t.Fatal("buildVolumeArchive() must reject a mock name with a path separator")
	}
}

func TestAddDirToArchiveDereferencesSymlinks(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "real.txt"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(src, "real.txt"), filepath.Join(src, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := addDirToArchive(tw, src, "prefix"); err != nil {
		t.Fatalf("addDirToArchive() error = %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	tr := tar.NewReader(&buf)
	var linkBody string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == "prefix/link.txt" {
			if hdr.Typeflag != tar.TypeReg {
				t.Fatalf("symlink must be archived as a regular file, got typeflag %v", hdr.Typeflag)
			}
			var body bytes.Buffer
			if _, err := io.Copy(&body, tr); err != nil { //nolint:gosec // bounded test archive
				t.Fatal(err)
			}
			linkBody = body.String()
		}
	}
	if linkBody != "content" {
		t.Fatalf("dereferenced symlink body = %q, want %q", linkBody, "content")
	}
}

func TestLazyTarOriginExtractsOnceAndCleansUp(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("extraction shells out to tar")
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	head := []byte("ref: refs/heads/main\n")
	if err := tw.WriteHeader(&tar.Header{Name: ".glut-origin.git/", Mode: 0755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: ".glut-origin.git/HEAD", Mode: 0644, Size: int64(len(head)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(head); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	origin := NewLazyTarOrigin(buf.Bytes())
	path1, err := origin.Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(path1, "HEAD")); err != nil || string(data) != string(head) {
		t.Fatalf("extracted HEAD = %q, %v", data, err)
	}

	path2, err := origin.Path()
	if err != nil || path2 != path1 {
		t.Fatalf("second Path() = %q, %v — must reuse the first extraction %q", path2, err, path1)
	}

	if err := origin.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Stat(path1); !os.IsNotExist(err) {
		t.Fatalf("Close() must remove the temp dir, stat err = %v", err)
	}
	if err := origin.Close(); err != nil {
		t.Fatalf("second Close() must be a no-op, got %v", err)
	}
}

func TestLazyTarOriginCorruptArchiveFails(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("extraction shells out to tar")
	}
	origin := NewLazyTarOrigin([]byte("this is not a tar archive"))
	if _, err := origin.Path(); err == nil {
		t.Fatal("Path() on a corrupt archive must fail")
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
