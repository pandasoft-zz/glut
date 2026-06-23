package docker

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestDialUnsupportedScheme(t *testing.T) {
	t.Parallel()
	if err := dial("ftp://localhost:1234"); err == nil {
		t.Fatal("expected error for unsupported scheme, got nil")
	}
}

func TestDialUnreachable(t *testing.T) {
	t.Parallel()
	if err := dial("unix:///tmp/glut-nonexistent-socket-test.sock"); err == nil {
		t.Fatal("expected error for nonexistent socket, got nil")
	}
}

func TestResolveVolumeStrategyExplicit(t *testing.T) {
	t.Parallel()
	for _, strategy := range []string{VolumeStrategyBind, VolumeStrategyVolume} {
		got := ResolveVolumeStrategy(strategy)
		if got != strategy {
			t.Errorf("ResolveVolumeStrategy(%q) = %q, want %q", strategy, got, strategy)
		}
	}
}

func TestResolveVolumeStrategyAutoReturnsKnown(t *testing.T) {
	t.Parallel()
	got := ResolveVolumeStrategy(VolumeStrategyAuto)
	if got != VolumeStrategyBind && got != VolumeStrategyVolume {
		t.Errorf("ResolveVolumeStrategy(auto) = %q, want bind or volume", got)
	}
}

func TestResolveVolumeStrategyEmptyEqualsAuto(t *testing.T) {
	t.Parallel()
	if ResolveVolumeStrategy("") != ResolveVolumeStrategy(VolumeStrategyAuto) {
		t.Error("empty strategy should behave same as auto")
	}
}

func TestResolveAutoVolumeStrategy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		dockerHost  string
		inContainer bool
		want        string
	}{
		// DinD: remote daemon over TCP. The workspace path is invisible to it,
		// so bind mounts are impossible regardless of /.dockerenv. This is the
		// case Kubernetes runners hit (no /.dockerenv) — must still pick volume.
		{name: "tcp daemon, no dockerenv", dockerHost: "tcp://docker:2376", inContainer: false, want: VolumeStrategyVolume},
		{name: "tcp daemon, in container", dockerHost: "tcp://docker:2376", inContainer: true, want: VolumeStrategyVolume},
		{name: "ssh daemon", dockerHost: "ssh://user@host", inContainer: false, want: VolumeStrategyVolume},
		// Local socket inside a container (devcontainer / DinD via mounted socket).
		{name: "unix socket in container", dockerHost: "unix:///var/run/docker.sock", inContainer: true, want: VolumeStrategyVolume},
		{name: "empty host in container", dockerHost: "", inContainer: true, want: VolumeStrategyVolume},
		// Native Linux host with a local daemon: bind mounts work, no overhead.
		{name: "empty host, native host", dockerHost: "", inContainer: false, want: VolumeStrategyBind},
		{name: "unix socket, native host", dockerHost: "unix:///var/run/docker.sock", inContainer: false, want: VolumeStrategyBind},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveAutoVolumeStrategy(tc.dockerHost, tc.inContainer); got != tc.want {
				t.Errorf("resolveAutoVolumeStrategy(%q, %v) = %q, want %q", tc.dockerHost, tc.inContainer, got, tc.want)
			}
		})
	}
}

func TestIsRemoteDockerHost(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		host string
		want bool
	}{
		{"tcp://docker:2376", true},
		{"  tcp://docker:2376  ", true},
		{"ssh://user@host", true},
		{"unix:///var/run/docker.sock", false},
		{"", false},
	} {
		if got := isRemoteDockerHost(tc.host); got != tc.want {
			t.Errorf("isRemoteDockerHost(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}

func TestWaitSucceedsWhenDaemonReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot open tcp listener: %v", err)
	}
	defer func() { _ = ln.Close() }()

	addr := "tcp://" + ln.Addr().String()
	t.Setenv("DOCKER_HOST", addr)

	if err := Wait(context.Background(), io.Discard, 5*time.Second); err != nil {
		t.Fatalf("Wait() = %v, want nil", err)
	}
}

func TestWaitTimesOutWhenDaemonUnreachable(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///tmp/glut-no-daemon-test.sock")

	err := Wait(context.Background(), io.Discard, 2*time.Second)
	if err == nil {
		t.Fatal("Wait() expected timeout error, got nil")
	}
}

func TestWaitCancelledByContext(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///tmp/glut-no-daemon-cancel-test.sock")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Wait(ctx, io.Discard, 30*time.Second)
	if err == nil {
		t.Fatal("Wait() expected context error, got nil")
	}
}
