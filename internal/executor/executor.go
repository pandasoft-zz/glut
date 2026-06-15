package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pandasoft-zz/glut/internal/config"
)

const (
	pipelineFileName   = ".gitlab-ci.yml"
	defaultSystemPATH  = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	jobMarkerPrefix    = "GLUT_JOB|"
	dependencyOptional = "optional"
)

var (
	gitlabOutputLineRE   = regexp.MustCompile(`^(.+?) > (.*)$`)
	gitlabFinishedLineRE = regexp.MustCompile(`^(.+?) finished in .*\s+(PASS|FAIL|WARN)(?:\s+([0-9]+))?\s*$`)
	gitlabSummaryLineRE  = regexp.MustCompile(`^\s*(PASS|FAIL)\s+(.+?)\s*$`)
)

type ExecutorConfig struct {
	WorkspacePath    string
	PipelineYAML     string
	EnvVars          map[string]string
	UnsetVars        []string // variables to explicitly unset via --unset-variable
	MockBinPath      string
	Timeout          time.Duration
	Debug            bool
	Verbose          bool
	UseDocker        bool
	ForceShell       bool
	Privileged       bool
	DockerVolumes    []string
	DockerExtraHosts []string
	HostEnv          []string // nil falls back to os.Environ()
}

type RunResult struct {
	Jobs      map[string]JobOutput
	RawStdout string
	RawStderr string
}

type JobOutput struct {
	Name       string
	ExitStatus int
	Stdout     string
	Stderr     string
	Present    bool
	// When is the evaluated `when:` value reported by the job list phase
	// (e.g. "on_success", "manual"). Empty when the list phase did not run.
	When string
	// Executed reports whether the job actually ran in the pipeline. A job can
	// be present but not executed (e.g. `when: manual`).
	Executed bool
}

// JobListEntry is one job from `gitlab-ci-local --list-json`.
type JobListEntry struct {
	Name string `json:"name"`
	When string `json:"when"`
}

func Run(ctx context.Context, cfg ExecutorConfig) (RunResult, error) {
	if err := writePipeline(cfg); err != nil {
		return RunResult{}, err
	}

	runCtx, cancel := withTimeout(ctx, cfg.Timeout)
	defer cancel()

	args := append(baseArgs(cfg), dockerArgs(cfg)...)
	args = append(args, envArgs(cfg.EnvVars)...)
	args = append(args, unsetArgs(cfg.UnsetVars)...)
	var monitor *dockerOutputMonitor
	if cfg.UseDocker && len(cfg.DockerVolumes) > 0 {
		volName := strings.SplitN(cfg.DockerVolumes[0], ":", 2)[0]
		monitor = startDockerOutputMonitor(runCtx, volName)
	}

	stdout, stderr, err := runCommand(runCtx, cfg, args...)

	if monitor != nil {
		monitor.stop()
	}

	result := RunResult{
		Jobs:      parseJobOutputs(stdout, stderr),
		RawStdout: stdout,
		RawStderr: stderr,
	}
	mergeJobLogs(result.Jobs, cfg.WorkspacePath)
	if monitor != nil {
		monitor.collectLogs(result.Jobs)
	}
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return result, fmt.Errorf("run gitlab-ci-local: test timeout after %s", cfg.Timeout)
		}
		if len(result.Jobs) > 0 {
			return result, nil
		}
		return result, fmt.Errorf("run gitlab-ci-local: %w", err)
	}

	return result, nil
}

func ListJobs(ctx context.Context, cfg ExecutorConfig) ([]JobListEntry, error) {
	if err := writePipeline(cfg); err != nil {
		return nil, err
	}

	runCtx, cancel := withTimeout(ctx, cfg.Timeout)
	defer cancel()

	args := append([]string{"--list-json", "--file", pipelineFileName}, envArgs(cfg.EnvVars)...)
	args = append(args, unsetArgs(cfg.UnsetVars)...)
	stdout, stderr, err := runCommand(runCtx, cfg, args...)
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("list gitlab-ci-local jobs: test timeout after %s", cfg.Timeout)
		}
		return nil, fmt.Errorf("list gitlab-ci-local jobs: %w", err)
	}

	return parseJobListJSON(stdout, stderr)
}

