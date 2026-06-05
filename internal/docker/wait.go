package docker

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

const (
	dialTimeout        = 1 * time.Second
	noTTYLogInterval   = 5 * time.Second
	barWidth           = 20
	cleanPollInterval  = 200 * time.Millisecond
)

// Wait blocks until the Docker daemon is reachable or the timeout expires.
// Progress is written to w; display adapts to whether w is a TTY.
// Returns nil immediately if the daemon is already reachable.
func Wait(ctx context.Context, w io.Writer, timeout time.Duration) error {
	endpoint := Endpoint()

	if dial(endpoint) == nil {
		return nil
	}

	isTTY := isWriterTTY(w)
	start := time.Now()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var nextLog time.Time // zero value causes first log to fire immediately

	for {
		select {
		case <-ctx.Done():
			if isTTY {
				_, _ = fmt.Fprintln(w, "")
			}
			return ctx.Err()
		case t := <-ticker.C:
			elapsed := t.Sub(start)
			if elapsed >= timeout {
				if isTTY {
					_, _ = fmt.Fprintln(w, "")
				}
				return fmt.Errorf("docker daemon not ready after %s (%s)", timeout.Round(time.Second), endpoint)
			}

			if dial(endpoint) == nil {
				if isTTY {
					_, _ = fmt.Fprintln(w, "")
				}
				return nil
			}

			if isTTY {
				renderProgress(w, elapsed, timeout)
			} else if !t.Before(nextLog) {
				_, _ = fmt.Fprintf(w, "waiting for Docker daemon at %s (%ds elapsed, timeout %ds)\n",
					endpoint, int(elapsed.Seconds()), int(timeout.Seconds()))
				nextLog = t.Add(noTTYLogInterval)
			}
		}
	}
}

// Endpoint returns the Docker daemon address from DOCKER_HOST or the default socket path.
func Endpoint() string {
	if h := os.Getenv("DOCKER_HOST"); h != "" {
		return h
	}
	return "unix:///var/run/docker.sock"
}

func dial(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	var network, addr string
	switch u.Scheme {
	case "tcp", "http", "https":
		network, addr = "tcp", u.Host
	case "unix", "":
		network = "unix"
		addr = u.Path
		if addr == "" {
			addr = endpoint
		}
	default:
		return fmt.Errorf("unsupported Docker endpoint scheme: %s", u.Scheme)
	}
	conn, err := net.DialTimeout(network, addr, dialTimeout)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}

// WaitClean blocks until no GLUT or GCL volumes remain visible in the Docker
// daemon, or until the timeout expires. GLUT volumes follow the "glut-*"
// naming convention; GCL per-job build volumes follow "gcl-*". Polling
// continues until both filters return empty output, which confirms that
// every volume created during the previous test has been fully removed and
// the daemon's registry is clean before the next test starts.
//
// Overlay-FS layer reclamation by the daemon is asynchronous and cannot be
// detected through this check — this function only guarantees that no named
// Docker objects from the previous test remain registered.
func WaitClean(ctx context.Context, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !hasGlutVolumes() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(cleanPollInterval):
		}
	}
}

// hasGlutVolumes returns true when any glut-* or gcl-* volumes are still
// visible in the daemon.
func hasGlutVolumes() bool {
	for _, prefix := range []string{"glut-", "gcl-"} {
		out, err := exec.Command("docker", "volume", "ls",
			"-q", "--filter", "name="+prefix).Output()
		if err != nil {
			return false // daemon unreachable — do not block
		}
		if len(strings.TrimSpace(string(out))) > 0 {
			return true
		}
	}
	return false
}

func isWriterTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd())
}

func renderProgress(w io.Writer, elapsed, timeout time.Duration) {
	r := lipgloss.NewRenderer(w)

	pct := float64(elapsed) / float64(timeout)
	if pct > 1 {
		pct = 1
	}
	filled := int(pct * barWidth)

	bar := r.NewStyle().Foreground(lipgloss.Color("6")).Render(strings.Repeat("█", filled)) +
		r.NewStyle().Foreground(lipgloss.Color("8")).Render(strings.Repeat("░", barWidth-filled))
	label := r.NewStyle().Foreground(lipgloss.Color("8")).Render(
		fmt.Sprintf("  %ds / %ds", int(elapsed.Seconds()), int(timeout.Seconds())))
	prefix := r.NewStyle().Foreground(lipgloss.Color("11")).Render("⏳")

	_, _ = fmt.Fprintf(w, "\r%s Waiting for Docker daemon  %s%s", prefix, bar, label)
}
