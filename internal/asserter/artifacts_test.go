package asserter

import (
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
)

func TestRunArtifactAsserts(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "output.json")
	if err := os.WriteFile(path, []byte("{\"status\":\"success\",\"items\":[1,2]}"), 0644); err != nil {
		t.Fatal(err)
	}

	asserts := config.AssertConfig{
		Artifacts: map[string]config.ArtifactAssert{
			"output.json": {
				Exists:   boolPtr(true),
				Contents: map[string]any{"gjson": map[string]any{"status": "success", "items.#": map[string]any{"gt": 0}}},
				Mode:     "0644",
				Filetype: "file",
			},
		},
	}

	results := Run(asserts, AssertContext{WorkspacePath: root})
	for _, result := range results {
		if runtime.GOOS == "windows" && result.Path == "assert.artifacts.\"output.json\".mode" {
			continue
		}
		if !result.Passed {
			t.Fatalf("unexpected failure: %+v", result)
		}
	}
}

func TestRunArtifactAssertsRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	asserts := config.AssertConfig{
		Artifacts: map[string]config.ArtifactAssert{
			"../secret.txt": {
				Exists: boolPtr(true),
			},
		},
	}

	results := Run(asserts, AssertContext{WorkspacePath: root})
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Passed {
		t.Fatalf("expected path escape to fail, got %+v", results[0])
	}
}

func TestRunArtifactAssertsFileTypesAndChecksumFailures(t *testing.T) {
	root := t.TempDir()
	dirPath := filepath.Join(root, "dir")
	if err := os.Mkdir(dirPath, 0755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(root, "link")
	if err := os.Symlink("dir", linkPath); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation needs extra permissions on windows")
		}
		t.Fatal(err)
	}

	results := Run(config.AssertConfig{
		Artifacts: map[string]config.ArtifactAssert{
			"dir": {
				Exists:   boolPtr(true),
				Filetype: "directory",
			},
			"link": {
				Exists:   boolPtr(true),
				Filetype: "symlink",
			},
		},
	}, AssertContext{WorkspacePath: root})

	for _, result := range results {
		if !result.Passed {
			t.Fatalf("unexpected filetype failure: %+v", result)
		}
	}

	checksumResults := runArtifactAssert("assert.artifacts.\"dir\"", dirPath, config.ArtifactAssert{MD5: "bad"})
	foundFailure := false
	for _, result := range checksumResults {
		if !result.Passed && result.Path == "assert.artifacts.\"dir\".md5" {
			foundFailure = true
		}
	}
	if !foundFailure {
		t.Fatalf("expected checksum failure for directory, got %+v", checksumResults)
	}
}

// TestRunArtifactAssertChecksumsAreCaseInsensitive guards against an
// uppercase MD5/SHA256 digest pasted from another tool never matching
// checksumFile's lowercase %x output, with no hint why the assert failed.
func TestRunArtifactAssertChecksumsAreCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	md5Sum := fmt.Sprintf("%x", md5.Sum([]byte("hello")))
	sha256Sum := fmt.Sprintf("%x", sha256.Sum256([]byte("hello")))

	results := runArtifactAssert("assert.artifacts.\"file.txt\"", path, config.ArtifactAssert{
		MD5:    strings.ToUpper(md5Sum),
		SHA256: strings.ToUpper(sha256Sum),
	})
	for _, result := range results {
		if !result.Passed {
			t.Fatalf("uppercase digest should match: %+v", result)
		}
	}
}

// A content-dependent assertion (report/contents/checksum) on an artifact the
// job never produced must fail rather than pass vacuously by returning early on
// the missing file.
func TestRunArtifactAssertMissingFileFailsContentAsserts(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "never-produced.xml")

	cases := []struct {
		name   string
		assert config.ArtifactAssert
		path   string
	}{
		{"report", config.ArtifactAssert{Report: &config.ReportAssert{Format: "junit", Failures: 0}}, "art.report"},
		{"contents", config.ArtifactAssert{Contents: "hello"}, "art.contents"},
		{"mode", config.ArtifactAssert{Mode: "0644"}, "art.mode"},
		{"size", config.ArtifactAssert{Size: map[string]any{"ge": 5}}, "art.size"},
		{"filetype", config.ArtifactAssert{Filetype: "file"}, "art.filetype"},
		{"md5", config.ArtifactAssert{MD5: "abc"}, "art.md5"},
		{"sha256", config.ArtifactAssert{SHA256: "abc"}, "art.sha256"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results := runArtifactAssert("art", missing, tc.assert)
			if !anyFailed(results) {
				t.Fatalf("expected %s assertion on a missing file to fail, got %+v", tc.name, results)
			}
		})
	}

	// exists:false on a genuinely absent file must still pass cleanly.
	ok := runArtifactAssert("art", missing, config.ArtifactAssert{Exists: boolPtr(false)})
	if anyFailed(ok) {
		t.Fatalf("exists:false on a missing file should pass, got %+v", ok)
	}
}
