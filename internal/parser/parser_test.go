package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createTempYAML(t *testing.T, content string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "parser-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatalf("failed to remove temp dir: %v", err)
		}
	})
	path := filepath.Join(dir, "test.yml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParse_Minimal(t *testing.T) {
	content := `
glut:
  name: "minimal"
stages:
  - test
`
	path := createTempYAML(t, content)
	tf, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tf.Glut.Name != "minimal" {
		t.Errorf("expected name 'minimal', got %s", tf.Glut.Name)
	}

	if strings.Contains(tf.PipelineYAML, "glut:") {
		t.Errorf("PipelineYAML should not contain glut key")
	}
	if !strings.Contains(tf.PipelineYAML, "stages:") {
		t.Errorf("PipelineYAML should contain stages key")
	}
}

func TestParse_PreservesPipelineYAMLFeatures(t *testing.T) {
	content := `
glut:
  name: "anchors"
variables: &vars
  IMAGE: alpine
test_job:
  variables: *vars
  script:
    - echo "$IMAGE"
`
	path := createTempYAML(t, content)
	tf, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(tf.PipelineYAML, "glut:") {
		t.Errorf("PipelineYAML should not contain glut key")
	}
	if !strings.Contains(tf.PipelineYAML, "&vars") {
		t.Errorf("PipelineYAML should preserve anchor")
	}
	if !strings.Contains(tf.PipelineYAML, "*vars") {
		t.Errorf("PipelineYAML should preserve alias")
	}
}

func TestParse_PushOnBranch(t *testing.T) {
	content := `
glut:
  name: "push"
  setup:
    branch: "main"
`
	path := createTempYAML(t, content)
	tf, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if tf.Glut.Setup.Branch != "main" {
		t.Errorf("expected branch main, got %s", tf.Glut.Setup.Branch)
	}
}

func TestParse_TagPush(t *testing.T) {
	content := `
glut:
  name: "tag push"
  setup:
    tag: "1.2.0"
`
	path := createTempYAML(t, content)
	tf, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if tf.Glut.Setup.Tag != "1.2.0" {
		t.Errorf("expected tag 1.2.0, got %s", tf.Glut.Setup.Tag)
	}
}

func TestParse_MergeRequest(t *testing.T) {
	content := `
glut:
  name: "mr"
  setup:
    pipeline_source: "merge_request_event"
    merge_request:
      title: "Fix bug"
      target_branch: "main"
      iid: 42
`
	path := createTempYAML(t, content)
	tf, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if tf.Glut.Setup.PipelineSource != "merge_request_event" {
		t.Errorf("expected source merge_request_event")
	}
	if tf.Glut.Setup.MergeRequest == nil {
		t.Fatal("expected merge_request config")
	}
	if tf.Glut.Setup.MergeRequest.IID != 42 {
		t.Errorf("expected IID 42, got %d", tf.Glut.Setup.MergeRequest.IID)
	}
}

func TestParse_GitOrigin(t *testing.T) {
	content := `
glut:
  name: "git"
  setup:
    git:
      origin:
        files:
          "test.txt": "hello"
        commands:
          - "echo test"
`
	path := createTempYAML(t, content)
	tf, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if tf.Glut.Setup.Git == nil || tf.Glut.Setup.Git.Origin == nil {
		t.Fatal("expected git origin")
	}
	if tf.Glut.Setup.Git.Origin.Files["test.txt"] != "hello" {
		t.Errorf("expected test.txt content to be hello")
	}
	if tf.Glut.Setup.Git.Origin.Commands[0] != "echo test" {
		t.Errorf("expected command echo test")
	}
}

func TestParse_MockBinaries(t *testing.T) {
	content := `
glut:
  name: "mocks"
  setup:
    mocks:
      binaries:
        kubectl:
          executable: "echo ok"
`
	path := createTempYAML(t, content)
	tf, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if tf.Glut.Setup.Mocks == nil {
		t.Fatal("expected mocks config")
	}
	if tf.Glut.Setup.Mocks.Binaries["kubectl"].Executable != "echo ok" {
		t.Errorf("expected kubectl executable echo ok")
	}
}

func TestParse_MockAPISeed(t *testing.T) {
	content := `
glut:
  name: "api"
  setup:
    api:
      seed:
        releases:
          - name: "v1.0"
`
	path := createTempYAML(t, content)
	tf, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if tf.Glut.Setup.API == nil || tf.Glut.Setup.API.Seed == nil {
		t.Fatal("expected api seed config")
	}
	if tf.Glut.Setup.API.Seed.Releases[0]["name"] != "v1.0" {
		t.Errorf("expected release name v1.0")
	}
}

func TestLint_Errors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		check   func([]LintError) bool
	}{
		{
			name: "unknown key",
			content: `
glut:
  name: "test"
  invalid_key: "value"
`,
			check: func(errs []LintError) bool {
				for _, err := range errs {
					if strings.Contains(err.Message, "additional") || strings.Contains(err.Message, "unknown key in glut:") {
						return true
					}
				}
				return false
			},
		},
		{
			name: "missing name",
			content: `
glut:
  setup: {}
`,
			check: func(errs []LintError) bool {
				for _, e := range errs {
					if e.Level == LevelWarning && strings.Contains(e.Message, "missing glut.name") {
						return true
					}
				}
				return false
			},
		},
		{
			name: "empty assert",
			content: `
glut:
  name: "test"
  assert: {}
`,
			check: func(errs []LintError) bool {
				for _, e := range errs {
					if e.Level == LevelWarning && strings.Contains(e.Message, "glut.assert is empty") {
						return true
					}
				}
				return false
			},
		},
		{
			name: "missing stage",
			content: `
glut:
  name: "test"
stages:
  - build
test_job:
  stage: test
`,
			check: func(errs []LintError) bool {
				for _, e := range errs {
					if e.Level == LevelError && strings.Contains(e.Message, "which is not in stages block") {
						return true
					}
				}
				return false
			},
		},
		{
			name: "assert non-existent job",
			content: `
glut:
  name: "test"
  assert:
    job:
      missing_job: {}
`,
			check: func(errs []LintError) bool {
				for _, e := range errs {
					if e.Level == LevelError && strings.Contains(e.Message, "references non-existent job") {
						return true
					}
				}
				return false
			},
		},
		{
			name: "tag and branch",
			content: `
glut:
  name: "test"
  setup:
    tag: "1.0"
    branch: "main"
`,
			check: func(errs []LintError) bool {
				for _, e := range errs {
					if e.Level == LevelError && strings.Contains(e.Message, "mutually exclusive") {
						return true
					}
				}
				return false
			},
		},
		{
			name: "mr event without mr",
			content: `
glut:
  name: "test"
  setup:
    pipeline_source: "merge_request_event"
`,
			check: func(errs []LintError) bool {
				for _, e := range errs {
					if e.Level == LevelError && strings.Contains(e.Message, "setup.merge_request is missing") {
						return true
					}
				}
				return false
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := createTempYAML(t, tt.content)
			errs := Lint(path)
			if !tt.check(errs) {
				t.Errorf("Lint did not return expected errors for %s. Got: %v", tt.name, errs)
			}
		})
	}
}
