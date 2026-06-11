package mockserver

import (
	"compress/gzip"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/pandasoft-zz/glut/internal/config"
)

// serveGitHTTP handles git smart HTTP protocol requests for /<project>.git/*.
// Returns true if the request was handled, false if it should fall through to
// the API handler.
func (s *Server) serveGitHTTP(w http.ResponseWriter, r *http.Request) bool {
	s.gitMu.RLock()
	repoPath := s.gitRepoPath
	s.gitMu.RUnlock()

	if repoPath == "" {
		return false
	}

	projectPath := s.projectPathValue()
	prefix := "/" + projectPath + ".git"
	urlPath := r.URL.EscapedPath()
	if !strings.HasPrefix(urlPath, prefix+"/") {
		return false
	}
	rest := strings.TrimPrefix(urlPath, prefix)

	// Require Basic auth: gitlab-ci-token:<CI_JOB_TOKEN>
	user, pass, ok := r.BasicAuth()
	if !ok || user != "gitlab-ci-token" || pass != config.MockJobToken {
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
		return true
	}

	switch {
	case r.Method == http.MethodGet && rest == "/info/refs":
		s.serveGitInfoRefs(w, r, repoPath)
	case r.Method == http.MethodPost && rest == "/git-upload-pack":
		s.serveGitPack(w, r, repoPath, "upload-pack", "application/x-git-upload-pack-result")
	case r.Method == http.MethodPost && rest == "/git-receive-pack":
		s.serveGitPack(w, r, repoPath, "receive-pack", "application/x-git-receive-pack-result")
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
	return true
}

func (s *Server) serveGitInfoRefs(w http.ResponseWriter, r *http.Request, repoPath string) {
	service := r.URL.Query().Get("service")
	var gitSubCmd string
	var contentType string

	switch service {
	case "git-upload-pack":
		gitSubCmd = "upload-pack"
		contentType = "application/x-git-upload-pack-advertisement"
	case "git-receive-pack":
		gitSubCmd = "receive-pack"
		contentType = "application/x-git-receive-pack-advertisement"
	default:
		http.Error(w, "unsupported service", http.StatusForbidden)
		return
	}

	cmd := exec.CommandContext(r.Context(), "git", gitSubCmd, "--stateless-rpc", "--advertise-refs", repoPath)
	output, err := cmd.Output()
	if err != nil {
		http.Error(w, "git command failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")

	// Prepend the service discovery pkt-line, then a flush packet, then the
	// refs advertisement from git.
	header := fmt.Sprintf("# service=%s\n", service)
	pktLen := len(header) + 4 // 4-digit hex length field
	_, _ = fmt.Fprintf(w, "%04x%s", pktLen, header)
	_, _ = w.Write([]byte("0000"))
	_, _ = w.Write(output)
}

func (s *Server) serveGitPack(w http.ResponseWriter, r *http.Request, repoPath, subCmd, contentType string) {
	// Git clients gzip large request bodies (e.g. fetch negotiation over many
	// refs); net/http does not decompress request bodies, so do it here like
	// the real git-http-backend does.
	body := r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(body)
		if err != nil {
			http.Error(w, "bad gzip body", http.StatusBadRequest)
			return
		}
		defer func() { _ = gz.Close() }()
		body = gz
	}

	cmd := exec.CommandContext(r.Context(), "git", subCmd, "--stateless-rpc", repoPath)
	cmd.Stdin = body

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	cmd.Stdout = w
	_ = cmd.Run()
}
