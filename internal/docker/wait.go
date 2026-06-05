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
	dialTimeout      = 1 * time.Second
	noTTYLogInterval = 5 * time.Second
	barWidth         = 20
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

// Volume strategy constants control how GLUT provides workspace files to
// Docker job containers.
const (
	// VolumeStrategyAuto detects the best strategy for the current environment.
	// On native Linux Docker, bind mounts are used; on Docker Desktop / WSL2
	// (where the workspace lives on a Windows-backed 9P filesystem) Docker
	// named volumes are used instead.
	VolumeStrategyAuto = "auto"
	// VolumeStrategyBind uses a host bind mount. The workspace directory is
	// mounted directly into containers at the same absolute path. No Docker
	// named volume or Alpine populate container is needed. Requires a Docker
	// daemon that can resolve host paths (native Linux Docker).
	VolumeStrategyBind = "bind"
	// VolumeStrategyVolume uses a shared Docker named volume (suite volume).
	// Required for Docker Desktop on Windows / WSL2 where the workspace path
	// is on a 9P filesystem invisible to the Docker daemon.
	VolumeStrategyVolume = "volume"
)

// ResolveVolumeStrategy returns the effective volume strategy for workDir.
// When strategy is VolumeStrategyAuto (or empty) it checks whether GLUT is
// running inside a Docker container by looking for /.dockerenv, which Docker
// creates in every container it starts.
//
// Inside a container (devcontainer on Docker Desktop OR Docker-in-Docker in CI):
// the Docker daemon resolves bind-mount paths against the host or outer-daemon
// filesystem, not the inner container's filesystem. Named volumes are required.
//
// Outside a container (native Linux host): the daemon and GLUT share the same
// filesystem, so a plain bind mount works and avoids all volume overhead.
func ResolveVolumeStrategy(strategy string) string {
	if strategy != VolumeStrategyAuto && strategy != "" {
		return strategy
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return VolumeStrategyVolume // inside a container — named volumes required
	}
	return VolumeStrategyBind
}

// PruneOrphanedVolumes removes dangling Docker volumes whose names match
// GLUT ("glut-*") or GCL ("gcl-*") naming conventions. These volumes
// accumulate from test suite runs that were interrupted or from cleanup
// failures in earlier versions. Removing them before the first Docker test
// reduces the daemon's background cleanup backlog.
//
// Errors are intentionally ignored: pruning is best-effort and the suite
// must not fail because of it.
func PruneOrphanedVolumes() {
	for _, prefix := range []string{"glut-", "gcl-"} {
		out, err := exec.Command("docker", "volume", "ls",
			"-q", "--filter", "name="+prefix).Output()
		if err != nil {
			return
		}
		for _, id := range strings.Fields(string(out)) {
			_ = exec.Command("docker", "volume", "rm", id).Run()
		}
	}
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
