package mockserver

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
)

// initServedRepo creates a git repo with one commit and returns its path and
// HEAD sha.
func initServedRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		base := []string{
			"-c", "user.email=test@example.com",
			"-c", "user.name=Test",
			"-c", "commit.gpgsign=false",
			"-c", "init.defaultBranch=main",
		}
		cmd := exec.Command("git", append(base, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init")
	run("commit", "--allow-empty", "-m", "init")
	return dir, run("rev-parse", "HEAD")
}

func newGitHTTPServer(t *testing.T, repoPath string) *httptest.Server {
	t.Helper()
	s, err := New(config.APISetupConfig{})
	if err != nil {
		t.Fatal(err)
	}
	s.SetGitRepo(repoPath)
	ts := httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(ts.Close)
	return ts
}

// TestGitHTTPRequestsAreNotRecorded guards against the recorder middleware
// buffering git smart HTTP bodies (potentially large packfiles) fully in
// memory and retaining them for the whole run: git requests must bypass
// record() entirely.
func TestGitHTTPRequestsAreNotRecorded(t *testing.T) {
	repo, _ := initServedRepo(t)
	s, err := New(config.APISetupConfig{})
	if err != nil {
		t.Fatal(err)
	}
	s.SetGitRepo(repo)
	ts := httptest.NewServer(http.HandlerFunc(s.record(s.handle)))
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/test-group/test-project.git/info/refs?service=git-upload-pack", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("gitlab-ci-token", config.MockJobToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if calls := s.Recorder().Calls(); len(calls) != 0 {
		t.Fatalf("expected git HTTP request not to be recorded, got %#v", calls)
	}
}

func TestGitSmartHTTP(t *testing.T) {
	repo, sha := initServedRepo(t)
	ts := newGitHTTPServer(t, repo)
	refsURL := ts.URL + "/test-group/test-project.git/info/refs?service=git-upload-pack"

	t.Run("info/refs requires auth", func(t *testing.T) {
		resp, err := http.Get(refsURL)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("info/refs advertises HEAD", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, refsURL, nil)
		req.SetBasicAuth("gitlab-ci-token", config.MockJobToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(resp.Body); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, body %q", resp.StatusCode, buf.String())
		}
		if !strings.HasPrefix(buf.String(), "001e# service=git-upload-pack\n") {
			t.Fatalf("missing service header in %q", buf.String()[:40])
		}
		if !strings.Contains(buf.String(), sha) {
			t.Fatalf("advertisement does not contain HEAD sha %s", sha)
		}
	})

	t.Run("upload-pack accepts gzip request body", func(t *testing.T) {
		// Minimal fetch negotiation: want HEAD, flush, done.
		want := fmt.Sprintf("want %s\n", sha)
		var plain bytes.Buffer
		fmt.Fprintf(&plain, "%04x%s", len(want)+4, want)
		plain.WriteString("0000")
		done := "done\n"
		fmt.Fprintf(&plain, "%04x%s", len(done)+4, done)

		var gzipped bytes.Buffer
		gz := gzip.NewWriter(&gzipped)
		if _, err := gz.Write(plain.Bytes()); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}

		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/test-group/test-project.git/git-upload-pack", &gzipped)
		req.SetBasicAuth("gitlab-ci-token", config.MockJobToken)
		req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
		req.Header.Set("Content-Encoding", "gzip")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(resp.Body); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		// upload-pack must have produced a NAK followed by pack data; with a
		// broken (still-gzipped) stdin it produces nothing.
		if !strings.Contains(buf.String(), "NAK") || !strings.Contains(buf.String(), "PACK") {
			t.Fatalf("response does not look like a packfile result: %q", buf.String()[:min(80, buf.Len())])
		}
	})
}

// TestGitInfoRefsFailureIncludesStderr guards against a bare "git command
// failed" 500 that discards the actual git error, which used to make a
// broken repo path or a corrupt repo undiagnosable from the client side.
func TestGitInfoRefsFailureIncludesStderr(t *testing.T) {
	// An empty directory is not a git repository, so `git upload-pack
	// --advertise-refs` fails with a real stderr message.
	notARepo := t.TempDir()
	ts := newGitHTTPServer(t, notARepo)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/test-group/test-project.git/info/refs?service=git-upload-pack", nil)
	req.SetBasicAuth("gitlab-ci-token", config.MockJobToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body %q", resp.StatusCode, buf.String())
	}
	if !strings.Contains(strings.ToLower(buf.String()), "does not appear to be a git repository") {
		t.Fatalf("expected the response body to include git's stderr, got %q", buf.String())
	}
}

// TestGitPackFailureIsLogged guards against a failed upload-pack/receive-pack
// being silently swallowed: since the HTTP response is already streaming by
// the time git exits, the only way to surface a mutating push/fetch failure
// is a server-side log line.
func TestGitPackFailureIsLogged(t *testing.T) {
	notARepo := t.TempDir()
	ts := newGitHTTPServer(t, notARepo)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	oldStderr := os.Stderr
	os.Stderr = w

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/test-group/test-project.git/git-upload-pack", strings.NewReader("0000"))
	req.SetBasicAuth("gitlab-ci-token", config.MockJobToken)
	req.Header.Set("Content-Type", "application/x-git-upload-pack-request")
	resp, err := http.DefaultClient.Do(req)

	os.Stderr = oldStderr
	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("failed to close pipe writer: %v", closeErr)
	}
	logged, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("failed to read captured stderr: %v", readErr)
	}

	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if !strings.Contains(string(logged), "upload-pack") || !strings.Contains(strings.ToLower(string(logged)), "does not appear to be a git repository") {
		t.Fatalf("expected the pack failure to be logged with git's stderr, got: %q", logged)
	}
}

// TestGitSmartHTTPPush exercises git-receive-pack — the mutating path that
// upload-pack/fetch tests never cover — with a real `git push` against the
// mock's HTTP server, verifying the new ref actually lands in the served
// repo. Pushes to a fresh branch (not "main", which is checked out) to avoid
// git's receive.denyCurrentBranch default.
func TestGitSmartHTTPPush(t *testing.T) {
	repo, _ := initServedRepo(t)
	ts := newGitHTTPServer(t, repo)

	clientDir := t.TempDir()
	runGit := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	runGit(clientDir, "clone", repo, ".")
	runGit(clientDir, "checkout", "-b", "feature/push-test")
	if err := os.WriteFile(filepath.Join(clientDir, "pushed.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(clientDir, "add", "pushed.txt")
	runGit(clientDir, "-c", "commit.gpgsign=false", "commit", "-m", "add pushed.txt")
	pushedSHA := runGit(clientDir, "rev-parse", "HEAD")

	pushURL := strings.Replace(ts.URL, "http://", fmt.Sprintf("http://gitlab-ci-token:%s@", config.MockJobToken), 1) +
		"/test-group/test-project.git"
	runGit(clientDir, "push", pushURL, "feature/push-test:refs/heads/feature/push-test")

	// Verify directly against the served repo, not through the mock: this
	// confirms the push actually mutated it, not just that git exited 0.
	gotSHA := runGit(repo, "rev-parse", "feature/push-test")
	if gotSHA != pushedSHA {
		t.Fatalf("served repo's feature/push-test = %s, want %s", gotSHA, pushedSHA)
	}
}
