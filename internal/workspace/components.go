package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ComponentFetch carries everything needed to resolve `include: component:`
// against a REAL GitLab over HTTPS in integration mode (setup.components.fetch:
// real), so a composite component is exercised with its real sub-components.
type ComponentFetch struct {
	// Namespace is the real CI_PROJECT_NAMESPACE to expose to gitlab-ci-local so
	// that component addresses like $CI_SERVER_FQDN/$CI_PROJECT_NAMESPACE/<name>
	// resolve to the real project path.
	Namespace string
	// ServerFQDN is the real CI_SERVER_FQDN ("host:port") to expose to
	// gitlab-ci-local. It matters for numeric / ~latest refs (e.g. @1): GCL
	// resolves those to a concrete tag with `git ls-remote --tags` against the
	// address domain (CI_SERVER_FQDN), not the gcl-origin remote — so this must
	// also point at the real server.
	ServerFQDN string
	// GCLOriginURL is a credential-free https URL added as the workspace's
	// `gcl-origin` remote. gitlab-ci-local derives the server it fetches
	// components from this remote, so it forces real HTTPS resolution.
	GCLOriginURL string
	// GitConfigEnv injects, via GIT_CONFIG_COUNT/KEY_n/VALUE_n, an insteadOf
	// rewrite that adds the real gitlab-ci-token credential to the component
	// clone URL. Kept in the environment (never on disk or in args) so the token
	// is not persisted or logged.
	GitConfigEnv map[string]string
}

// RealComponentFetch resolves the real GitLab coordinates from the host
// environment (CI_* as present inside a GitLab CI job, with optional
// GLUT_COMPONENTS_* overrides) and builds the gcl-origin remote URL plus the
// GIT_CONFIG_* credential rewrite. It returns an error when a required
// coordinate is missing (e.g. running outside CI without overrides).
func RealComponentFetch(hostEnv []string) (ComponentFetch, error) {
	if hostEnv == nil {
		hostEnv = os.Environ()
	}
	h := envSliceToMap(hostEnv)
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := h[k]; v != "" {
				return v
			}
		}
		return ""
	}

	host := first("GLUT_COMPONENTS_SERVER", "CI_SERVER_HOST")
	port := h["GLUT_COMPONENTS_PORT"]
	token := first("GLUT_COMPONENTS_TOKEN", "CI_JOB_TOKEN")
	namespace := first("GLUT_COMPONENTS_NAMESPACE", "CI_PROJECT_NAMESPACE")
	projectPath := first("GLUT_COMPONENTS_PROJECT", "CI_PROJECT_PATH")

	// GLUT_COMPONENTS_SERVER may be given as "host" or "host:port". Inside a
	// real CI job CI_SERVER_PORT is always set, so it must rank below an
	// explicit port here — otherwise GLUT_COMPONENTS_SERVER=host:8443 would
	// silently produce port 443 (or whatever CI_SERVER_PORT holds) URLs.
	// Only GLUT_COMPONENTS_PORT itself ranks higher.
	if hostOnly, p, ok := strings.Cut(host, ":"); ok {
		host = hostOnly
		if port == "" {
			port = p
		}
	}
	if port == "" {
		port = h["CI_SERVER_PORT"]
	}
	if port == "" {
		port = "443"
	}

	var missing []string
	if host == "" {
		missing = append(missing, "CI_SERVER_HOST (or GLUT_COMPONENTS_SERVER)")
	}
	if token == "" {
		missing = append(missing, "CI_JOB_TOKEN (or GLUT_COMPONENTS_TOKEN)")
	}
	if namespace == "" {
		missing = append(missing, "CI_PROJECT_NAMESPACE (or GLUT_COMPONENTS_NAMESPACE)")
	}
	if len(missing) > 0 {
		return ComponentFetch{}, fmt.Errorf(
			"real component fetch requires real GitLab coordinates from the environment (run inside GitLab CI, or set GLUT_COMPONENTS_*); missing: %s",
			strings.Join(missing, ", "))
	}

	// gitlab-ci-local parses the gcl-origin URL with a regex that needs at least
	// <group>/<project> after the host. CI_PROJECT_PATH provides that; fall back
	// to a synthetic project under the namespace. Only host/port/schema are used
	// for the component clone, so the exact project segment is irrelevant.
	if !strings.Contains(projectPath, "/") {
		projectPath = namespace + "/glut-component-host"
	}

	// gitlab-ci-local builds the component clone URL as
	// https://<host>:<port>/<projectPath>.git, so the insteadOf "original" prefix
	// must carry the explicit port to match.
	original := fmt.Sprintf("https://%s:%s/", host, port)
	withToken := fmt.Sprintf("https://gitlab-ci-token:%s@%s:%s/", token, host, port)

	return ComponentFetch{
		Namespace:    namespace,
		ServerFQDN:   fmt.Sprintf("%s:%s", host, port),
		GCLOriginURL: fmt.Sprintf("https://%s:%s/%s.git", host, port, projectPath),
		GitConfigEnv: map[string]string{
			"GIT_CONFIG_COUNT":   "1",
			"GIT_CONFIG_KEY_0":   "url." + withToken + ".insteadOf",
			"GIT_CONFIG_VALUE_0": original,
		},
	}, nil
}

// SetGCLOriginRemote adds (or refreshes) a `gcl-origin` remote on the workspace
// clone. gitlab-ci-local prefers this remote over `origin` when deriving the
// server it fetches `include: component:` from, so pointing it at a real GitLab
// HTTPS URL forces real component resolution. Only this extra remote is touched;
// `origin` and the bare sandbox repo are left intact.
func (w *Workspace) SetGCLOriginRemote(url string) error {
	dir := w.WorkspaceDir
	if dir == "" {
		dir = w.Dir
	}
	gitBin := resolveExecutable("git", w.hostEnv)

	// Drop a stale gcl-origin if present (ignore failure: it may not exist).
	rm := exec.Command(gitBin, "remote", "remove", "gcl-origin")
	rm.Dir = dir
	rm.Env = w.hostEnv
	_ = rm.Run()

	add := exec.Command(gitBin, "remote", "add", "gcl-origin", url)
	add.Dir = dir
	add.Env = w.hostEnv
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("add gcl-origin remote: %w, output: %s", err, string(out))
	}
	return nil
}
