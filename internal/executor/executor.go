package executor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
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
	MockBinPath      string
	Timeout          time.Duration
	Debug            bool
	Verbose          bool
	UseDocker        bool
	ForceShell       bool
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
}

func Run(ctx context.Context, cfg ExecutorConfig) (RunResult, error) {
	if err := writePipeline(cfg); err != nil {
		return RunResult{}, err
	}

	runCtx, cancel := withTimeout(ctx, cfg.Timeout)
	defer cancel()

	args := append(baseArgs(cfg), dockerArgs(cfg)...)
	args = append(args, envArgs(cfg.EnvVars)...)
	stdout, stderr, err := runCommand(runCtx, cfg, args...)
	result := RunResult{
		Jobs:      parseJobOutputs(stdout, stderr),
		RawStdout: stdout,
		RawStderr: stderr,
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

func ListJobs(ctx context.Context, cfg ExecutorConfig) ([]string, error) {
	if err := writePipeline(cfg); err != nil {
		return nil, err
	}

	runCtx, cancel := withTimeout(ctx, cfg.Timeout)
	defer cancel()

	args := append([]string{"--list", "--file", pipelineFileName}, envArgs(cfg.EnvVars)...)
	stdout, stderr, err := runCommand(runCtx, cfg, args...)
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("list gitlab-ci-local jobs: test timeout after %s", cfg.Timeout)
		}
		return nil, fmt.Errorf("list gitlab-ci-local jobs: %w", err)
	}

	return parseJobList(stdout, stderr), nil
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
	return "", "", fmt.Errorf("executor: runCommand: could not exec binary after %d attempts", maxAttempts)
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

func dockerArgs(cfg ExecutorConfig) []string {
	var args []string
	for _, vol := range cfg.DockerVolumes {
		args = append(args, "--volume", vol)
	}
	for _, host := range cfg.DockerExtraHosts {
		args = append(args, "--extra-host", host)
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

func parseJobList(stdout string, stderr string) []string {
	_ = stderr
	seen := make(map[string]struct{})
	var jobs []string
	scanner := newLineScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "GLUT_JOB|") {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		jobs = append(jobs, line)
	}
	return jobs
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
