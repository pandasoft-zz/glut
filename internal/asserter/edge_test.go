package asserter

import (
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/mockserver"
	"github.com/pandasoft-zz/glut/internal/mockwrapper"
)

func TestArtifactChecksumsAndMissingFileBehavior(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "checksums.txt")
	content := []byte("hello checksum\n")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	md5Sum := fmt.Sprintf("%x", md5.Sum(content))
	sha256Sum := fmt.Sprintf("%x", sha256.Sum256(content))

	results := Run(config.AssertConfig{
		Artifacts: map[string]config.ArtifactAssert{
			"checksums.txt": {
				MD5:    md5Sum,
				SHA256: sha256Sum,
				Size:   map[string]any{"ge": 1},
			},
			"missing.txt": {
				Exists: boolPtr(false),
			},
		},
	}, AssertContext{WorkspacePath: root})

	for _, result := range results {
		if !result.Passed {
			t.Fatalf("unexpected failure: %+v", result)
		}
	}
}

func TestMatcherFailureBranches(t *testing.T) {
	tests := []struct {
		name     string
		expected any
		actual   any
	}{
		{name: "invalid regexp", expected: map[string]any{"match-regexp": "("}, actual: "x"},
		{name: "invalid semver constraint", expected: map[string]any{"semver-constraint": "not-a-constraint"}, actual: "1.0.0"},
		{name: "missing gjson path", expected: map[string]any{"gjson": map[string]any{"missing": 1}}, actual: `{"present":1}`},
		{name: "or all fail", expected: map[string]any{"or": []any{"a", "b"}}, actual: "c"},
		{name: "unknown matcher", expected: map[string]any{"unknown": 1}, actual: 1},
		{name: "non-slice pattern input", expected: []any{1}, actual: "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var state matchState
			if tt.name == "non-slice pattern input" {
				state = matchTextPatterns(tt.expected, tt.actual.(string))
			} else {
				state = matchValue(tt.expected, tt.actual)
			}
			if state.Passed {
				t.Fatalf("expected failure, got %+v", state)
			}
		})
	}
}

func TestAPIHelpersAndBodyFailures(t *testing.T) {
	if method, path := splitEndpointPattern("GET /x"); method != "GET" || path != "/x" {
		t.Fatalf("splitEndpointPattern() = %q %q", method, path)
	}
	if method, path := splitEndpointPattern("BROKEN"); method != "BROKEN" || path != "" {
		t.Fatalf("splitEndpointPattern no path = %q %q", method, path)
	}
	if pathMatches("/a/*/c", "/a/b") {
		t.Fatal("path length mismatch should fail")
	}
	if parts := splitPath("/"); parts != nil {
		t.Fatalf("splitPath(root) = %#v", parts)
	}
	if apiBodyMatches([]byte("{bad json"), map[string]any{"name": "x"}) {
		t.Fatal("invalid JSON body should fail")
	}

	results := runAPIBodyAssert("assert.api.\"GET /x\".body", nil, map[string]any{"name": "x"})
	if len(results) != 1 || results[0].Passed {
		t.Fatalf("expected missing call failure, got %+v", results)
	}

	filtered := filterAPICalls([]mockserver.APICall{{Method: "GET", Path: "/a/b"}}, "POST /a/*")
	if len(filtered) != 0 {
		t.Fatalf("expected method mismatch to filter out calls, got %+v", filtered)
	}
}

func TestBinaryEdgeCases(t *testing.T) {
	results := Run(config.AssertConfig{
		Binary: map[string]config.BinaryAssert{
			"release-cli": {
				Called: boolPtr(false),
				Times:  0,
				Calls: []config.BinaryCallAssert{
					{Args: []any{"missing"}},
				},
			},
		},
	}, AssertContext{})

	foundFailure := false
	for _, result := range results {
		if !result.Passed && result.Path == "assert.binary.\"release-cli\".calls[0]" {
			foundFailure = true
		}
	}
	if !foundFailure {
		t.Fatalf("expected missing indexed call failure, got %+v", results)
	}

	if binaryCallMatches(config.BinaryCallAssert{Args: []any{"a"}}, mockwrapper.BinaryCall{Args: []string{"b"}}) {
		t.Fatal("binaryCallMatches should fail for mismatched args")
	}

	callResults := runSingleBinaryCallAssert("assert.binary.\"release-cli\".calls[0]", config.BinaryCallAssert{
		Args:  []any{"semantic-release"},
		CWD:   "/builds/project",
		Stdin: "payload",
	}, mockwrapper.BinaryCall{
		Args:  []string{"semantic-release"},
		CWD:   "/builds/project",
		Stdin: "payload",
	})
	for _, result := range callResults {
		if !result.Passed {
			t.Fatalf("expected binary call assert to pass, got %+v", result)
		}
	}
}

func TestGitEdgeCases(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	origin := filepath.Join(root, "origin.git")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}

	mustRunGit(t, source, "init")
	mustRunGit(t, source, "config", "user.email", "alice@example.com")
	mustRunGit(t, source, "config", "user.name", "Alice Example")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "config.txt"), []byte("value\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, source, "add", "nested/config.txt")
	mustRunGit(t, source, "commit", "-m", "feat: add nested file")
	mustRunGit(t, source, "init", "--bare", origin)
	mustRunGit(t, source, "remote", "add", "origin", origin)
	mustRunGit(t, source, "branch", "-M", "main")
	mustRunGit(t, source, "push", "-u", "origin", "main")
	mustRunGit(t, root, "--git-dir", origin, "symbolic-ref", "HEAD", "refs/heads/main")

	bareResults := runBareGitFileAssert("assert.git.origin.file.\"nested/config.txt\"", origin, "nested/config.txt", config.ArtifactAssert{
		Exists:   boolPtr(true),
		Contents: []any{"value"},
		Mode:     "100644",
		Filetype: "file",
	})
	for _, result := range bareResults {
		if !result.Passed {
			t.Fatalf("unexpected bare file failure: %+v", result)
		}
	}

	missingResults := runBareGitFileAssert("assert.git.origin.file.\"missing.txt\"", origin, "missing.txt", config.ArtifactAssert{
		Exists: boolPtr(false),
	})
	for _, result := range missingResults {
		if !result.Passed {
			t.Fatalf("expected missing bare file assert to pass, got %+v", result)
		}
	}

	escapeResults := runBareGitFileAssert("assert.git.origin.file.\"../bad\"", origin, "../bad", config.ArtifactAssert{
		Exists: boolPtr(true),
	})
	if len(escapeResults) != 1 || escapeResults[0].Passed {
		t.Fatalf("expected bare path escape failure, got %+v", escapeResults)
	}

	if _, err := gitCommitCount(root, false); err == nil {
		t.Fatal("expected gitCommitCount to fail on non-repo")
	}
}

func TestResultHelpersFailureBranch(t *testing.T) {
	result := resultFromState("assert.job.\"build\".exit-status", matchState{Expected: 0, Actual: 1})
	if result.Passed {
		t.Fatalf("expected failure result, got %+v", result)
	}
}
