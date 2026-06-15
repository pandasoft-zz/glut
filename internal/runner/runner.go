package runner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pandasoft-zz/glut/internal/asserter"
	"github.com/pandasoft-zz/glut/internal/docker"
	"github.com/pandasoft-zz/glut/internal/executor"
	"github.com/pandasoft-zz/glut/internal/mockserver"
	"github.com/pandasoft-zz/glut/internal/mockwrapper"
	"github.com/pandasoft-zz/glut/internal/parser"
	"github.com/pandasoft-zz/glut/internal/workspace"
	glutschema "github.com/pandasoft-zz/glut/schema"
)

type ExitCode int

const (
	ExitOK          ExitCode = 0
	ExitTestFailed  ExitCode = 1
	ExitRunnerError ExitCode = 2
)

const (
	defaultKeepLastFailed = 3
	DefaultWaitTimeout    = 120 * time.Second
	dockerTestRetryPause  = 5 * time.Second
)

type RunOptions struct {
	RunPattern           string
	FailFast             bool
	MaxFail              int
	Verbose              bool
	Quiet                bool
	Timeout              time.Duration
	Debug                bool
	KeepWorkspace        bool
	DebugPause           string
	KeepLastFailed       int
	GlutBinPath          string
	CopyStrategy         string
	Include              []string
	Progress             []ProgressSink
	HostEnv              []string      // nil falls back to os.Environ(); propagated to executor and workspace
	WorkDir              string        // working directory for test discovery; empty falls back to os.Getwd()
	WaitTimeout          time.Duration // max time to wait for Docker daemon; 0 uses default (120s)
	DockerWaitOutput     io.Writer     // where to write Docker wait progress; nil discards output
	DockerVolumeStrategy string        // "auto" (default), "bind" (native Linux), "volume" (Docker Desktop/WSL2)
}

type ListOptions struct {
	RunPattern string
	WorkDir    string // working directory for path resolution; empty falls back to current dir
}

type ProgressSink interface {
	Start(totalTests int)
	TestRetry(testName string, err error)
	TestDone(result TestResult)
	Summary(result RunResult)
}

type RunResult struct {
	Tests    []TestResult
	Passed   int
	Failed   int
	Duration time.Duration
	Error    error
}

type TestResult struct {
	FilePath           string
	TestName           string
	Passed             bool
	Duration           time.Duration
	Failures           []asserter.AssertResult
	Error              error
	JobOutputs         map[string]executor.JobOutput
	WorkspacePath      string
	PreservedWorkspace bool
	Debug              *DebugData
}

type DebugData struct {
	RawStdout       string
	RawStderr       string
	BinaryLogs      map[string][]mockwrapper.BinaryCall
	APICalls        []mockserver.APICall
	WorkspaceGitLog string
	OriginGitLog    string
	PhaseTimings    map[string]time.Duration
	CleanupErrors   []string
}

type ListedTest struct {
	FilePath string
	TestName string
}

// relPath returns path made relative to base. It returns path unchanged when
// base is empty or filepath.Rel returns an error.
func relPath(base, path string) string {
	if base == "" {
		return path
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return path
	}
	return rel
}

