package runner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pandasoft-zz/glut/internal/asserter"
	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/docker"
	"github.com/pandasoft-zz/glut/internal/executor"
	"github.com/pandasoft-zz/glut/internal/mockserver"
	"github.com/pandasoft-zz/glut/internal/mockwrapper"
	"github.com/pandasoft-zz/glut/internal/parser"
	"github.com/pandasoft-zz/glut/internal/workspace"
)

// testRun carries one test's mutable state through its execution phases.
// runSingleTest owns the lifecycle: it runs the phases in order and holds the
// single cleanup defer (finalize). Each phase stores what later phases need
// on the struct and returns its error already wrapped with phase context.
type testRun struct {
	suite    *suiteRun
	testFile parser.TestFile

	// result points at runSingleTest's named return value, so phases and
	// finalize mutate what the caller receives.
	result *TestResult

	work             *workspace.Workspace
	server           *mockserver.Server
	execResult       executor.RunResult
	execCfg          executor.ExecutorConfig
	phaseTimings     map[string]time.Duration
	binaryLogs       map[string][]mockwrapper.BinaryCall
	apiCalls         []mockserver.APICall
	cleanupErrors    []error
	primaryErr       error
	dockerVolumeName string

	useDocker         bool
	forceShell        bool
	usePrivileged     bool
	needsGCLArtifacts bool
	preRunGCLVolumes  []string
	mockHostIP        string
	gitConfigEnv      map[string]string
}

// runSingleTest executes one test file end to end. Setup phases fail fast;
// from the pipeline on, phases are best-effort so assertions and debug data
// reflect as much of the pipeline outcome as possible.
func (s *suiteRun) runSingleTest(ctx context.Context, testFile parser.TestFile) (result TestResult) {
	if testFile.ParseError != nil {
		fp := relPath(s.repoRoot, testFile.FilePath)
		return TestResult{
			FilePath: fp,
			TestName: fp,
			Passed:   false,
			Error:    testFile.ParseError,
		}
	}

	result = TestResult{
		FilePath:   relPath(s.repoRoot, testFile.FilePath),
		TestName:   testFile.Glut.Name,
		JobOutputs: map[string]executor.JobOutput{},
	}

	r := &testRun{
		suite:        s,
		testFile:     testFile,
		result:       &result,
		phaseTimings: map[string]time.Duration{},
		binaryLogs:   map[string][]mockwrapper.BinaryCall{},
	}

	defer r.finalize()

	testStart := time.Now()
	defer func() {
		result.Duration = time.Since(testStart)
	}()

	// Setup phases fail fast: an error here means the pipeline never ran,
	// so there is nothing to assert on or to collect.
	if r.primaryErr = r.createWorkspace(); r.primaryErr != nil {
		return
	}
	if r.primaryErr = r.startMockServer(); r.primaryErr != nil {
		return
	}
	if r.primaryErr = r.setupDockerVolumeAndMocks(); r.primaryErr != nil {
		return
	}
	if r.primaryErr = r.buildExecConfig(); r.primaryErr != nil {
		return
	}

	if r.primaryErr = maybePause(s.opts.DebugPause, "before-pipeline", r.work.Dir); r.primaryErr != nil {
		return
	}

	// ListJobs and Run each apply cfg.Timeout independently via their own
	// context; without a shared deadline, a test that needs both invocations
	// could take up to 2x --timeout in the worst case. Derive one deadline
	// here and let both share it.
	execCtx, cancelExec := sharedExecutorDeadline(ctx, s.opts.Timeout)
	defer cancelExec()

	if needsJobList(testFile) {
		if r.primaryErr = r.listJobs(execCtx); r.primaryErr != nil {
			return
		}
	}

	// From the pipeline on, phases are best-effort: the first error is kept
	// as the root cause (recordErr), but later phases still run.
	r.recordErr(r.runPipeline(execCtx))
	r.recordErr(r.collectMockLogs())
	r.recordErr(r.fetchGCLArtifacts())

	originSource, closeOrigin := r.resolveOriginSource()
	defer closeOrigin()

	r.recordErr(maybePause(s.opts.DebugPause, "before-asserts", r.work.Dir))

	r.runAsserts(originSource)

	r.recordErr(maybePauseOnFail(s.opts.DebugPause, result.Passed, r.work.Dir))

	return
}

