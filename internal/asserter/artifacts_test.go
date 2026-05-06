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
