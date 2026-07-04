package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestDockerOutputMonitorStopDoesNotKillInProgressCapture guards against
// stop() cancelling the same context used by captureAfterExit: a "docker
// logs --follow" still draining the final job's output must survive stop()
// being called, since collectLogs (not stop) owns waiting for captures to
// finish.
func TestDockerOutputMonitorStopDoesNotKillInProgressCapture(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("docker CLI mocking assumes a POSIX shell")
	}

	dir := t.TempDir()
	volumeName := "glut-test-vol"
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
  events)
    echo abc123
    exec sleep 5
    ;;
  inspect)
    echo %q
    echo "gcl-myjob-42-build"
    ;;
  logs)
    sleep 1
    echo "captured job output"
    ;;
esac
`, volumeName)
	dockerPath := filepath.Join(dir, "docker")
	if err := os.WriteFile(dockerPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	monitor := startDockerOutputMonitor(context.Background(), volumeName, nil)
	// Give the events watcher time to see the container and start the
	// capture goroutine, which is now mid-"sleep 1" inside docker logs.
	time.Sleep(300 * time.Millisecond)

	monitor.stop()

	jobs := map[string]JobOutput{}
	monitor.collectLogs(jobs)

	job, ok := jobs["myjob"]
	if !ok || strings.TrimSpace(job.Stdout) != "captured job output" {
		t.Fatalf("expected the in-progress capture to survive stop(), got %#v", jobs)
	}
}

// TestContainerInfoUsesHostEnvForDockerHost guards against the docker CLI
// helpers reading the real process environment instead of a caller-resolved
// hostEnv: with a custom DOCKER_HOST, the monitor's `docker inspect` call
// must talk to that daemon, not whatever DOCKER_HOST the actual process
// happens to have (or lack).
func TestContainerInfoUsesHostEnvForDockerHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("docker CLI mocking assumes a POSIX shell")
	}

	dir := t.TempDir()
	callLog := filepath.Join(dir, "calls.log")
	script := fmt.Sprintf(`#!/bin/sh
echo "DOCKER_HOST=$DOCKER_HOST" >> %q
echo "glut-test-vol"
`, callLog)
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	hostEnv := []string{"DOCKER_HOST=tcp://custom-daemon-for-test:1234"}
	if _, ok := containerInfo(context.Background(), "abc123", "glut-test-vol", hostEnv); !ok {
		t.Fatal("expected containerInfo to report the volume match")
	}

	calls, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	if !strings.Contains(string(calls), "DOCKER_HOST=tcp://custom-daemon-for-test:1234") {
		t.Fatalf("expected the docker subprocess to see hostEnv's DOCKER_HOST, got: %s", calls)
	}
}
