package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/docker"
	"github.com/pandasoft-zz/glut/internal/executor"
	"github.com/pandasoft-zz/glut/internal/mockwrapper"
	"github.com/pandasoft-zz/glut/internal/parser"
	"github.com/pandasoft-zz/glut/internal/workspace"
)

// fakeDockerForVolumeOps installs a fake `docker` CLI so the named-volume
// phases of testRun can run without a daemon. Steered via FAKE_* env vars;
// `docker run ... tar -cC` emits a tar of FAKE_TAR_SRC.
func fakeDockerForVolumeOps(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake docker CLI is a POSIX shell script")
	}
	binDir := t.TempDir()
	script := `#!/bin/sh
case "$*" in
  "volume create "*) exit 0 ;;
  "volume rm "*) exit 0 ;;
  "volume ls "*)
    if [ -n "$FAKE_GCL_VOLUMES" ]; then printf '%s\n' $FAKE_GCL_VOLUMES; fi
    exit 0 ;;
  "ps "*) exit 0 ;;
  "rm "*) exit 0 ;;
  *" sync") exit 0 ;;
  *" tar -xC "*) cat >/dev/null; exit 0 ;;
  *" tar -cC "*)
    if [ "${FAKE_TAR_EXIT:-0}" != 0 ]; then echo "tar blew up" >&2; exit "$FAKE_TAR_EXIT"; fi
    tar -cC "$FAKE_TAR_SRC" .
    exit 0 ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "docker"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func mockedTestFile() parser.TestFile {
	return parser.TestFile{Glut: parser.GlutSection{
		Setup: parser.SetupConfig{Mocks: &parser.MocksConfig{Binaries: map[string]parser.BinaryMockConfig{
			"release-cli": {Executable: "echo release"},
		}}},
		Assert: config.AssertConfig{
			Binary:    map[string]config.BinaryAssert{"release-cli": {}},
			Artifacts: map[string]config.ArtifactAssert{"out.txt": {}},
		},
	}}
}

func TestSetupDockerVolumeAndMocksVolumeStrategy(t *testing.T) {
	fakeDockerForVolumeOps(t)
	t.Setenv("FAKE_GCL_VOLUMES", "gcl-old-1-build")

	glutBin := filepath.Join(t.TempDir(), "glut")
	if err := os.WriteFile(glutBin, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	originRepo := t.TempDir()
	if err := os.WriteFile(filepath.Join(originRepo, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	r := &testRun{
		suite:        &suiteRun{volumeStrategy: docker.VolumeStrategyVolume, opts: RunOptions{GlutBinPath: glutBin}},
		testFile:     mockedTestFile(),
		useDocker:    true,
		work:         &workspace.Workspace{Dir: workDir, OriginRepo: originRepo},
		phaseTimings: map[string]time.Duration{},
	}
	if err := r.setupDockerVolumeAndMocks(); err != nil {
		t.Fatalf("setupDockerVolumeAndMocks() error = %v", err)
	}
	if r.dockerVolumeName != "glut-"+filepath.Base(workDir) {
		t.Fatalf("dockerVolumeName = %q", r.dockerVolumeName)
	}
	if !r.needsGCLArtifacts || len(r.preRunGCLVolumes) != 1 {
		t.Fatalf("gcl artifact snapshot = needs %v, preRun %v", r.needsGCLArtifacts, r.preRunGCLVolumes)
	}
	if _, err := os.Stat(filepath.Join(workspace.MockBinaryBinDir(workDir), "release-cli")); err != nil {
		t.Fatalf("mock binary not installed: %v", err)
	}
	if _, ok := r.phaseTimings["mock-binaries"]; !ok {
		t.Fatal("mock-binaries phase timing not recorded")
	}
}

func TestCollectMockLogsSyncsFromDockerVolume(t *testing.T) {
	fakeDockerForVolumeOps(t)

	// The fake `docker run ... tar -cC` streams this directory back as the
	// volume's mock-logs content.
	src := t.TempDir()
	line := `{"name":"release-cli","args":["create"],"stdin":""}` + "\n"
	if err := os.WriteFile(filepath.Join(src, "release-cli.jsonl"), []byte(line), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TAR_SRC", src)

	r := &testRun{
		suite:            &suiteRun{},
		testFile:         mockedTestFile(),
		dockerVolumeName: "glut-abc",
		work:             &workspace.Workspace{Dir: t.TempDir()},
		phaseTimings:     map[string]time.Duration{},
		binaryLogs:       map[string][]mockwrapper.BinaryCall{},
	}
	if err := r.collectMockLogs(); err != nil {
		t.Fatalf("collectMockLogs() error = %v", err)
	}
	calls := r.binaryLogs["release-cli"]
	if len(calls) != 1 || calls[0].Args[0] != "create" {
		t.Fatalf("binaryLogs = %#v, want the synced release-cli call", r.binaryLogs)
	}
	if _, ok := r.phaseTimings["mock-logs"]; !ok {
		t.Fatal("mock-logs phase timing not recorded")
	}
}

func TestFetchGCLArtifactsExtractsIntoWorkspace(t *testing.T) {
	fakeDockerForVolumeOps(t)
	t.Setenv("FAKE_GCL_VOLUMES", "gcl-build-9-build")

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "out.txt"), []byte("artifact"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TAR_SRC", src)

	workspaceDir := t.TempDir()
	r := &testRun{
		suite:             &suiteRun{},
		needsGCLArtifacts: true,
		work:              &workspace.Workspace{WorkspaceDir: workspaceDir},
		result: &TestResult{JobOutputs: map[string]executor.JobOutput{
			"build": {Name: "build", Present: true, Executed: true},
		}},
	}
	if err := r.fetchGCLArtifacts(); err != nil {
		t.Fatalf("fetchGCLArtifacts() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspaceDir, "out.txt")); err != nil {
		t.Fatalf("artifact not extracted: %v", err)
	}

	// The nominal filter must have matched the "build" job volume.
	r2 := &testRun{suite: &suiteRun{}, needsGCLArtifacts: false}
	if err := r2.fetchGCLArtifacts(); err != nil {
		t.Fatalf("fetchGCLArtifacts() without artifacts = %v, want nil", err)
	}
}

func TestResolveOriginSourceFetchesFromDockerVolume(t *testing.T) {
	fakeDockerForVolumeOps(t)

	src := t.TempDir()
	originDir := filepath.Join(src, ".glut-origin.git")
	if err := os.MkdirAll(originDir, 0755); err != nil {
		t.Fatal(err)
	}
	head := "ref: refs/heads/main\n"
	if err := os.WriteFile(filepath.Join(originDir, "HEAD"), []byte(head), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TAR_SRC", src)

	gitAssert := &config.GitAssert{Origin: &config.GitRepoAssert{}}
	r := &testRun{
		suite:            &suiteRun{},
		testFile:         parser.TestFile{Glut: parser.GlutSection{Assert: config.AssertConfig{Git: gitAssert}}},
		dockerVolumeName: "glut-abc",
		work:             &workspace.Workspace{Dir: t.TempDir(), OriginRepo: "/host/origin"},
	}

	source, closeOrigin := r.resolveOriginSource()
	path, err := source.Path()
	if err != nil {
		t.Fatalf("origin Path() error = %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(path, "HEAD")); err != nil || string(data) != head {
		t.Fatalf("origin HEAD = %q, %v", data, err)
	}
	closeOrigin()
	if r.primaryErr != nil {
		t.Fatalf("closeOrigin recorded error: %v", r.primaryErr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("lazy origin temp dir must be removed on close, stat err = %v", err)
	}
}

func TestResolveOriginSourceFallsBackToHostOnFetchError(t *testing.T) {
	fakeDockerForVolumeOps(t)
	t.Setenv("FAKE_TAR_EXIT", "1")
	t.Setenv("FAKE_TAR_SRC", t.TempDir())

	gitAssert := &config.GitAssert{Origin: &config.GitRepoAssert{}}
	r := &testRun{
		suite:            &suiteRun{},
		testFile:         parser.TestFile{Glut: parser.GlutSection{Assert: config.AssertConfig{Git: gitAssert}}},
		dockerVolumeName: "glut-abc",
		work:             &workspace.Workspace{Dir: t.TempDir(), OriginRepo: "/host/origin"},
	}

	source, closeOrigin := r.resolveOriginSource()
	defer closeOrigin()
	if r.primaryErr == nil || !strings.Contains(r.primaryErr.Error(), "fetch git origin from docker volume") {
		t.Fatalf("primaryErr = %v, want the fetch failure recorded", r.primaryErr)
	}
	if path, err := source.Path(); err != nil || path != "/host/origin" {
		t.Fatalf("fallback source = %q, %v, want the host origin", path, err)
	}
}
