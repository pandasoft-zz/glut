package docker

import "net"

// VolumeMounts returns the --volume mounts needed for Docker executor jobs.
//
// Named volume strategy: mounts the pre-populated Docker volume at workDir so
// the path is identical inside and outside the container. Required for Docker
// Desktop / WSL2 where host bind-mounts are invisible to the daemon.
//
// Bind mount strategy: mounts the host workspace directory at the same path.
// No volume creation is needed; the host filesystem is directly accessible
// to the daemon on native Linux Docker.
func VolumeMounts(useDocker bool, workDir string, volName string, strategy string) []string {
	if !useDocker {
		return nil
	}
	if strategy == VolumeStrategyBind {
		return []string{workDir + ":" + workDir}
	}
	if volName != "" {
		return []string{volName + ":" + workDir}
	}
	return nil
}

// ExtraHosts returns the --extra-host entries needed for Docker executor jobs.
// ip is the address of the GLUT process (mock server) reachable from inside containers.
// Two entries are injected:
//   - host.docker.internal — standard Docker Desktop alias; we set it explicitly so it
//     works on Linux too (where Docker Desktop is absent).
//   - glut-mock — GLUT's own stable hostname used in CI_API_V4_URL / CI_SERVER_URL,
//     isolated from any unintended side-effects of the host.docker.internal alias.
func ExtraHosts(useDocker bool, ip string) []string {
	if !useDocker {
		return nil
	}
	return []string{
		"host.docker.internal:" + ip,
		"glut-mock:" + ip,
	}
}

// OutboundIP returns the local IP address that would be used to reach an external
// host. In DinD (Docker-in-Docker via socket) this is the GLUT container's bridge
// IP, which is reachable from sibling job containers on the same bridge network.
func OutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer func() { _ = conn.Close() }()
		return conn.LocalAddr().(*net.UDPAddr).IP.String()
	}
	// Fallback: scan network interfaces for the first non-loopback IPv4 address.
	// host.docker.internal is intentionally avoided here: when GLUT runs as a
	// container that hostname resolves to the Docker Desktop gateway, not to the
	// GLUT container itself, making it unreachable from sibling job containers.
	ifaces, err := net.Interfaces()
	if err != nil {
		return "host.docker.internal"
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
				return ip.String()
			}
		}
	}
	return "host.docker.internal"
}
