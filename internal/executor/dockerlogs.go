package executor

import (
	"bufio"
	"context"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// gclBuildVolumeRE matches the gcl build volume naming pattern:
// gcl-{safeJobName}-{rand}-build
var gclBuildVolumeRE = regexp.MustCompile(`^gcl-(.+)-\d+-build$`)

type containerCapture struct {
	id      string
	jobName string // URL-decoded job name; empty if unknown
	logs    chan []byte
}

// dockerOutputMonitor watches Docker events for containers that use the GLUT
// volume and streams their output via docker logs. This captures the full
// container stdout+stderr that gcl discards due to pipe buffering.
type dockerOutputMonitor struct {
	mu         sync.Mutex
	captured   []*containerCapture
	volumeName string // GLUT volume name used to filter containers
	cancel     context.CancelFunc
	watchDone  chan struct{}
}

// startDockerOutputMonitor begins watching Docker events for containers that
// have the GLUT volume mounted. Call stop() after the pipeline finishes, then
// collectLogs() to get output.
func startDockerOutputMonitor(ctx context.Context, volumeName string) *dockerOutputMonitor {
	watchCtx, cancel := context.WithCancel(ctx)
	m := &dockerOutputMonitor{
		volumeName: volumeName,
		cancel:     cancel,
		watchDone:  make(chan struct{}),
	}
	go m.run(watchCtx)
	return m
}

func (m *dockerOutputMonitor) run(ctx context.Context) {
	defer close(m.watchDone)

	// Docker Desktop on WSL2 does not support --filter volume=NAME for
	// container events. We watch all container starts and filter by
	// inspecting mounts in the event loop.
	cmd := exec.CommandContext(ctx, "docker", "events",
		"--filter", "type=container",
		"--filter", "event=start",
		"--format", "{{.Actor.ID}}")

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
		// Filter: only capture containers that have the GLUT volume mounted.
		jobName, ok := containerInfo(containerID, m.volumeName)
		if !ok {
			continue
		}
		cap := &containerCapture{
			id:      containerID,
			jobName: jobName,
			logs:    make(chan []byte, 1),
		}
		m.mu.Lock()
		m.captured = append(m.captured, cap)
		m.mu.Unlock()

		go streamContainerLogs(cap)
	}
}

// streamContainerLogs runs docker logs --follow in the background and sends
// the combined output to cap.logs when the container exits.
func streamContainerLogs(cap *containerCapture) {
	// Use a background context with a long timeout so logs are collected
	// even after the monitor's context is cancelled (gcl already exited).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	out, _ := exec.CommandContext(ctx, "docker", "logs", "--follow", cap.id).CombinedOutput()
	cap.logs <- out
}

// stop cancels the event watcher and waits for it to finish.
func (m *dockerOutputMonitor) stop() {
	m.cancel()
	<-m.watchDone
}

// collectLogs waits for all docker logs goroutines to finish and merges the
// captured output into the provided jobs map. Must be called after stop().
func (m *dockerOutputMonitor) collectLogs(jobs map[string]JobOutput) {
	m.mu.Lock()
	captured := make([]*containerCapture, len(m.captured))
	copy(captured, m.captured)
	m.mu.Unlock()

	for _, cap := range captured {
		if cap.jobName == "" {
			continue
		}
		var output []byte
		select {
		case output = <-cap.logs:
		case <-time.After(15 * time.Second):
			continue
		}
		if len(output) == 0 {
			continue
		}
		job := jobs[cap.jobName]
		job.Name = cap.jobName
		job.Stdout = string(output)
		jobs[cap.jobName] = job
	}
}

// containerInfo inspects a container and returns the CI job name and whether
// the GLUT volume is mounted. ok is false when the container is not part of
// this GLUT test run (no GLUT volume) or when inspect fails.
func containerInfo(containerID, glutVolumeName string) (jobName string, ok bool) {
	out, err := exec.Command("docker", "inspect",
		"--format", "{{range .Mounts}}{{if eq .Type \"volume\"}}{{.Name}}\n{{end}}{{end}}",
		containerID).Output()
	if err != nil {
		return "", false
	}
	hasGlutVol := false
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(line)
		if name == glutVolumeName {
			hasGlutVol = true
		}
		if m := gclBuildVolumeRE.FindStringSubmatch(name); len(m) == 2 {
			decoded, err := url.PathUnescape(m[1])
			if err == nil {
				jobName = decoded
			} else {
				jobName = m[1]
			}
		}
	}
	if !hasGlutVol {
		return "", false
	}
	return jobName, true
}
