package asserter

import (
	"os"
	"path/filepath"
	"runtime"
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