func Run(ctx context.Context, paths []string, opts RunOptions) (RunResult, ExitCode) {
	opts = normalizeRunOptions(opts)
	if err := validateDebugPause(opts.DebugPause); err != nil {
		return RunResult{Error: err}, ExitRunnerError
	}

	repoRoot := opts.WorkDir
	if repoRoot == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return RunResult{Error: fmt.Errorf("read current directory: %w", err)}, ExitRunnerError
		}
	}

	tests, err := discoverTests(absifyPaths(paths, repoRoot), opts.RunPattern)
	if err != nil {
		return RunResult{Error: err}, ExitRunnerError
	}

	// Non-docker tests run first so the Docker daemon can start while they execute.
	sort.SliceStable(tests, func(i, j int) bool {
		return !testNeedsDocker(&tests[i]) && testNeedsDocker(&tests[j])
	})

	for _, sink := range opts.Progress {
		sink.Start(len(tests))
	}

	runStart := time.Now()
	result := RunResult{Tests: make([]TestResult, 0, len(tests))}
	preservedFailed := make([]string, 0, opts.KeepLastFailed)
	dockerReady := !anyTestNeedsDocker(tests)

	// Resolve the Docker volume strategy once for the whole suite run.
	// "auto" detects whether the workspace path is on a native Linux
	// filesystem (bind mounts) or a Windows-backed 9P path (named volumes).
	effectiveVolumeStrategy := docker.ResolveVolumeStrategy(opts.DockerVolumeStrategy)

	// Docker volumes are collected here and destroyed together at the end.
	// Destroying them immediately after each test triggers overlay-FS cleanup
	// in the daemon between tests, which is the main source of transient
	// failures on Docker Desktop / WSL2. Bulk removal after all tests avoids
	// this inter-test contention.
	var pendingVolumeCleanup []string
	defer func() {
		for _, vol := range pendingVolumeCleanup {
			_ = workspace.DestroyDockerVolume(vol)
		}
	}()

	for _, testFile := range tests {
		if testNeedsDocker(&testFile) && !dockerReady {
			remaining := opts.WaitTimeout - time.Since(runStart)
			if remaining < 0 {
				remaining = 0
			}
			if err := docker.Wait(ctx, opts.DockerWaitOutput, remaining); err != nil {
				return RunResult{Error: fmt.Errorf("wait for Docker: %w", err)}, ExitRunnerError
			}
			// Prune orphaned volumes from previous GLUT runs before the first
			// Docker test. Named volumes that are no longer referenced by any
			// container accumulate across suite runs and keep the daemon busy
			// with background cleanup work that can delay new container starts.
			docker.PruneOrphanedVolumes()
			dockerReady = true
		}

		testResult := runSingleTest(ctx, repoRoot, testFile, opts, effectiveVolumeStrategy, &pendingVolumeCleanup, &preservedFailed)

		// For Docker tests that fail at the infrastructure level (volume
		// creation, daemon communication), retry once. InfraError covers
		// failures explicitly tagged as infrastructure (e.g. volume create,
		// populate). The fallback covers executor-level daemon failures that
		// occur before any job starts and are therefore not wrapped as
		// InfraError — those also produce error + no job outputs.
		var infraErr *workspace.InfraError
		executorFailedBeforeJobs := testResult.Error != nil && len(testResult.JobOutputs) == 0
		if testNeedsDocker(&testFile) && !testResult.Passed &&
			(errors.As(testResult.Error, &infraErr) || executorFailedBeforeJobs) {
			for _, sink := range opts.Progress {
				sink.TestRetry(testResult.TestName, testResult.Error)
			}
			select {
			case <-time.After(dockerTestRetryPause):
			case <-ctx.Done():
			}
			retryResult := runSingleTest(ctx, repoRoot, testFile, opts, effectiveVolumeStrategy, &pendingVolumeCleanup, &preservedFailed)
			if retryResult.Passed || len(retryResult.Failures) > 0 {
				testResult = retryResult
			}
		}

		result.Tests = append(result.Tests, testResult)
		if testResult.Passed {
			result.Passed++
		} else {
			result.Failed++
		}

		for _, sink := range opts.Progress {
			sink.TestDone(testResult)
		}

		if shouldStop(result, opts) {
			break
		}
	}

	result.Duration = time.Since(runStart)
	for _, sink := range opts.Progress {
		sink.Summary(result)
	}

	if result.Failed > 0 {
		return result, ExitTestFailed
	}
	return result, ExitOK
}

func List(ctx context.Context, paths []string, opts ListOptions) ([]ListedTest, error) {
	_ = ctx

	workDir := opts.WorkDir
	if workDir == "" {
		cwd, err := os.Getwd()
		if err == nil {
			workDir = cwd
		}
	}

	tests, err := discoverTests(absifyPaths(paths, workDir), opts.RunPattern)
	if err != nil {
		return nil, err
	}

	listed := make([]ListedTest, 0, len(tests))
	for _, testFile := range tests {
		if testFile.ParseError != nil {
			continue
		}
		listed = append(listed, ListedTest{
			FilePath: relPath(workDir, testFile.FilePath),
			TestName: testFile.Glut.Name,
		})
	}
	return listed, nil
}