// timePhase starts timing a named phase and returns the stop function that
// records the elapsed time, usually invoked via defer.
func (r *testRun) timePhase(name string) func() {
	start := time.Now()
	return func() { r.phaseTimings[name] = time.Since(start) }
}

// recordErr keeps the first error of the run: later best-effort failures must
// not overwrite the root cause that made the test fail.
func (r *testRun) recordErr(err error) {
	if err != nil && r.primaryErr == nil {
		r.primaryErr = err
	}
}

// createWorkspace prepares the isolated workspace and fake git origin.
func (r *testRun) createWorkspace() error {
	defer r.timePhase("workspace")()
	work, err := workspace.New(r.testFile.Glut.Setup, false, r.suite.repoRoot, workspace.Options{
		CopyStrategy: r.suite.opts.CopyStrategy,
		Include:      r.suite.opts.Include,
		Verbose:      r.suite.opts.Verbose,
		HostEnv:      r.suite.opts.HostEnv,
		TempDir:      r.suite.opts.WorkspaceTempDir,
	})
	if err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	r.work = work
	return nil
}

// startMockServer creates and starts the mock GitLab API server. The Docker
// mode is resolved first because the bind address is scoped to loopback for
// shell-only tests — Docker jobs need the mock server reachable from the
// bridge network, but a shell-only job never does, and 0.0.0.0 is reachable
// by any host on a shared network.
func (r *testRun) startMockServer() error {
	r.useDocker, r.forceShell = resolveDockerMode(r.testFile.Glut.Setup.Docker)

	stop := r.timePhase("mockserver-new")
	server, err := mockserver.New(apiConfig(r.testFile))
	stop()
	r.server = server
	if err != nil {
		return fmt.Errorf("create mock server: %w", err)
	}

	stop = r.timePhase("mockserver-start")
	err = r.server.Start(r.useDocker)
	stop()
	if err != nil {
		return fmt.Errorf("start mock server: %w", err)
	}
	r.server.SetGitRepo(r.work.OriginRepo)
	return nil
}

// setupDockerVolumeAndMocks prepares what jobs need to find at runtime.
//
// Named volume strategy: populate a Docker volume from the host workspace and
// hand it to the suite's deferred bulk cleanup. Bind mount strategy: the host
// workspace directory is already populated by workspace.New and
// SetupMockBinaries, so no extra Docker operation is needed.
//
// Mock binaries are always injected into the shell PATH so jobs without
// image: can find them regardless of docker mode. In Docker mode the wrapper
// binary is copied inside the workspace directory (the mount root inside the
// container) instead of being symlinked to a host path that is invisible to
// the job container.
func (r *testRun) setupDockerVolumeAndMocks() error {
	defer r.timePhase("mock-binaries")()

	r.usePrivileged = r.testFile.Glut.Setup.Privileged != nil && *r.testFile.Glut.Setup.Privileged

	// Snapshot gcl volumes before the pipeline so FetchArtifactsFromGCLVolumes
	// can identify which ones belong to this run (volume strategy only, where
	// bind mounts do not work and gitlab-ci-local uses its own named volumes).
	r.needsGCLArtifacts = r.useDocker &&
		r.suite.volumeStrategy == docker.VolumeStrategyVolume &&
		len(r.testFile.Glut.Assert.Artifacts) > 0
	if r.needsGCLArtifacts {
		r.preRunGCLVolumes = workspace.ListGCLVolumes()
	}

	if r.useDocker && r.suite.volumeStrategy == docker.VolumeStrategyVolume {
		var mocks *parser.MocksConfig
		if hasMockBinaries(r.testFile) {
			mocks = r.testFile.Glut.Setup.Mocks
		}
		volumeName, err := workspace.CreateDockerVolume(r.work.Dir, r.work.OriginRepo, mocks)
		// Record the name even on error so a partially-created volume still
		// reaches the suite's deferred cleanup.
		r.dockerVolumeName = volumeName
		if err != nil {
			return fmt.Errorf("setup mock binaries: %w", err)
		}
	}

	if hasMockBinaries(r.testFile) {
		if err := workspace.SetupMockBinaries(r.work.Dir, *r.testFile.Glut.Setup.Mocks, resolveGlutBinPath(r.suite.opts.GlutBinPath), r.useDocker); err != nil {
			return fmt.Errorf("setup mock binaries: %w", err)
		}
	}
	return nil
}

