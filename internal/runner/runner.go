package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
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
)

// dockerTestRetryPause is a var (not const) so tests can shrink it.
var dockerTestRetryPause = 5 * time.Second

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
	// WorkspaceTempDir is the base directory each test's ephemeral workspace
	// is created under (empty uses the system default, e.g. /tmp). Set this
	// when GLUT itself runs inside a container that only bind-mounts part of
	// its filesystem (e.g. GLUT_WORK_DIR in the Makefile's containerized
	// test-integration target), so temp workspaces land somewhere the host
	// can actually see rather than an ephemeral path invisible outside the
	// container.
	WorkspaceTempDir string
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

// suiteRun carries the state one Run call shares across all its tests: the
// resolved volume strategy, the Docker volumes waiting for bulk removal, and
// the failed workspaces preserved by the keep-last-failed policy.
type suiteRun struct {
	repoRoot        string
	opts            RunOptions
	volumeStrategy  string
	pendingVolumes  []string
	preservedFailed []string
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

	suite := &suiteRun{
		repoRoot: repoRoot,
		opts:     opts,
		// Resolve the Docker volume strategy once for the whole suite run.
		// "auto" detects whether the workspace path is on a native Linux
		// filesystem (bind mounts) or a Windows-backed 9P path (named volumes).
		volumeStrategy:  docker.ResolveVolumeStrategy(opts.DockerVolumeStrategy, opts.HostEnv),
		preservedFailed: make([]string, 0, opts.KeepLastFailed),
	}
	defer suite.destroyPendingVolumes()

	runStart := time.Now()
	result := RunResult{Tests: make([]TestResult, 0, len(tests))}
	dockerReady := !anyTestNeedsDocker(tests)

	for _, testFile := range tests {
		if ctx.Err() != nil {
			break
		}

		if testNeedsDocker(&testFile) && !dockerReady {
			if err := ensureDockerReady(ctx, opts, runStart); err != nil {
				result.Error = err
				break
			}
			dockerReady = true
		}

		testResult := suite.runTestWithRetry(ctx, testFile)

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

	if result.Error != nil {
		return result, ExitRunnerError
	}
	if result.Failed > 0 {
		return result, ExitTestFailed
	}
	return result, ExitOK
}

// destroyPendingVolumes removes the Docker volumes collected across the whole
// suite. Destroying volumes between sequential tests triggers overlay-FS
// cleanup in the daemon, which keeps it busy and causes transient failures in
// the next test's container start; bulk removal at the end avoids this
// inter-test contention entirely.
func (s *suiteRun) destroyPendingVolumes() {
	for _, vol := range s.pendingVolumes {
		_ = workspace.DestroyDockerVolume(vol)
	}
}

// ensureDockerReady blocks until the Docker daemon responds (bounded by the
// remaining wait budget) and prunes orphaned volumes from previous GLUT runs
// before the suite's first Docker test. Unreferenced named volumes accumulate
// across suite runs and keep the daemon busy with background cleanup work
// that can delay new container starts.
func ensureDockerReady(ctx context.Context, opts RunOptions, runStart time.Time) error {
	remaining := opts.WaitTimeout - time.Since(runStart)
	if remaining < 0 {
		remaining = 0
	}
	if err := docker.Wait(ctx, opts.DockerWaitOutput, remaining, opts.HostEnv); err != nil {
		return fmt.Errorf("wait for Docker: %w", err)
	}
	docker.PruneOrphanedVolumes(opts.HostEnv)
	return nil
}

// runTestWithRetry runs one test and retries it once when a Docker test
// failed at the infrastructure level (volume creation, daemon communication)
// rather than on its own merits. The retry result replaces the original only
// when it produced a real verdict (pass or assertion failures).
func (s *suiteRun) runTestWithRetry(ctx context.Context, testFile parser.TestFile) TestResult {
	testResult := s.runSingleTest(ctx, testFile)
	if !shouldRetryInfraFailure(ctx, &testFile, testResult) {
		return testResult
	}

	for _, sink := range s.opts.Progress {
		sink.TestRetry(testResult.TestName, testResult.Error)
	}
	select {
	case <-time.After(dockerTestRetryPause):
	case <-ctx.Done():
	}
	if ctx.Err() != nil {
		return testResult
	}

	retryResult := s.runSingleTest(ctx, testFile)
	if retryResult.Passed || len(retryResult.Failures) > 0 {
		return retryResult
	}
	return testResult
}

// shouldRetryInfraFailure reports whether a failed Docker test deserves one
// retry. InfraError covers failures explicitly tagged as infrastructure
// (e.g. volume create, populate). The fallback covers executor-level daemon
// failures that occur before any job starts and are therefore not wrapped as
// InfraError — those also produce an error and no executed job output.
// JobOutputs alone cannot signal this: when the test has present:/when:
// assertions, executor.ListJobs pre-populates JobOutputs before the pipeline
// ever runs, so its length is non-zero even on a daemon-level failure. Only
// entries actually produced by the pipeline run are marked Executed.
func shouldRetryInfraFailure(ctx context.Context, testFile *parser.TestFile, result TestResult) bool {
	if !testNeedsDocker(testFile) || result.Passed || ctx.Err() != nil {
		return false
	}

	var infraErr *workspace.InfraError
	if errors.As(result.Error, &infraErr) {
		return true
	}

	executorRan := false
	for _, output := range result.JobOutputs {
		if output.Executed {
			executorRan = true
			break
		}
	}
	return result.Error != nil && !executorRan
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
					if parser.SkipDiscoveryDir(entry.Name()) {
						return filepath.SkipDir
					}
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

func shouldStop(result RunResult, opts RunOptions) bool {
	if opts.FailFast && result.Failed > 0 {
		return true
	}
	return opts.MaxFail > 0 && result.Failed >= opts.MaxFail
}