func CheckDependencies(ctx context.Context, hostEnv []string) []string {
	type check struct {
		name     string
		args     []string
		optional bool
	}

	checks := []check{
		{name: "gitlab-ci-local", args: []string{"--version"}},
		{name: "git", args: []string{"--version"}},
		{name: "bash", args: []string{"--version"}},
		{name: "rsync", args: []string{"--version"}, optional: true},
	}

	var problems []string
	for _, item := range checks {
		binaryPath := resolveExecutable(item.name, hostEnv)
		// When a custom hostEnv is provided, resolveExecutable returns the bare
		// name when the binary is absent from that PATH. Avoid exec.Command
		// falling back to the process PATH in that case.
		if hostEnv != nil && binaryPath == item.name {
			if item.optional {
				problems = append(problems, fmt.Sprintf("%s: not available (not in PATH, GLUT can use native copy fallback)", item.name))
			} else {
				problems = append(problems, fmt.Sprintf("%s: not available (not in PATH)", item.name))
			}
			continue
		}
		cmd := exec.CommandContext(ctx, binaryPath, item.args...)
		cmd.Env = hostEnv // nil = inherit process env
		if output, err := cmd.CombinedOutput(); err != nil {
			if item.optional {
				problems = append(problems, fmt.Sprintf("%s: not available (%s, GLUT can use native copy fallback)", item.name, strings.TrimSpace(firstLine(string(output), err.Error()))))
				continue
			}
			problems = append(problems, fmt.Sprintf("%s: not available (%s)", item.name, strings.TrimSpace(firstLine(string(output), err.Error()))))
		}
	}

	return problems
}

func writePipeline(cfg ExecutorConfig) error {
	if strings.TrimSpace(cfg.WorkspacePath) == "" {
		return fmt.Errorf("write pipeline file: workspace path is required")
	}

	pipelinePath := filepath.Join(cfg.WorkspacePath, pipelineFileName)
	if err := os.WriteFile(pipelinePath, []byte(cfg.PipelineYAML), 0644); err != nil {
		return fmt.Errorf("write pipeline file %s: %w", pipelinePath, err)
	}
	return nil
}

func withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func runCommand(ctx context.Context, cfg ExecutorConfig, args ...string) (string, string, error) {
	binaryPath := resolveExecutable("gitlab-ci-local", cfg.HostEnv)
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		cmd := exec.CommandContext(ctx, binaryPath, args...)
		cmd.Dir = cfg.WorkspacePath
		cmd.Env = buildCommandEnv(cfg)

		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return stdout.String(), stderr.String(), ctx.Err()
			}
			// Retry on ETXTBSY (overlayfs write-reference not yet cleared).
			if attempt < maxAttempts-1 && strings.Contains(err.Error(), "text file busy") {
				time.Sleep(time.Duration(1<<uint(attempt)) * 10 * time.Millisecond)
				continue
			}
			return stdout.String(), stderr.String(), fmt.Errorf("%w; stdout: %s; stderr: %s", err, tailForError(stdout.String()), tailForError(stderr.String()))
		}
		return stdout.String(), stderr.String(), nil
	}
	panic("unreachable: runCommand loop always returns on the final attempt")
}

func buildCommandEnv(cfg ExecutorConfig) []string {
	env := make(map[string]string, len(cfg.EnvVars)+8)
	for key, value := range cfg.EnvVars {
		env[key] = value
	}

	hostEnv := cfg.HostEnv
	if hostEnv == nil {
		hostEnv = os.Environ()
	}
	host := envSliceToMap(hostEnv)

	basePath := host["PATH"]
	if basePath == "" {
		basePath = defaultSystemPATH
	}
	if cfg.MockBinPath != "" {
		basePath = cfg.MockBinPath + string(os.PathListSeparator) + basePath
	}
	env["PATH"] = basePath

	for _, key := range []string{
		"HOME", "TMPDIR", "TMP",
		"DOCKER_CONFIG", "DOCKER_HOST", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH",
	} {
		if v := host[key]; v != "" {
			env[key] = v
		}
	}

	if _, ok := env[config.EnvMockLogDir]; !ok {
		if v := host[config.EnvMockLogDir]; v != "" {
			env[config.EnvMockLogDir] = v
		}
	}
	if _, ok := env[config.EnvMockBinReal]; !ok {
		if v := host[config.EnvMockBinReal]; v != "" {
			env[config.EnvMockBinReal] = v
		}
	}

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	items := make([]string, 0, len(keys))
	for _, key := range keys {
		items = append(items, key+"="+env[key])
	}
	return items
}