// buildExecConfig collects everything the executor invocation needs: the
// workspace HEAD metadata, CI variables, Docker networking, the optional
// integration-mode component fetch, and the final ExecutorConfig.
func (r *testRun) buildExecConfig() error {
	stop := r.timePhase("git-head")
	sha, shortSHA, err := gitHeads(r.work.WorkspaceDir)
	stop()
	if err != nil {
		return err
	}
	commitMessage, commitTimestamp, err := gitHeadCommit(r.work.WorkspaceDir)
	if err != nil {
		return err
	}

	if r.useDocker {
		r.mockHostIP = outboundIP()
	}

	envVars := r.work.EnvVars(r.testFile.Glut.Setup, r.server.Port(), sha, shortSHA, r.testFile.Glut.Name)
	workspace.ApplyCommitEnv(envVars, commitMessage, commitTimestamp)
	applyDockerCompatibilityEnv(envVars, r.useDocker)
	if r.useDocker {
		// BUG-3: Docker containers cannot reach 127.0.0.1. Use the bridge IP directly
		// so the URL works for both Docker jobs (container on same bridge) and shell jobs
		// (same host or container). glut-mock remains an alias via --extra-host.
		workspace.ApplyServerBaseURL(envVars, r.mockHostIP, r.server.Port())
	}

	// Integration mode: resolve `include: component:` against a real GitLab using
	// the real CI_JOB_TOKEN, so a composite component runs with its real
	// sub-components. Only the component fetch becomes real (via a gcl-origin
	// remote + an insteadOf credential rewrite injected into gitlab-ci-local's
	// environment); the runtime GitLab API stays mocked, and origin/sandbox are
	// untouched.
	if componentsRealFetch(r.testFile.Glut.Setup) {
		fetch, err := workspace.RealComponentFetch(r.suite.opts.HostEnv)
		if err != nil {
			return fmt.Errorf("components.fetch real: %w", err)
		}
		if err := r.work.SetGCLOriginRemote(fetch.GCLOriginURL); err != nil {
			return fmt.Errorf("components.fetch real: %w", err)
		}
		// CI_PROJECT_NAMESPACE: the component address path segment.
		// CI_SERVER_FQDN: the address domain — also used by GCL's `git ls-remote
		// --tags` step that resolves numeric/~latest refs (e.g. @1), so it must
		// point at the real server, not the mock. Set after ApplyServerBaseURL so
		// it is not overwritten by the Docker bridge-IP value.
		envVars["CI_PROJECT_NAMESPACE"] = fetch.Namespace
		envVars["CI_SERVER_FQDN"] = fetch.ServerFQDN
		r.gitConfigEnv = fetch.GitConfigEnv
	}

	r.execCfg = executor.ExecutorConfig{
		WorkspacePath:       r.work.WorkspaceDir,
		PipelineYAML:        r.testFile.PipelineYAML,
		EnvVars:             envVars,
		UnsetVars:           r.work.UnsetVars(r.testFile.Glut.Setup),
		MockBinPath:         workspace.MockBinaryBinDir(r.work.Dir),
		Timeout:             r.suite.opts.Timeout,
		Debug:               r.suite.opts.Debug,
		Verbose:             r.suite.opts.Verbose,
		UseDocker:           r.useDocker,
		ForceShell:          r.forceShell,
		Privileged:          r.usePrivileged,
		DockerVolumes:       dockerVolumes(r.useDocker, r.work.Dir, r.dockerVolumeName, r.suite.volumeStrategy),
		DockerExtraHosts:    dockerExtraHosts(r.useDocker, r.mockHostIP),
		HostEnv:             r.suite.opts.HostEnv,
		KeepDockerResources: r.needsGCLArtifacts,
		GitConfigEnv:        r.gitConfigEnv,
		// dockerVolumeName is only set for the named-volume strategy (see
		// setupDockerVolumeAndMocks); bind-mount workspaces have no volume
		// for the output monitor to watch.
		MonitorVolume: r.dockerVolumeName,
	}
	return nil
}

