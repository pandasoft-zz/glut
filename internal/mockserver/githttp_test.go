package mockserver

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
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