// resolveExecutable looks up name in hostEnv's PATH when hostEnv is non-nil,
// falling back to exec.LookPath (process PATH) when hostEnv is nil.
func resolveExecutable(name string, hostEnv []string) string {
	if hostEnv == nil {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
		return name
	}
	host := envSliceToMap(hostEnv)
	for _, dir := range filepath.SplitList(host["PATH"]) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return name
}

func envSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		key, value, _ := strings.Cut(kv, "=")
		m[key] = value
	}
	return m
}

func baseArgs(cfg ExecutorConfig) []string {
	args := []string{"--no-color", "--file", pipelineFileName}
	if cfg.ForceShell {
		return append([]string{"--force-shell-executor"}, args...)
	}
	if !cfg.UseDocker {
		return append([]string{"--shell-executor-no-image"}, args...)
	}
	return args
}

func unsetArgs(vars []string) []string {
	args := make([]string, 0, len(vars)*2)
	for _, v := range vars {
		args = append(args, "--unset-variable", v)
	}
	return args
}

func dockerArgs(cfg ExecutorConfig) []string {
	var args []string
	for _, vol := range cfg.DockerVolumes {
		args = append(args, "--volume", vol)
	}
	for _, host := range cfg.DockerExtraHosts {
		args = append(args, "--extra-host", host)
	}
	if cfg.Privileged {
		args = append(args, "--privileged")
	}
	return args
}

func envArgs(envVars map[string]string) []string {
	keys := make([]string, 0, len(envVars))
	for key := range envVars {
		// Keys with GCL_ prefix configure gitlab-ci-local itself via process
		// environment. Passing them as CI --variable args makes GCL parse them
		// as CLI options and can break older versions.
		if strings.HasPrefix(key, "GCL_") {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	args := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		args = append(args, "--variable", key+"="+envVars[key])
	}
	return args
}

func parseJobOutputs(stdout string, stderr string) map[string]JobOutput {
	jobs := make(map[string]JobOutput)
	parseJobMarkers(jobs, stdout)
	parseJobMarkers(jobs, stderr)
	parseGitLabOutput(jobs, stdout, "stdout")
	parseGitLabOutput(jobs, stderr, "stderr")
	return jobs
}

func parseJobMarkers(jobs map[string]JobOutput, raw string) {
	scanner := newLineScanner(raw)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, jobMarkerPrefix) {
			continue
		}

		job, ok := parseJobMarker(line)
		if !ok || job.Name == "" {
			continue
		}
		jobs[job.Name] = job
	}
}

func parseJobMarker(line string) (JobOutput, bool) {
	job := JobOutput{Present: true}
	parts := strings.Split(line, "|")
	for _, part := range parts[1:] {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch key {
		case "name":
			job.Name = value
		case "exit":
			status, err := strconv.Atoi(value)
			if err != nil {
				return JobOutput{}, false
			}
			job.ExitStatus = status
		case "stdout":
			job.Stdout = strings.ReplaceAll(value, `\n`, "\n")
		case "stderr":
			job.Stderr = strings.ReplaceAll(value, `\n`, "\n")
		case "present":
			job.Present = value != "false"
		}
	}
	return job, true
}