// listJobs pre-populates JobOutputs with the pipeline's declared jobs, which
// present:/when: assertions need even for jobs that never run.
func (r *testRun) listJobs(execCtx context.Context) error {
	defer r.timePhase("list-jobs")()
	jobEntries, err := executor.ListJobs(execCtx, r.execCfg)
	if err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}
	for _, entry := range jobEntries {
		r.result.JobOutputs[entry.Name] = executor.JobOutput{Name: entry.Name, Present: true, When: entry.When}
	}
	return nil
}

// runPipeline executes gitlab-ci-local and merges its job outputs into the
// result. Entries pre-populated by listJobs keep their evaluated `when` — the
// run output does not carry it — and executed entries gain Executed, which
// the infra-retry heuristic in Run relies on to tell "pipeline produced
// output" apart from "daemon failed before any job ran".
func (r *testRun) runPipeline(execCtx context.Context) error {
	defer r.timePhase("pipeline")()
	execResult, err := executor.Run(execCtx, r.execCfg)
	r.execResult = execResult
	for name, output := range execResult.Jobs {
		output.Present = true
		output.Executed = true
		if existing, ok := r.result.JobOutputs[name]; ok {
			output.When = existing.When
		}
		r.result.JobOutputs[name] = output
	}
	if err != nil {
		return fmt.Errorf("run pipeline: %w", err)
	}
	return nil
}

// collectMockLogs copies mock-binary call logs back from the Docker volume
// (named-volume strategy), verifies no wrapper was killed mid-write, and
// reads the logs. Logs are read only for binaries that have assertions — a
// partially-written or missing log for an un-asserted binary must not fail
// the test.
func (r *testRun) collectMockLogs() error {
	defer r.timePhase("mock-logs")()
	if !hasMockBinaries(r.testFile) {
		return nil
	}

	var firstErr error
	record := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if r.dockerVolumeName != "" {
		// Flush filesystem writes inside the volume before copying logs so
		// that all wrapper writes are visible to the tar command.
		if err := workspace.SyncDockerVolume(r.dockerVolumeName, r.work.Dir); err != nil {
			record(fmt.Errorf("sync docker volume: %w", err))
		}
		if err := workspace.ReadLogsFromDockerVolume(r.dockerVolumeName, r.work.Dir); err != nil {
			record(fmt.Errorf("sync mock logs from docker volume: %w", err))
		}
	}

	// Detect wrapper processes killed before completing their log write.
	if err := mockwrapper.CheckMockLogBarriers(workspace.MockBinaryLogDir(r.work.Dir)); err != nil {
		record(fmt.Errorf("mock log write interrupted: %w", err))
	}

	assertedBinaries := make(map[string]struct{}, len(r.testFile.Glut.Assert.Binary))
	for name := range r.testFile.Glut.Assert.Binary {
		assertedBinaries[name] = struct{}{}
	}
	logs, err := mockwrapper.ReadBinaryLogs(workspace.MockBinaryLogDir(r.work.Dir), assertedBinaries)
	r.binaryLogs = logs
	if err != nil {
		record(fmt.Errorf("read mock logs: %w", err))
	}
	return firstErr
}

// fetchGCLArtifacts extracts job-produced files from gitlab-ci-local's own
// named volumes (gcl-*-build) into the workspace. In volume strategy, bind
// mounts of host paths do not reach Docker Desktop from inside a
// devcontainer, so artifact files never land on the host filesystem. The
// pipeline ran with --cleanup=false to keep those volumes alive; extract all
// job-produced files, then remove them.
func (r *testRun) fetchGCLArtifacts() error {
	if !r.needsGCLArtifacts {
		return nil
	}
	jobNames := make([]string, 0, len(r.result.JobOutputs))
	for name := range r.result.JobOutputs {
		jobNames = append(jobNames, name)
	}
	if err := workspace.FetchArtifactsFromGCLVolumes(r.preRunGCLVolumes, jobNames, r.work.WorkspaceDir); err != nil {
		return fmt.Errorf("sync workspace artifacts from gcl volumes: %w", err)
	}
	return nil
}

