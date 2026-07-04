package executor

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/pandasoft-zz/glut/internal/workspace"
)

const (
	// logCaptureTimeout caps how long captureAfterExit waits for a container
	// to exit and its logs to be streamed.
	logCaptureTimeout = 5 * time.Minute
	// collectLogsTimeout is the per-container deadline inside collectLogs.
	collectLogsTimeout = 15 * time.Second
)

type containerCapture struct {
	id      string
	jobName string // URL-decoded job name; empty if unknown
	logs    chan []byte
}

// dockerOutputMonitor watches Docker container events for containers that use
// the GLUT volume and captures their output before docker rm is called.
// Strategy: on start, launch a goroutine that blocks on "docker wait" and then
// immediately calls "docker logs" — this races against gcl's docker rm cleanup
// but consistently wins because gcl's cleanup runs async (multiple Promise hops
// in Node.js) while our goroutine calls docker logs synchronously.
type dockerOutputMonitor struct {
	mu         sync.Mutex
	known      map[string]*containerCapture // containerID → capture
	volumeName string
	// hostEnv is set on every docker CLI invocation this monitor makes (nil
	// inherits the process environment, matching exec.Cmd's own convention),
	// so a custom DOCKER_HOST talks to the same daemon gitlab-ci-local uses.
	hostEnv []string
	// watchCancel stops only the "docker events" watcher (called from stop()).
	// captureCtx/captureCancel govern the per-container "docker logs --follow"
	// goroutines and are cancelled separately, after collectLogs() returns —
	// sharing watchCtx here would kill a capture still draining the final
	// job's output the instant stop() is called, silently losing it.
	watchCancel   context.CancelFunc
	captureCtx    context.Context
	captureCancel context.CancelFunc
	watchDone     chan struct{}
}

// startDockerOutputMonitor begins watching Docker events for the given volume.
// Call stop() after the pipeline finishes, then collectLogs() to merge output.
func startDockerOutputMonitor(ctx context.Context, volumeName string, hostEnv []string) *dockerOutputMonitor {
	watchCtx, watchCancel := context.WithCancel(ctx)
	captureCtx, captureCancel := context.WithCancel(ctx)
	m := &dockerOutputMonitor{
		known:         make(map[string]*containerCapture),
		volumeName:    volumeName,
		hostEnv:       hostEnv,
		watchCancel:   watchCancel,
		captureCtx:    captureCtx,
		captureCancel: captureCancel,
		watchDone:     make(chan struct{}),
	}
	go m.run(watchCtx)
	return m
}

func (m *dockerOutputMonitor) run(ctx context.Context) {
	defer close(m.watchDone)

	// Docker Desktop on WSL2 does not support --filter volume=NAME for
	// container events so we filter by inspecting mounts on each start.
	cmd := exec.CommandContext(ctx, "docker", "events",
		"--filter", "type=container",
		"--filter", "event=start",
		"--format", "{{.Actor.ID}}")
	cmd.Env = m.hostEnv

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	defer cmd.Wait() //nolint:errcheck

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		containerID := strings.TrimSpace(scanner.Text())
		if containerID == "" {
			continue
		}
		jobName, ok := containerInfo(ctx, containerID, m.volumeName, m.hostEnv)
		if !ok {
			continue
		}
		cap := &containerCapture{
			id:      containerID,
			jobName: jobName,
			logs:    make(chan []byte, 1),
		}
		m.mu.Lock()
		m.known[containerID] = cap
		m.mu.Unlock()

		// docker wait blocks until container exits, then we immediately call
		// docker logs — this races against gcl's docker rm but wins because
		// gcl's cleanup runs through multiple async Node.js Promise hops.
		// Uses captureCtx (not the watcher's ctx) so stop() cancelling the
		// events watcher cannot kill a capture still in progress.
		go captureAfterExit(m.captureCtx, cap, m.hostEnv)
	}
}

// captureAfterExit streams the container's logs in real-time until it exits.
// Using --follow eliminates the docker wait → docker logs race against GCL's
// async docker rm: by the time the container exits and --follow returns, all
// output has already been received, so a concurrent docker rm cannot cause a
// "No such container" error on a separate docker logs call.
func captureAfterExit(parentCtx context.Context, cap *containerCapture, hostEnv []string) {
	ctx, cancel := context.WithTimeout(parentCtx, logCaptureTimeout)
	defer cancel()

	// --follow streams stdout until the container exits, then terminates.
	// Output() (not CombinedOutput) keeps daemon error messages off the job stdout.
	logsCmd := exec.CommandContext(ctx, "docker", "logs", "--follow", cap.id)
	logsCmd.Env = hostEnv
	out, err := logsCmd.Output()
	if err != nil {
		cap.logs <- nil
		return
	}
	cap.logs <- out
}

// stop cancels the event watcher and waits for it to finish. It does not
// touch capture goroutines still draining container logs — those are
// cancelled by collectLogs once it is done waiting on them.
func (m *dockerOutputMonitor) stop() {
	m.watchCancel()
	<-m.watchDone
}

// collectLogs waits for all log-capture goroutines and merges output into
// jobs. Must be called after stop(). Cancels captureCtx before returning,
// releasing any capture goroutine still running past its own wait here.
func (m *dockerOutputMonitor) collectLogs(jobs map[string]JobOutput) {
	defer m.captureCancel()

	m.mu.Lock()
	caps := make([]*containerCapture, 0, len(m.known))
	for _, c := range m.known {
		caps = append(caps, c)
	}
	m.mu.Unlock()

	type captureResult struct {
		jobName string
		output  []byte
	}
	results := make(chan captureResult, len(caps))

	var wg sync.WaitGroup
	for _, cap := range caps {
		if cap.jobName == "" {
			continue
		}
		wg.Add(1)
		c := cap
		go func() {
			defer wg.Done()
			t := time.NewTimer(collectLogsTimeout)
			defer t.Stop()
			select {
			case output := <-c.logs:
				results <- captureResult{c.jobName, output}
			case <-t.C:
			}
		}()
	}
	wg.Wait()
	close(results)

	for r := range results {
		if len(r.output) == 0 {
			continue
		}
		job := jobs[r.jobName]
		job.Name = r.jobName
		job.Stdout = string(r.output)
		jobs[r.jobName] = job
	}
}

// containerInfo inspects a container and returns the CI job name and whether
// the GLUT volume is mounted. ok is false when the container is not part of
// this GLUT test run or when inspect fails.
func containerInfo(ctx context.Context, containerID, glutVolumeName string, hostEnv []string) (jobName string, ok bool) {
	inspectCmd := exec.CommandContext(ctx, "docker", "inspect",
		"--format", "{{range .Mounts}}{{if eq .Type \"volume\"}}{{.Name}}\n{{end}}{{end}}",
		containerID)
	inspectCmd.Env = hostEnv
	out, err := inspectCmd.Output()
	if err != nil {
		return "", false
	}
	hasGlutVol := false
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(line)
		if name == glutVolumeName {
			hasGlutVol = true
		}
		if decoded, ok := workspace.GCLJobName(name); ok {
			jobName = decoded
		}
	}
	if !hasGlutVol {
		return "", false
	}
	return jobName, true
}