func discoverTests(paths []string, pattern string) ([]parser.TestFile, error) {
	matcher := compilePattern(pattern)

	inputs := normalizePaths(paths)
	collected := make([]parser.TestFile, 0)
	for _, input := range inputs {
		info, err := os.Stat(input)
		if err != nil {
			return nil, fmt.Errorf("stat path %s: %w", input, err)
		}

		if info.IsDir() {
			err = filepath.WalkDir(input, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() {
					return nil
				}
				if !isYAMLPath(path) {
					return nil
				}

				testFile, err := loadTestFile(path)
				if err != nil {
					if parser.IsMissingGlut(err) {
						return nil
					}
					collected = append(collected, parser.TestFile{FilePath: path, ParseError: err})
					return nil
				}
				if matcher(testFile) {
					collected = append(collected, *testFile)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			continue
		}

		testFile, err := loadTestFile(input)
		if err != nil {
			if parser.IsMissingGlut(err) {
				return nil, err
			}
			collected = append(collected, parser.TestFile{FilePath: input, ParseError: err})
			continue
		}
		if matcher(testFile) {
			collected = append(collected, *testFile)
		}
	}

	sort.Slice(collected, func(i, j int) bool {
		if collected[i].FilePath == collected[j].FilePath {
			return collected[i].Glut.Name < collected[j].Glut.Name
		}
		return collected[i].FilePath < collected[j].FilePath
	})
	return collected, nil
}

func loadTestFile(path string) (*parser.TestFile, error) {
	testFile, err := parser.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	validationErrors, err := glutschema.ValidateGlut(testFile.GlutRaw)
	if err != nil {
		return nil, fmt.Errorf("validate schema for %s: %w", path, err)
	}
	if len(validationErrors) > 0 {
		messages := make([]string, 0, len(validationErrors))
		for _, validationErr := range validationErrors {
			messages = append(messages, glutschema.FormatValidationError(validationErr))
		}
		return nil, fmt.Errorf("validate schema for %s: %s", path, strings.Join(messages, "; "))
	}

	lints := parser.SemanticLint(testFile.FilePath, testFile.GlutRaw)
	errorsOnly := make([]string, 0, len(lints))
	for _, lint := range lints {
		if lint.Level != parser.LevelError {
			continue
		}
		errorsOnly = append(errorsOnly, lint.Message)
	}
	if len(errorsOnly) > 0 {
		return nil, fmt.Errorf("lint %s: %s", path, strings.Join(errorsOnly, "; "))
	}

	return testFile, nil
}

func runSingleTest(
	ctx context.Context,
	repoRoot string,
	testFile parser.TestFile,
	opts RunOptions,
	volumeStrategy string,
	pendingVolumeCleanup *[]string,
	preservedFailed *[]string,
) (result TestResult) {
	if testFile.ParseError != nil {
		fp := relPath(repoRoot, testFile.FilePath)
		return TestResult{
			FilePath: fp,
			TestName: fp,
			Passed:   false,
			Error:    testFile.ParseError,
		}
	}

	result = TestResult{
		FilePath:   relPath(repoRoot, testFile.FilePath),
		TestName:   testFile.Glut.Name,
		JobOutputs: map[string]executor.JobOutput{},
	}

	var (
		work             *workspace.Workspace
		server           *mockserver.Server
		execResult       executor.RunResult
		phaseTimings     = map[string]time.Duration{}
		binaryLogs       = map[string][]mockwrapper.BinaryCall{}
		apiCalls         []mockserver.APICall
		cleanupErrors    []error
		primaryErr       error
		dockerVolumeName string
	)

	defer func() {
		if server != nil {
			if err := server.Stop(); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("stop mock server: %w", err))
			}
			apiCalls = server.Recorder().Calls()
		}

		if dockerVolumeName != "" {
			// Defer volume removal to after the whole test suite completes.
			// Destroying volumes between sequential tests triggers overlay-FS
			// cleanup in the Docker daemon, which keeps it busy and causes
			// transient failures in the next test's container start. Bulk
			// removal at the end avoids this inter-test contention entirely.
			*pendingVolumeCleanup = append(*pendingVolumeCleanup, dockerVolumeName)
		}

		if work != nil {
			result.WorkspacePath = work.Dir
			preserved, err := applyWorkspacePolicy(work, result.Passed, opts, preservedFailed)
			if err != nil {
				cleanupErrors = append(cleanupErrors, err)
			}
			result.PreservedWorkspace = preserved
		}

		if primaryErr == nil && len(cleanupErrors) > 0 {
			primaryErr = cleanupErrors[0]
		}
		result.Error = primaryErr

		if opts.Debug && !result.Passed {
			result.Debug = &DebugData{
				RawStdout:       execResult.RawStdout,
				RawStderr:       execResult.RawStderr,
				BinaryLogs:      binaryLogs,
				APICalls:        apiCalls,
				WorkspaceGitLog: safeGitLog(work.WorkspaceDir),
				OriginGitLog:    safeGitLog(work.WorkspaceDir, "--git-dir="+filepath.Join(work.WorkspaceDir, ".glut-origin.git")),
				PhaseTimings:    copyPhaseTimings(phaseTimings),
				CleanupErrors:   errorsToStrings(cleanupErrors),
			}
		}
	}()

	testStart := time.Now()
	defer func() {
		result.Duration = time.Since(testStart)
	}()

	phaseStart := time.Now()
	work, primaryErr = workspace.New(testFile.Glut.Setup, false, repoRoot, workspace.Options{
		CopyStrategy: opts.CopyStrategy,
		Include:      opts.Include,
		Verbose:      opts.Verbose,
		HostEnv:      opts.HostEnv,
	})
	phaseTimings["workspace"] = time.Since(phaseStart)
	if primaryErr != nil {
		primaryErr = fmt.Errorf("create workspace: %w", primaryErr)
		result.Passed = false
		return
	}

	phaseStart = time.Now()
	server, primaryErr = mockserver.New(apiConfig(testFile))
	phaseTimings["mockserver-new"] = time.Since(phaseStart)
	if primaryErr != nil {
		primaryErr = fmt.Errorf("create mock server: %w", primaryErr)
		result.Passed = false
		return
	}

	phaseStart = time.Now()
	primaryErr = server.Start()
	phaseTimings["mockserver-start"] = time.Since(phaseStart)
	if primaryErr != nil {
		primaryErr = fmt.Errorf("start mock server: %w", primaryErr)
		result.Passed = false
		return
	}
	server.SetGitRepo(work.OriginRepo)

	phaseStart = time.Now()
	useDocker, forceShell := resolveDockerMode(testFile.Glut.Setup.Docker)
	usePrivileged := testFile.Glut.Setup.Privileged != nil && *testFile.Glut.Setup.Privileged
	if useDocker && volumeStrategy == docker.VolumeStrategyVolume {
		// Named volume: populate from host and collect for deferred cleanup.
		var mocks *parser.MocksConfig
		if hasMockBinaries(testFile) {
			mocks = testFile.Glut.Setup.Mocks
		}
		dockerVolumeName, primaryErr = workspace.CreateDockerVolume(work.Dir, work.OriginRepo, mocks)
	}
	// Bind mount strategy: the host workspace directory is already populated
	// by workspace.New() and SetupMockBinaries. No extra Docker operation needed.
	// Always inject mock binaries into shell PATH so jobs without image: can find
	// them regardless of docker mode. For docker:true this runs alongside the volume;
	// for docker:false this is the only setup path.
	if primaryErr == nil && hasMockBinaries(testFile) {
		primaryErr = workspace.SetupMockBinaries(work.Dir, *testFile.Glut.Setup.Mocks, resolveGlutBinPath(opts.GlutBinPath), false)
	}
	phaseTimings["mock-binaries"] = time.Since(phaseStart)
	if primaryErr != nil {
		primaryErr = fmt.Errorf("setup mock binaries: %w", primaryErr)
		result.Passed = false
		return
	}

	phaseStart = time.Now()
	sha, shortSHA, err := gitHeads(work.WorkspaceDir)
	phaseTimings["git-head"] = time.Since(phaseStart)
	if err != nil {
		primaryErr = err
		result.Passed = false
		return
	}
	commitMessage, commitTimestamp, err := gitHeadCommit(work.WorkspaceDir)
	if err != nil {
		primaryErr = err
		result.Passed = false
		return
	}

	var mockHostIP string
	if useDocker {
		mockHostIP = outboundIP()
	}

	envVars := work.EnvVars(testFile.Glut.Setup, server.Port(), sha, shortSHA, testFile.Glut.Name)
	workspace.ApplyCommitEnv(envVars, commitMessage, commitTimestamp)
	applyDockerCompatibilityEnv(envVars, useDocker)
	if useDocker {
		// BUG-3: Docker containers cannot reach 127.0.0.1. Use the bridge IP directly
		// so the URL works for both Docker jobs (container on same bridge) and shell jobs
		// (same host or container). glut-mock remains an alias via --extra-host.
		workspace.ApplyServerBaseURL(envVars, mockHostIP, server.Port())
	}
	execCfg := executor.ExecutorConfig{
		WorkspacePath:    work.WorkspaceDir,
		PipelineYAML:     testFile.PipelineYAML,
		EnvVars:          envVars,
		UnsetVars:        work.UnsetVars(testFile.Glut.Setup),
		MockBinPath:      workspace.MockBinaryBinDir(work.Dir),
		Timeout:          opts.Timeout,
		Debug:            opts.Debug,
		Verbose:          opts.Verbose,
		UseDocker:        useDocker,
		ForceShell:       forceShell,
		Privileged:       usePrivileged,
		DockerVolumes:    dockerVolumes(useDocker, work.Dir, dockerVolumeName, volumeStrategy),
		DockerExtraHosts: dockerExtraHosts(useDocker, mockHostIP),
		HostEnv:          opts.HostEnv,
	}

	if err := maybePause(opts.DebugPause, "before-pipeline", work.Dir); err != nil {
		primaryErr = err
		result.Passed = false
		return
	}

	if needsJobList(testFile) {
		phaseStart = time.Now()
		jobEntries, err := executor.ListJobs(ctx, execCfg)
		phaseTimings["list-jobs"] = time.Since(phaseStart)
		if err != nil {
			primaryErr = fmt.Errorf("list jobs: %w", err)
			result.Passed = false
			return
		}
		for _, entry := range jobEntries {
			result.JobOutputs[entry.Name] = executor.JobOutput{Name: entry.Name, Present: true, When: entry.When}
		}
	}

	phaseStart = time.Now()
	execResult, err = executor.Run(ctx, execCfg)
	phaseTimings["pipeline"] = time.Since(phaseStart)
	for name, output := range execResult.Jobs {
		output.Present = true
		output.Executed = true
		// Keep the evaluated `when` from the list phase — the run output does
		// not carry it.
		if existing, ok := result.JobOutputs[name]; ok {
			output.When = existing.When
		}
		result.JobOutputs[name] = output
	}
	if err != nil {
		primaryErr = fmt.Errorf("run pipeline: %w", err)
	}

	phaseStart = time.Now()
	if hasMockBinaries(testFile) {
		if dockerVolumeName != "" {
			// Flush filesystem writes inside the volume before copying logs so
			// that all wrapper writes are visible to the tar command.
			if syncErr := workspace.SyncDockerVolume(dockerVolumeName, work.Dir); syncErr != nil && primaryErr == nil {
				primaryErr = fmt.Errorf("sync docker volume: %w", syncErr)
			}
			// Copy mock logs from the volume back to the host.
			if syncErr := workspace.ReadLogsFromDockerVolume(dockerVolumeName, work.Dir); syncErr != nil && primaryErr == nil {
				primaryErr = fmt.Errorf("sync mock logs from docker volume: %w", syncErr)
			}
		}
		// Detect wrapper processes killed before completing their log write.
		if barrierErr := mockwrapper.CheckMockLogBarriers(workspace.MockBinaryLogDir(work.Dir)); barrierErr != nil && primaryErr == nil {
			primaryErr = fmt.Errorf("mock log write interrupted: %w", barrierErr)
		}
		// Read logs only for binaries that have assertions — a partially-written
		// or missing log for an un-asserted binary must not fail the test.
		assertedBinaries := make(map[string]struct{}, len(testFile.Glut.Assert.Binary))
		for name := range testFile.Glut.Assert.Binary {
			assertedBinaries[name] = struct{}{}
		}
		binaryLogs, err = mockwrapper.ReadBinaryLogs(workspace.MockBinaryLogDir(work.Dir), assertedBinaries)
	}
	phaseTimings["mock-logs"] = time.Since(phaseStart)
	if err != nil && primaryErr == nil {
		primaryErr = fmt.Errorf("read mock logs: %w", err)
	}

	// Build an origin source for assertions. Named volume: fetch from inside
	// the volume because the pipeline may have pushed commits that changed it.
	// Bind mount or no git.origin assertions: read directly from the host.
	originSource := asserter.NewFSOrigin(work.OriginRepo)
	if dockerVolumeName != "" && needsDockerOrigin(testFile) {
		tarData, fetchErr := workspace.FetchGitOriginTar(dockerVolumeName, work.Dir)
		if fetchErr != nil && primaryErr == nil {
			primaryErr = fmt.Errorf("fetch git origin from docker volume: %w", fetchErr)
		} else if fetchErr == nil {
			lazyOrigin := workspace.NewLazyTarOrigin(tarData)
			defer func() {
				if closeErr := lazyOrigin.Close(); closeErr != nil && primaryErr == nil {
					primaryErr = closeErr
				}
			}()
			originSource = lazyOrigin
		}
	}

	if err := maybePause(opts.DebugPause, "before-asserts", work.Dir); err != nil && primaryErr == nil {
		primaryErr = err
	}

	phaseStart = time.Now()
	apiCalls = server.Recorder().Calls()
	assertResults := asserter.Run(testFile.Glut.Assert, asserter.AssertContext{
		WorkspacePath: work.WorkspaceDir,
		OriginRepo:    originSource,
		JobOutputs:    result.JobOutputs,
		APICalls:      apiCalls,
		BinaryLogs:    binaryLogs,
	})
	phaseTimings["asserts"] = time.Since(phaseStart)
	result.Failures = failedAssertions(assertResults)
	result.Passed = primaryErr == nil && len(result.Failures) == 0

	if err := maybePauseOnFail(opts.DebugPause, result.Passed, work.Dir); err != nil && primaryErr == nil {
		primaryErr = err
		result.Passed = false
	}

	return
}

