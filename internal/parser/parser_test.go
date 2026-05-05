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

func testFile(pipeline string, glut string) string {
	return pipeline + "\n---\n.glut:\n" + indent(glut, "  ")
}

func indent(value string, prefix string) string {
	lines := strings.Split(strings.TrimPrefix(value, "\n"), "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

func TestParse_Minimal(t *testing.T) {
	content := testFile(`
stages:
  - test
`, `
name: "minimal"
`)
	path := createTempYAML(t, content)
	tf, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tf.Glut.Name != "minimal" {
		t.Errorf("expected name 'minimal', got %s", tf.Glut.Name)
	}
	if strings.Contains(tf.PipelineYAML, ".glut:") {
		t.Errorf("PipelineYAML should not contain .glut key")
	}
	if !strings.Contains(tf.PipelineYAML, "stages:") {
		t.Errorf("PipelineYAML should contain stages key")
	}
}

func TestParse_PreservesPipelineYAMLFeatures(t *testing.T) {
	content := testFile(`
variables: &vars
  IMAGE: alpine
test_job:
  variables: *vars
  script:
    - echo "$IMAGE"
`, `
name: "anchors"
`)
	path := createTempYAML(t, content)
	tf, err := Parse(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(tf.PipelineYAML, ".glut:") {
		t.Errorf("PipelineYAML should not contain .glut key")
	}
	if !strings.Contains(tf.PipelineYAML, "&vars") {
		t.Errorf("PipelineYAML should preserve anchor")
	}
	if !strings.Contains(tf.PipelineYAML, "*vars") {
		t.Errorf("PipelineYAML should preserve alias")
	}
}

func TestParse_SetupVariants(t *testing.T) {
	tests := []struct {
		name  string
		glut  string
		check func(*TestFile)
	}{
		{
			name: "branch",
			glut: `
name: "push"
setup:
  branch: "main"
`,
			check: func(tf *TestFile) {
				if tf.Glut.Setup.Branch != "main" {
					t.Errorf("expected branch main, got %s", tf.Glut.Setup.Branch)
				}
			},
		},
		{
			name: "tag",
			glut: `
name: "tag push"
setup:
  tag: "1.2.0"
`,
			check: func(tf *TestFile) {
				if tf.Glut.Setup.Tag != "1.2.0" {
					t.Errorf("expected tag 1.2.0, got %s", tf.Glut.Setup.Tag)
				}
			},
		},
		{
			name: "merge request",
			glut: `
name: "mr"
setup:
  pipeline_source: "merge_request_event"
  merge_request:
    title: "Fix bug"
    target_branch: "main"
    iid: 42
`,
			check: func(tf *TestFile) {
				if tf.Glut.Setup.PipelineSource != "merge_request_event" {
					t.Errorf("expected source merge_request_event")
				}
				if tf.Glut.Setup.MergeRequest == nil {
					t.Fatal("expected merge_request config")
				}
				if tf.Glut.Setup.MergeRequest.IID != 42 {
					t.Errorf("expected IID 42, got %d", tf.Glut.Setup.MergeRequest.IID)
				}
			},
		},
		{
			name: "git origin",
			glut: `
name: "git"
setup:
  git:
    origin:
      files:
        "test.txt": "hello"
      commands:
        - "echo test"
`,
			check: func(tf *TestFile) {
				if tf.Glut.Setup.Git == nil || tf.Glut.Setup.Git.Origin == nil {
					t.Fatal("expected git origin")
				}
				if tf.Glut.Setup.Git.Origin.Files["test.txt"] != "hello" {
					t.Errorf("expected test.txt content to be hello")
				}
				if tf.Glut.Setup.Git.Origin.Commands[0] != "echo test" {
					t.Errorf("expected command echo test")
				}
			},
		},
		{
			name: "mock binaries",
			glut: `
name: "mocks"
setup:
  mocks:
    binaries:
      kubectl:
        executable: "echo ok"
`,
			check: func(tf *TestFile) {
				if tf.Glut.Setup.Mocks == nil {
					t.Fatal("expected mocks config")
				}
				if tf.Glut.Setup.Mocks.Binaries["kubectl"].Executable != "echo ok" {
					t.Errorf("expected kubectl executable echo ok")
				}
			},
		},
		{
			name: "api seed",
			glut: `
name: "api"
setup:
  api:
    seed:
      releases:
        - name: "v1.0"
`,
			check: func(tf *TestFile) {
				if tf.Glut.Setup.API == nil || tf.Glut.Setup.API.Seed == nil {
					t.Fatal("expected api seed config")
				}
				if tf.Glut.Setup.API.Seed.Releases[0]["name"] != "v1.0" {
					t.Errorf("expected release name v1.0")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := createTempYAML(t, testFile("test_job:\n  script: echo ok\n", tt.glut))
			tf, err := Parse(path)
			if err != nil {
				t.Fatal(err)
			}
			tt.check(tf)
		})
	}
}

func TestParse_RequiresGlutMetadataDocument(t *testing.T) {
	path := createTempYAML(t, `
test_job:
  script: echo ok
`)
	_, err := Parse(path)
	if err == nil {
		t.Fatal("expected missing .glut error")
	}
}

func TestLint_Errors(t *testing.T) {
	tests := []struct {
		name     string
		pipeline string
		glut     string
		check    func([]LintError) bool
	}{
		{
			name:     "unknown key",
			pipeline: "test_job:\n  script: echo ok\n",
			glut: `
name: "test"
invalid_key: "value"
`,
			check: func(errs []LintError) bool {
				for _, err := range errs {
					if strings.Contains(err.Message, "additional") || strings.Contains(err.Message, "unknown key") {
						return true
					}
				}
				return false
			},
		},
		{
			name:     "missing name",
			pipeline: "test_job:\n  script: echo ok\n",
			glut: `
setup: {}
`,
			check: func(errs []LintError) bool {
				for _, e := range errs {
					if e.Level == LevelWarning && strings.Contains(e.Message, "missing .glut.name") {
						return true
					}
				}
				return false
			},
		},
		{
			name:     "empty assert",
			pipeline: "test_job:\n  script: echo ok\n",
			glut: `
name: "test"
assert: {}
`,
			check: func(errs []LintError) bool {
				for _, e := range errs {
					if e.Level == LevelWarning && strings.Contains(e.Message, ".glut.assert is empty") {
						return true
					}
				}
				return false
			},
		},
		{
			name: "missing stage",
			pipeline: `
stages:
  - build
test_job:
  stage: test
`,
			glut: `
name: "test"
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
			name:     "assert non-existent job",
			pipeline: "test_job:\n  script: echo ok\n",
			glut: `
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
			name:     "tag and branch",
			pipeline: "test_job:\n  script: echo ok\n",
			glut: `
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
			name:     "mr event without mr",
			pipeline: "test_job:\n  script: echo ok\n",
			glut: `
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
			path := createTempYAML(t, testFile(tt.pipeline, tt.glut))
			errs := Lint(path)
			if !tt.check(errs) {
				t.Errorf("Lint did not return expected errors for %s. Got: %v", tt.name, errs)
			}
		})
	}
}