func parseGitLabOutput(jobs map[string]JobOutput, raw string, stream string) {
	scanner := newLineScanner(raw)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, jobMarkerPrefix) {
			continue
		}

		if matches := gitlabFinishedLineRE.FindStringSubmatch(line); len(matches) == 4 {
			job := ensureJob(jobs, strings.TrimSpace(matches[1]))
			job.ExitStatus = statusFromGitLab(matches[2], matches[3])
			jobs[job.Name] = job
			continue
		}

		if matches := gitlabSummaryLineRE.FindStringSubmatch(line); len(matches) == 3 {
			jobName := strings.TrimSpace(matches[2])
			if strings.Contains(jobName, " ") {
				continue
			}
			job := ensureJob(jobs, jobName)
			if matches[1] == "PASS" {
				job.ExitStatus = 0
			} else if job.ExitStatus == 0 {
				job.ExitStatus = 1
			}
			jobs[job.Name] = job
			continue
		}

		if matches := gitlabOutputLineRE.FindStringSubmatch(line); len(matches) == 3 {
			jobName := strings.TrimSpace(matches[1])
			if strings.HasPrefix(jobName, ">") || strings.Contains(jobName, " ") {
				continue
			}
			// Skip gcl heartbeat messages — not real container output.
			if matches[2] == "still running..." {
				continue
			}
			job := ensureJob(jobs, jobName)
			switch stream {
			case "stderr":
				job.Stderr = appendOutputLine(job.Stderr, matches[2])
			default:
				job.Stdout = appendOutputLine(job.Stdout, matches[2])
			}
			jobs[job.Name] = job
		}
	}
}

// mergeJobLogs reads .gitlab-ci-local/output/*.log files and overwrites the
// Stdout of each matching job. In Docker mode, gitlab-ci-local buffers
// container output due to missing TTY allocation; the log files contain the
// complete output that is not captured in gcl's own stdout stream.
func mergeJobLogs(jobs map[string]JobOutput, workspacePath string) {
	logDir := filepath.Join(workspacePath, ".gitlab-ci-local", "output")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		encoded := strings.TrimSuffix(entry.Name(), ".log")
		jobName, err := url.PathUnescape(encoded)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(logDir, entry.Name()))
		if err != nil || len(data) == 0 {
			continue
		}
		job := jobs[jobName]
		job.Name = jobName
		job.Present = true
		job.Stdout = string(data)
		jobs[jobName] = job
	}
}

func ensureJob(jobs map[string]JobOutput, name string) JobOutput {
	job := jobs[name]
	job.Name = name
	job.Present = true
	return job
}

func statusFromGitLab(status string, rawExit string) int {
	if status == "PASS" {
		return 0
	}
	exitStatus, err := strconv.Atoi(strings.TrimSpace(rawExit))
	if err != nil || exitStatus == 0 {
		return 1
	}
	return exitStatus
}

func appendOutputLine(current string, line string) string {
	if current == "" {
		return line
	}
	return current + "\n" + line
}

// parseJobListJSON parses `gitlab-ci-local --list-json` stdout. Jobs whose
// evaluated `when` is "never" are dropped: in real GitLab CI they are absent
// from the pipeline.
func parseJobListJSON(stdout string, stderr string) ([]JobListEntry, error) {
	// gitlab-ci-local may print diagnostics before the JSON array — including
	// warnings that themselves contain '[' (e.g. "[CI_COMMIT_BRANCH,...]") —
	// so try every '[' until one parses as the job array.
	raw := strings.TrimSpace(stdout)
	var entries []JobListEntry
	var parseErr error
	parsed := false
	for offset := 0; offset < len(raw); {
		idx := strings.Index(raw[offset:], "[")
		if idx < 0 {
			break
		}
		start := offset + idx
		entries = nil
		if err := json.Unmarshal([]byte(raw[start:]), &entries); err != nil {
			if parseErr == nil {
				parseErr = err
			}
			offset = start + 1
			continue
		}
		parsed = true
		break
	}
	if !parsed {
		if parseErr == nil {
			parseErr = errors.New("no JSON array in output")
		}
		return nil, fmt.Errorf("parse gitlab-ci-local --list-json output (requires gitlab-ci-local with --list-json support): %w; stderr: %s", parseErr, tailForError(stderr))
	}

	seen := make(map[string]struct{}, len(entries))
	jobs := make([]JobListEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Name == "" || entry.When == "never" {
			continue
		}
		if _, ok := seen[entry.Name]; ok {
			continue
		}
		seen[entry.Name] = struct{}{}
		jobs = append(jobs, entry)
	}
	return jobs, nil
}

func newLineScanner(raw string) *bufio.Scanner {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	return scanner
}

func tailForError(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	lines := strings.Split(raw, "\n")
	if len(lines) <= 8 {
		return raw
	}
	return strings.Join(lines[len(lines)-8:], "\n")
}

func firstLine(values ...string) string {
	for _, value := range values {
		for _, line := range strings.Split(value, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				return line
			}
		}
	}
	return ""
}
