package runner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pandasoft-zz/glut/internal/asserter"
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

const defaultKeepLastFailed = 3

type RunOptions struct {
	RunPattern     string
	FailFast       bool
	MaxFail        int
	Verbose        bool
	Quiet          bool
	Timeout        time.Duration
	Debug          bool
	KeepWorkspace  bool
	DebugPause     string
	KeepLastFailed int
	GlutBinPath    string
	CopyStrategy   string
	Include        []string
	Progress       []ProgressSink
}

type ListOptions struct {
	RunPattern string
}

type ProgressSink interface {
	Start(totalTests int)
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

func Run(ctx context.Context, paths []string, opts RunOptions) (RunResult, ExitCode) {
	opts = normalizeRunOptions(opts)
	if err := validateDebugPause(opts.DebugPause); err != nil {
		return RunResult{Error: err}, ExitRunnerError
	}

	tests, err := discoverTests(paths, opts.RunPattern)
	if err != nil {
		return RunResult{Error: err}, ExitRunnerError
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		return RunResult{Error: fmt.Errorf("read current directory: %w", err)}, ExitRunnerError
	}

	for _, sink := range opts.Progress {
		sink.Start(len(tests))
	}

	runStart := time.Now()
	result := RunResult{Tests: make([]TestResult, 0, len(tests))}
	preservedFailed := make([]string, 0, opts.KeepLastFailed)

	for _, testFile := range tests {
		testResult := runSingleTest(ctx, repoRoot, testFile, opts, &preservedFailed)
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

	tests, err := discoverTests(paths, opts.RunPattern)
	if err != nil {
		return nil, err
	}

	listed := make([]ListedTest, 0, len(tests))
	for _, testFile := range tests {
		if testFile.ParseError != nil {
			continue
		}
		listed = append(listed, ListedTest{
			FilePath: testFile.FilePath,
			TestName: testFile.Glut.Name,
		})
	}
	return listed, nil
}

func discoverTests(paths []string, pattern string) ([]parser.TestFile, error) {
	matcher, err := compilePattern(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile run pattern: %w", err)
	}

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
	preservedFailed *[]string,
) (result TestResult) {
	if testFile.ParseError != nil {
		return TestResult{
			FilePath: testFile.FilePath,
			TestName: testFile.FilePath,
			Passed:   false,
			Error:    testFile.ParseError,
		}
	}

	result = TestResult{
		FilePath:   testFile.FilePath,
		TestName:   testFile.Glut.Name,
		JobOutputs: map[string]executor.JobOutput{},
	}

	var (
		work          *workspace.Workspace
		server        *mockserver.Server
		execResult    executor.RunResult
		phaseTimings  = map[string]time.Duration{}
		binaryLogs    = map[string][]mockwrapper.BinaryCall{}
		apiCalls      []mockserver.APICall
		cleanupErrors []error
		primaryErr    error
	)

	defer func() {
		if server != nil {
			if err := server.Stop(); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("stop mock server: %w", err))
			}
			apiCalls = server.Recorder().Calls()
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
				WorkspaceGitLog: safeGitLog(result.WorkspacePath),
				OriginGitLog:    safeGitLog(result.WorkspacePath, "--git-dir="+filepath.Join(result.WorkspacePath, ".glut-origin.git")),
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

	phaseStart = time.Now()
	useDocker, forceShell := resolveDockerMode(testFile.Glut.Setup.Docker)
	if hasMockBinaries(testFile) {
		primaryErr = workspace.SetupMockBinaries(work.Dir, *testFile.Glut.Setup.Mocks, resolveGlutBinPath(opts.GlutBinPath), useDocker)
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

	envVars := work.EnvVars(testFile.Glut.Setup, server.Port(), sha, shortSHA, testFile.Glut.Name)
	if useDocker {
		// BUG-3: Docker containers cannot reach 127.0.0.1; rewrite API URLs to use
		// host.docker.internal so the mock server is reachable from inside containers.
		port := server.Port()
		envVars["CI_SERVER_URL"] = fmt.Sprintf("http://host.docker.internal:%d", port)
		envVars["CI_API_V4_URL"] = fmt.Sprintf("http://host.docker.internal:%d/api/v4", port)
	}
	execCfg := executor.ExecutorConfig{
		WorkspacePath:    work.WorkspaceDir,
		PipelineYAML:     testFile.PipelineYAML,
		EnvVars:          envVars,
		MockBinPath:      workspace.MockBinaryBinDir(work.Dir),
		Timeout:          opts.Timeout,
		Debug:            opts.Debug,
		Verbose:          opts.Verbose,
		UseDocker:        useDocker,
		ForceShell:       forceShell,
		DockerVolumes:    dockerVolumes(useDocker, work.Dir),
		DockerExtraHosts: dockerExtraHosts(useDocker),
	}

	if err := maybePause(opts.DebugPause, "before-pipeline", work.Dir); err != nil {
		primaryErr = err
		result.Passed = false
		return
	}

	if needsJobList(testFile) {
		phaseStart = time.Now()
		jobNames, err := executor.ListJobs(ctx, execCfg)
		phaseTimings["list-jobs"] = time.Since(phaseStart)
		if err != nil {
			primaryErr = fmt.Errorf("list jobs: %w", err)
			result.Passed = false
			return
		}
		for _, name := range jobNames {
			result.JobOutputs[name] = executor.JobOutput{Name: name, Present: true}
		}
	}

	phaseStart = time.Now()
	execResult, err = executor.Run(ctx, execCfg)
	phaseTimings["pipeline"] = time.Since(phaseStart)
	for name, output := range execResult.Jobs {
		output.Present = true
		result.JobOutputs[name] = output
	}
	if err != nil {
		primaryErr = fmt.Errorf("run pipeline: %w", err)
	}

	phaseStart = time.Now()
	if hasMockBinaries(testFile) {
		binaryLogs, err = mockwrapper.ReadBinaryLogs(workspace.MockBinaryLogDir(work.Dir))
	}
	phaseTimings["mock-logs"] = time.Since(phaseStart)
	if err != nil && primaryErr == nil {
		primaryErr = fmt.Errorf("read mock logs: %w", err)
	}

	if err := maybePause(opts.DebugPause, "before-asserts", work.Dir); err != nil && primaryErr == nil {
		primaryErr = err
	}

	phaseStart = time.Now()
	apiCalls = server.Recorder().Calls()
	assertResults := asserter.Run(testFile.Glut.Assert, asserter.AssertContext{
		WorkspacePath:  work.WorkspaceDir,
		OriginRepoPath: work.OriginRepo,
		JobOutputs:     result.JobOutputs,
		APICalls:       apiCalls,
		BinaryLogs:     binaryLogs,
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
	return opts
}

func normalizePaths(paths []string) []string {
	if len(paths) == 0 {
		return []string{"."}
	}
	return paths
}

func isYAMLPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yml" || ext == ".yaml"
}

func compilePattern(pattern string) (func(*parser.TestFile) bool, error) {
	if pattern == "" {
		return func(*parser.TestFile) bool { return true }, nil
	}

	if re, err := regexp.Compile(pattern); err == nil {
		return func(testFile *parser.TestFile) bool {
			return re.MatchString(testFile.Glut.Name) || re.MatchString(testFile.FilePath)
		}, nil
	}

	return func(testFile *parser.TestFile) bool {
		return strings.Contains(testFile.Glut.Name, pattern) || strings.Contains(testFile.FilePath, pattern)
	}, nil
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
		if jobAssert.Present != nil {
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
// work.Dir contains mock binaries (BUG-2), mock logs, and the bare git origin
// (BUG-4), so mounting it at the same path gives containers full access.
func dockerVolumes(useDocker bool, workDir string) []string {
	if !useDocker {
		return nil
	}
	return []string{workDir + ":" + workDir}
}

// dockerExtraHosts returns the --extra-host entries needed for Docker executor jobs.
// host.docker.internal resolves to the host IP inside containers so they can reach
// the mock API server that GLUT starts on the host (BUG-3).
func dockerExtraHosts(useDocker bool) []string {
	if !useDocker {
		return nil
	}
	return []string{"host.docker.internal:host-gateway"}
}

// resolveDockerMode converts the three-state *bool Docker field into the two executor flags.
// nil (absent) → backward-compat: Docker for image: jobs, shell otherwise.
// &true → full Docker mode with volume/extra-host support.
// &false → force all jobs to shell, even those with image:.
func resolveDockerMode(docker *bool) (useDocker bool, forceShell bool) {
	if docker == nil {
		return false, false
	}
	if *docker {
		return true, false
	}
	return false, true
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
