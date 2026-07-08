package docker

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestEndpointResolvesDockerHost(t *testing.T) {
	t.Parallel()
	if got := Endpoint([]string{"DOCKER_HOST=tcp://daemon:2375"}); got != "tcp://daemon:2375" {
		t.Fatalf("Endpoint() = %q, want the DOCKER_HOST value", got)
	}
	// An empty (but non-nil) host env must not fall back to the process env.
	if got := Endpoint([]string{}); got != "unix:///var/run/docker.sock" {
		t.Fatalf("Endpoint() = %q, want the default socket", got)
	}
}

func TestIsWriterTTYForPlainWriter(t *testing.T) {
	t.Parallel()
	if isWriterTTY(&bytes.Buffer{}) {
		t.Fatal("a bytes.Buffer must never look like a TTY")
	}
}

func TestRenderProgressDrawsBoundedBar(t *testing.T) {
	t.Parallel()
	r := lipgloss.NewRenderer(&bytes.Buffer{})

	var half bytes.Buffer
	renderProgress(&half, 30*time.Second, 60*time.Second, r)
	if !strings.Contains(half.String(), "Waiting for Docker daemon") || !strings.Contains(half.String(), "30s / 60s") {
		t.Fatalf("progress line = %q", half.String())
	}

	// Elapsed beyond the timeout must clamp at 100%, not overflow the bar.
	var over bytes.Buffer
	renderProgress(&over, 2*time.Minute, time.Minute, r)
	if !strings.Contains(over.String(), "120s / 60s") {
		t.Fatalf("overflow progress line = %q", over.String())
	}
}