// resolveOriginSource returns the git origin source assertions read from,
// plus a close function the caller must defer. Named volume: fetch the origin
// from inside the volume, because the pipeline may have pushed commits that
// changed it. Bind mount or no git.origin assertions: read directly from the
// host.
func (r *testRun) resolveOriginSource() (asserter.GitOriginSource, func()) {
	if r.dockerVolumeName == "" || !needsDockerOrigin(r.testFile) {
		return asserter.NewFSOrigin(r.work.OriginRepo), func() {}
	}
	tarData, err := workspace.FetchGitOriginTar(r.dockerVolumeName, r.work.Dir)
	if err != nil {
		r.recordErr(fmt.Errorf("fetch git origin from docker volume: %w", err))
		return asserter.NewFSOrigin(r.work.OriginRepo), func() {}
	}
	lazyOrigin := workspace.NewLazyTarOrigin(tarData)
	return lazyOrigin, func() { r.recordErr(lazyOrigin.Close()) }
}

// runAsserts evaluates the test's assertions and computes the verdict.
func (r *testRun) runAsserts(originSource asserter.GitOriginSource) {
	defer r.timePhase("asserts")()
	r.apiCalls = r.server.Recorder().Calls()
	assertResults := asserter.Run(r.testFile.Glut.Assert, asserter.AssertContext{
		WorkspacePath: r.work.WorkspaceDir,
		OriginRepo:    originSource,
		JobOutputs:    r.result.JobOutputs,
		APICalls:      r.apiCalls,
		BinaryLogs:    r.binaryLogs,
	})
	r.result.Failures = failedAssertions(assertResults)
	r.result.Passed = r.primaryErr == nil && len(r.result.Failures) == 0
}

// finalize is runSingleTest's single cleanup step: stop the mock server, hand
// the Docker volume to the suite's bulk cleanup, apply the workspace
// keep/remove policy, merge cleanup errors into the verdict, and attach debug
// data for failed runs.
func (r *testRun) finalize() {
	if r.server != nil {
		if err := r.server.Stop(); err != nil {
			r.cleanupErrors = append(r.cleanupErrors, fmt.Errorf("stop mock server: %w", err))
		}
		r.apiCalls = r.server.Recorder().Calls()
	}

	if r.dockerVolumeName != "" {
		// Volume removal is deferred to after the whole test suite completes
		// — see suiteRun.destroyPendingVolumes for why.
		r.suite.pendingVolumes = append(r.suite.pendingVolumes, r.dockerVolumeName)
	}

	if r.work != nil {
		r.result.WorkspacePath = r.work.Dir
		preserved, err := applyWorkspacePolicy(r.work, r.result.Passed, r.suite.opts, &r.suite.preservedFailed)
		if err != nil {
			r.cleanupErrors = append(r.cleanupErrors, err)
		}
		r.result.PreservedWorkspace = preserved
	}

	r.result.Passed, r.result.Error = finalizeCleanupError(r.result.Passed, r.primaryErr, r.cleanupErrors)

	if r.suite.opts.Debug && !r.result.Passed {
		debug := &DebugData{
			RawStdout:     r.execResult.RawStdout,
			RawStderr:     r.execResult.RawStderr,
			BinaryLogs:    r.binaryLogs,
			APICalls:      r.apiCalls,
			PhaseTimings:  copyPhaseTimings(r.phaseTimings),
			CleanupErrors: errorsToStrings(r.cleanupErrors),
		}
		if r.work != nil {
			debug.WorkspaceGitLog = safeGitLog(r.work.WorkspaceDir)
			debug.OriginGitLog = safeGitLog(r.work.WorkspaceDir, "--git-dir="+filepath.Join(r.work.WorkspaceDir, ".glut-origin.git"))
		}
		r.result.Debug = debug
	}
}

// sharedExecutorDeadline derives a single deadline for a test's executor
// invocations (ListJobs and Run), so they share one --timeout budget instead
// of each getting a fresh one. timeout <= 0 means unlimited, matching
// executor.withTimeout's own convention.
func sharedExecutorDeadline(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithDeadline(ctx, time.Now().Add(timeout))
}

