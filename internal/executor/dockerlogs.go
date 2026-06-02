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
	cancel     context.CancelFunc
	watchDone  chan struct{}
}

// startDockerOutputMonitor begins watching Docker events for the given volume.
// Call stop() after the pipeline finishes, then collectLogs() to merge output.
func startDockerOutputMonitor(ctx context.Context, volumeName string) *dockerOutputMonitor {
	watchCtx, cancel := context.WithCancel(ctx)
	m := &dockerOutputMonitor{
		known:      make(map[string]*containerCapture),
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
	// container events so we filter by inspecting mounts on each start.
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
		m.known[containerID] = cap
		m.mu.Unlock()

		// docker wait blocks until container exits, then we immediately call
		// docker logs — this races against gcl's docker rm but wins because
		// gcl's cleanup runs through multiple async Node.js Promise hops.
		go captureAfterExit(cap)
	}
}

// captureAfterExit waits for the container to exit then immediately captures
// its logs before gcl's async cleanup can call docker rm.
func captureAfterExit(cap *containerCapture) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Block until the container exits.
	exec.CommandContext(ctx, "docker", "wait", cap.id).Run() //nolint:errcheck

	// Container has exited; capture logs immediately before docker rm.
	out, _ := exec.CommandContext(ctx, "docker", "logs", cap.id).CombinedOutput()
	cap.logs <- out
}

// stop cancels the event watcher and waits for it to finish.
func (m *dockerOutputMonitor) stop() {
	m.cancel()
	<-m.watchDone
}

// collectLogs waits for all log-capture goroutines and merges output into jobs.
// Must be called after stop().
func (m *dockerOutputMonitor) collectLogs(jobs map[string]JobOutput) {
	m.mu.Lock()
	caps := make([]*containerCapture, 0, len(m.known))
	for _, c := range m.known {
		caps = append(caps, c)
	}
	m.mu.Unlock()

	for _, cap := range caps {
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
// this GLUT test run or when inspect fails.
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
