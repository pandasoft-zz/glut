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

func TestWaitSucceedsWhenDaemonReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot open tcp listener: %v", err)
	}
	defer ln.Close()

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