func normalizeRunOptions(opts RunOptions) RunOptions {
	if opts.KeepLastFailed <= 0 {
		opts.KeepLastFailed = defaultKeepLastFailed
	}
	if opts.WaitTimeout <= 0 {
		opts.WaitTimeout = DefaultWaitTimeout
	}
	if opts.DockerWaitOutput == nil {
		opts.DockerWaitOutput = io.Discard
	}
	return opts
}

// testNeedsDocker reports whether a test requires a Docker daemon.
// docker: nil (absent) and docker: true both require Docker; docker: false does not.
func testNeedsDocker(t *parser.TestFile) bool {
	if t.ParseError != nil {
		return false
	}
	return t.Glut.Setup.Docker == nil || *t.Glut.Setup.Docker
}

func anyTestNeedsDocker(tests []parser.TestFile) bool {
	for i := range tests {
		if testNeedsDocker(&tests[i]) {
			return true
		}
	}
	return false
}

func normalizePaths(paths []string) []string {
	if len(paths) == 0 {
		return []string{"."}
	}
	return paths
}

func absifyPaths(paths []string, workDir string) []string {
	if workDir == "" {
		return normalizePaths(paths)
	}
	normalized := normalizePaths(paths)
	result := make([]string, len(normalized))
	for i, p := range normalized {
		if filepath.IsAbs(p) {
			result[i] = p
		} else {
			result[i] = filepath.Join(workDir, p)
		}
	}
	return result
}

func isYAMLPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yml" || ext == ".yaml"
}

func compilePattern(pattern string) func(*parser.TestFile) bool {
	if pattern == "" {
		return func(*parser.TestFile) bool { return true }
	}

	if re, err := regexp.Compile(pattern); err == nil {
		return func(testFile *parser.TestFile) bool {
			return re.MatchString(testFile.Glut.Name) || re.MatchString(testFile.FilePath)
		}
	}

	// Invalid regex: treat pattern as a substring match.
	return func(testFile *parser.TestFile) bool {
		return strings.Contains(testFile.Glut.Name, pattern) || strings.Contains(testFile.FilePath, pattern)
	}
}

func validateDebugPause(point string) error {
	switch point {
	case "", "before-pipeline", "before-asserts", "after-pipeline", "on-fail":
		return nil
	default:
		return fmt.Errorf("invalid debug pause point %q", point)
	}
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
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
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

func shouldStop(result RunResult, opts RunOptions) bool {
	if opts.FailFast && result.Failed > 0 {
		return true
	}
	return opts.MaxFail > 0 && result.Failed >= opts.MaxFail
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

// resolveDockerMode converts the three-state *bool Docker field into the two executor flags.
// nil (absent) → full Docker mode, same as &true.
// &true → full Docker mode with volume/extra-host support.
// &false → force all jobs to shell, even those with image:.
func applyDockerCompatibilityEnv(envVars map[string]string, useDocker bool) {
	if !useDocker {
		return
	}
	// Keep GitLab-like user behavior in Docker jobs. Do not force root via GCL.
	// This lets rootless images run with their own user settings.
	envVars["GCL_UMASK"] = "false"
}

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