func maybePause(point string, current string, workspacePath string) error {
	if point == "after-pipeline" {
		point = "before-asserts"
	}
	if point != current {
		return nil
	}

	fmt.Printf("Debug pause at %s\n", current)
	fmt.Printf("Workspace: %s\n", workspacePath)
	fmt.Print("Press Enter to continue...")
	_, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, os.ErrClosed) {
		return fmt.Errorf("wait for debug pause input: %w", err)
	}
	return nil
}

func maybePauseOnFail(point string, passed bool, workspacePath string) error {
	if passed || point != "on-fail" {
		return nil
	}
	return maybePause(point, point, workspacePath)
}

func apiConfig(testFile parser.TestFile) parser.APISetupConfig {
	if testFile.Glut.Setup.API == nil {
		return parser.APISetupConfig{}
	}
	return *testFile.Glut.Setup.API
}

func hasMockBinaries(testFile parser.TestFile) bool {
	return testFile.Glut.Setup.Mocks != nil && len(testFile.Glut.Setup.Mocks.Binaries) > 0
}

// componentsRealFetch reports whether the test opted into integration-mode
// component resolution (setup.components.fetch: real).
func componentsRealFetch(setup parser.SetupConfig) bool {
	return setup.Components != nil && setup.Components.Fetch == config.ComponentsFetchReal
}

func resolveGlutBinPath(path string) string {
	if path != "" {
		return path
	}
	executable, err := os.Executable()
	if err != nil {
		return "glut"
	}
	return executable
}

func needsJobList(testFile parser.TestFile) bool {
	for _, jobAssert := range testFile.Glut.Assert.Job {
		if jobAssert.Present != nil || jobAssert.When != "" {
			return true
		}
	}
	return false
}

func gitHeads(dir string) (string, string, error) {
	sha, err := gitOutput(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("read workspace git HEAD: %w", err)
	}
	shortSHA, err := gitOutput(dir, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("read workspace short git HEAD: %w", err)
	}
	return sha, shortSHA, nil
}

// gitHeadCommit returns the full commit message and committer timestamp
// (strict ISO 8601) of the workspace HEAD commit.
func gitHeadCommit(dir string) (string, string, error) {
	message, err := gitOutput(dir, "log", "-1", "--format=%B")
	if err != nil {
		return "", "", fmt.Errorf("read workspace HEAD commit message: %w", err)
	}
	timestamp, err := gitOutput(dir, "log", "-1", "--format=%cI")
	if err != nil {
		return "", "", fmt.Errorf("read workspace HEAD commit timestamp: %w", err)
	}
	return message, timestamp, nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(output)), nil
}

func safeGitLog(dir string, extraArgs ...string) string {
	if dir == "" {
		return ""
	}

	args := append([]string{}, extraArgs...)
	args = append(args, "log", "--oneline", "--decorate", "--graph", "--all")
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(output))
	}
	return strings.TrimSpace(string(output))
}

func failedAssertions(results []asserter.AssertResult) []asserter.AssertResult {
	failures := make([]asserter.AssertResult, 0)
	for _, result := range results {
		if result.Passed {
			continue
		}
		failures = append(failures, result)
	}
	return failures
}

// finalizeCleanupError merges a deferred cleanup error (mock server stop,
// workspace policy) into the result's error, promoting the first cleanup
// error into primaryErr when the test otherwise succeeded. passed is
// recomputed here rather than trusted from the caller: it was set earlier
// in runSingleTest based only on primaryErr as it stood before cleanup ran,
// so without this a cleanup failure could leave Passed=true alongside a
// non-nil Error.
func finalizeCleanupError(passed bool, primaryErr error, cleanupErrors []error) (bool, error) {
	if primaryErr == nil && len(cleanupErrors) > 0 {
		primaryErr = cleanupErrors[0]
	}
	if primaryErr != nil {
		passed = false
	}
	return passed, primaryErr
}

func applyWorkspacePolicy(
	work *workspace.Workspace,
	passed bool,
	opts RunOptions,
	preservedFailed *[]string,
) (bool, error) {
	if work == nil {
		return false, nil
	}

	if opts.KeepWorkspace {
		return true, nil
	}

	if !passed && opts.KeepLastFailed > 0 {
		*preservedFailed = append(*preservedFailed, work.Dir)
		if len(*preservedFailed) > opts.KeepLastFailed {
			oldest := (*preservedFailed)[0]
			*preservedFailed = (*preservedFailed)[1:]
			if err := os.RemoveAll(oldest); err != nil {
				return true, fmt.Errorf("remove old preserved workspace %s: %w", oldest, err)
			}
		}
		return true, nil
	}

	if err := os.RemoveAll(work.Dir); err != nil {
		return false, fmt.Errorf("remove workspace %s: %w", work.Dir, err)
	}
	return false, nil
}

func copyPhaseTimings(values map[string]time.Duration) map[string]time.Duration {
	out := make(map[string]time.Duration, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

// dockerVolumes returns the --volume mounts needed for Docker executor jobs.
//
// Named volume strategy: mounts the pre-populated Docker volume at workDir so
// the path is identical inside and outside the container. Required for Docker
// Desktop / WSL2 where host bind-mounts are invisible to the daemon.
//
// Bind mount strategy: mounts the host workspace directory at the same path.
// No volume creation is needed; the host filesystem is directly accessible
// to the daemon on native Linux Docker.
func dockerVolumes(useDocker bool, workDir string, volName string, strategy string) []string {
	if !useDocker {
		return nil
	}
	if strategy == docker.VolumeStrategyBind {
		return []string{workDir + ":" + workDir}
	}
	if volName != "" {
		return []string{volName + ":" + workDir}
	}
	return nil
}

// dockerExtraHosts returns the --extra-host entries needed for Docker executor jobs.
// ip is the address of the GLUT process (mock server) reachable from inside containers.
// Two entries are injected:
//   - host.docker.internal — standard Docker Desktop alias; we set it explicitly so it
//     works on Linux too (where Docker Desktop is absent).
//   - glut-mock — GLUT's own stable hostname used in CI_API_V4_URL / CI_SERVER_URL,
//     isolated from any unintended side-effects of the host.docker.internal alias.
func dockerExtraHosts(useDocker bool, ip string) []string {
	if !useDocker {
		return nil
	}
	return []string{
		"host.docker.internal:" + ip,
		"glut-mock:" + ip,
	}
}

// outboundIP returns the local IP address that would be used to reach an external
// host. In DinD (Docker-in-Docker via socket) this is the GLUT container's bridge
// IP, which is reachable from sibling job containers on the same bridge network.
func outboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer func() { _ = conn.Close() }()
		return conn.LocalAddr().(*net.UDPAddr).IP.String()
	}
	// Fallback: scan network interfaces for the first non-loopback IPv4 address.
	// host.docker.internal is intentionally avoided here: when GLUT runs as a
	// container that hostname resolves to the Docker Desktop gateway, not to the
	// GLUT container itself, making it unreachable from sibling job containers.
	ifaces, err := net.Interfaces()
	if err != nil {
		return "host.docker.internal"
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
				return ip.String()
			}
		}
	}
	return "host.docker.internal"
}

func applyDockerCompatibilityEnv(envVars map[string]string, useDocker bool) {
	if !useDocker {
		return
	}
	// Keep GitLab-like user behavior in Docker jobs. Do not force root via GCL.
	// This lets rootless images run with their own user settings.
	envVars["GCL_UMASK"] = "false"
}

// resolveDockerMode converts the three-state *bool Docker field into the two executor flags.
// nil (absent) → full Docker mode, same as &true.
// &true → full Docker mode with volume/extra-host support.
// &false → force all jobs to shell, even those with image:.
func resolveDockerMode(docker *bool) (useDocker bool, forceShell bool) {
	if docker == nil || *docker {
		return true, false
	}
	return false, true
}

// needsDockerOrigin reports whether the test has assert.git.origin assertions
// that require reading the git origin from inside the Docker volume. When a
// pipeline pushes commits, the in-volume origin differs from the host copy.
func needsDockerOrigin(testFile parser.TestFile) bool {
	return testFile.Glut.Assert.Git != nil && testFile.Glut.Assert.Git.Origin != nil
}

func errorsToStrings(errs []error) []string {
	if len(errs) == 0 {
		return nil
	}

	values := make([]string, 0, len(errs))
	for _, err := range errs {
		values = append(values, err.Error())
	}
	return values
}
