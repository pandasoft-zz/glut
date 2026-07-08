package parser

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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
	if !IsMissingGlut(err) {
		t.Fatalf("IsMissingGlut() = false for %v", err)
	}
	if IsMissingGlut(errors.New("other")) {
		t.Fatal("IsMissingGlut() should reject unrelated errors")
	}
}

func TestParseReportsReadFileError(t *testing.T) {
	_, err := Parse(filepath.Join(t.TempDir(), "missing.yml"))
	if err == nil {
		t.Fatal("expected read file error")
	}
}

func TestParse_ErrorCases(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "invalid yaml",
			content: `
test_job:
  script: [broken
---
.glut:
  name: bad
`,
			want: "failed to parse yaml",
		},
		{
			name: "too many documents",
			content: `
test_job:
  script: echo ok
---
.glut:
  name: bad
---
extra: true
`,
			want: "exactly two YAML documents",
		},
		{
			name: "glut not mapping",
			content: `
test_job:
  script: echo ok
---
.glut:
  - bad
`,
			want: "failed to parse .glut metadata",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := createTempYAML(t, tt.content)
			_, err := Parse(path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestSplitTestDocumentsAndDecodeHelpers(t *testing.T) {
	t.Run("split missing glut", func(t *testing.T) {
		_, _, err := splitTestDocuments([]byte("job:\n  script: echo ok\n"))
		if !errors.Is(err, errMissingGlut) {
			t.Fatalf("splitTestDocuments() error = %v, want errMissingGlut", err)
		}
	})

	t.Run("decode invalid yaml", func(t *testing.T) {
		_, err := decodeDocuments([]byte("job: [broken"))
		if err == nil {
			t.Fatal("expected decodeDocuments to fail")
		}
	})

	t.Run("decode reads multiple docs", func(t *testing.T) {
		docs, err := decodeDocuments([]byte("---\n---\nkey: value\n"))
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) != 2 {
			t.Fatalf("decodeDocuments() len = %d, want 2", len(docs))
		}
	})
}

func TestYAMLNodeHelpers(t *testing.T) {
	t.Run("documentRoot", func(t *testing.T) {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte("key: value\n"), &doc); err != nil {
			t.Fatal(err)
		}
		root := documentRoot(&doc)
		if root == nil || root.Kind != yaml.MappingNode {
			t.Fatalf("documentRoot() = %#v", root)
		}

		plain := &yaml.Node{Kind: yaml.ScalarNode, Value: "value"}
		if got := documentRoot(plain); got != plain {
			t.Fatal("documentRoot should return plain node unchanged")
		}
	})

	t.Run("topLevelValue", func(t *testing.T) {
		var doc yaml.Node
		if err := yaml.Unmarshal([]byte(".glut:\n  name: ok\n"), &doc); err != nil {
			t.Fatal(err)
		}
		value, ok := topLevelValue(documentRoot(&doc), ".glut")
		if !ok || value == nil {
			t.Fatal("expected topLevelValue to find .glut")
		}
		if _, ok := topLevelValue(nil, ".glut"); ok {
			t.Fatal("nil root should not match")
		}
		if _, ok := topLevelValue(&yaml.Node{Kind: yaml.SequenceNode}, ".glut"); ok {
			t.Fatal("non-mapping root should not match")
		}
	})

	t.Run("nodeToMap", func(t *testing.T) {
		if _, err := nodeToMap(nil); err == nil {
			t.Fatal("expected nodeToMap(nil) to fail")
		}

		var doc yaml.Node
		if err := yaml.Unmarshal([]byte("name: ok\n"), &doc); err != nil {
			t.Fatal(err)
		}
		mapped, err := nodeToMap(documentRoot(&doc))
		if err != nil {
			t.Fatal(err)
		}
		if mapped["name"] != "ok" {
			t.Fatalf("nodeToMap() = %#v", mapped)
		}
	})
}

func TestParseDir(t *testing.T) {
	root := t.TempDir()
	validDir := filepath.Join(root, "valid")
	badDir := filepath.Join(root, "bad")
	txtDir := filepath.Join(root, "txt")
	if err := os.MkdirAll(validDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(txtDir, 0755); err != nil {
		t.Fatal(err)
	}

	validPath := filepath.Join(validDir, "ok.yml")
	if err := os.WriteFile(validPath, []byte(testFile("job:\n  script: echo ok\n", "\nname: ok\n")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "bad.yaml"), []byte("job: [broken"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(txtDir, "skip.txt"), []byte("ignore"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "noglut.yml"), []byte("job:\n  script: echo ok\n"), 0644); err != nil {
		t.Fatal(err)
	}

	files, errs := ParseDir(root)
	if len(files) != 1 {
		t.Fatalf("ParseDir() files len = %d, want 1", len(files))
	}
	if len(errs) != 1 {
		t.Fatalf("ParseDir() errs len = %d, want 1", len(errs))
	}
	if !strings.Contains(errs[0].Error(), "failed to parse yaml") {
		t.Fatalf("ParseDir() error = %v", errs[0])
	}
}

// TestParseDirSkipsGitAndGlutTmpDirs guards against lint descending into
// .git or GLUT's own stale .glut-tmp* workspace copies, which would
// otherwise produce phantom results.
func TestParseDirSkipsGitAndGlutTmpDirs(t *testing.T) {
	root := t.TempDir()
	skippedDirs := []string{".git", ".glut-tmp-abc123"}
	for _, name := range skippedDirs {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		content := testFile("job:\n  script: echo ok\n", "\nname: phantom\n")
		if err := os.WriteFile(filepath.Join(dir, "phantom.yml"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	realPath := filepath.Join(root, "real.yml")
	if err := os.WriteFile(realPath, []byte(testFile("job:\n  script: echo ok\n", "\nname: real\n")), 0644); err != nil {
		t.Fatal(err)
	}

	files, errs := ParseDir(root)
	if len(errs) != 0 {
		t.Fatalf("ParseDir() errs = %v, want none", errs)
	}
	if len(files) != 1 || files[0].FilePath != realPath {
		t.Fatalf("ParseDir() files = %#v, want only %q", files, realPath)
	}
}

func TestSkipDiscoveryDir(t *testing.T) {
	cases := map[string]bool{
		".git":          true,
		".glut-tmp-abc": true,
		".glut-tmp":     true,
		"tests":         false,
		".hidden":       false,
	}
	for name, want := range cases {
		if got := SkipDiscoveryDir(name); got != want {
			t.Errorf("SkipDiscoveryDir(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestLint_HelperBranches(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		errs := Lint(filepath.Join(t.TempDir(), "missing.yml"))
		if len(errs) != 1 || errs[0].Level != LevelError {
			t.Fatalf("Lint missing file = %+v", errs)
		}
	})

	t.Run("invalid pipeline yaml", func(t *testing.T) {
		path := createTempYAML(t, `
- invalid
---
.glut:
  name: bad
`)
		errs := Lint(path)
		found := false
		for _, err := range errs {
			if strings.Contains(err.Message, "invalid pipeline yaml") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected invalid pipeline yaml error, got %+v", errs)
		}
	})

	t.Run("glut metadata not map", func(t *testing.T) {
		path := createTempYAML(t, `
job:
  script: echo ok
---
.glut:
  - bad
`)
		errs := Lint(path)
		found := false
		for _, err := range errs {
			if strings.Contains(err.Message, ".glut metadata is not a map") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected .glut metadata map error, got %+v", errs)
		}
	})

	t.Run("assert not map and stages not array", func(t *testing.T) {
		path := createTempYAML(t, testFile(`
stages: build
job:
  stage: build
  script: echo ok
`, `
name: "test"
assert: "oops"
`))
		errs := Lint(path)
		for _, err := range errs {
			if strings.Contains(err.Message, ".glut.assert is empty") {
				t.Fatal("string assert should not be treated as empty map")
			}
		}
	})

	t.Run("setup not map", func(t *testing.T) {
		path := createTempYAML(t, testFile(`
job:
  script: echo ok
`, `
name: "test"
setup: "oops"
`))
		errs := Lint(path)
		if len(errs) != 0 {
			for _, err := range errs {
				if strings.Contains(err.Message, "setup.") {
					t.Fatalf("unexpected setup lint for non-map setup: %+v", errs)
				}
			}
		}
	})
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
		{
			name:     "assert.job references missing pipeline job",
			pipeline: "test_job:\n  script: echo ok\n",
			glut: `
name: "test"
assert:
  job:
    test-jbo:
      exit-status: 0
`,
			check: func(errs []LintError) bool {
				for _, e := range errs {
					if e.Level == LevelError && strings.Contains(e.Message, "assert.job.test-jbo") && strings.Contains(e.Message, "not defined in the pipeline") {
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

func TestLintSchemaErrorIncludesLineNumber(t *testing.T) {
	glutSection := `
name: "bad source"
setup:
  pipeline_source: "manual"
`
	pipeline := "test_job:\n  script: echo ok\n"
	content := testFile(pipeline, glutSection)
	path := createTempYAML(t, content)

	idx := strings.Index(content, "pipeline_source")
	if idx < 0 {
		t.Fatalf("test fixture missing pipeline_source: %q", content)
	}
	wantLine := 1 + strings.Count(content[:idx], "\n")

	checkLine := func(t *testing.T, lints []LintError, via string) {
		t.Helper()
		var found bool
		for _, l := range lints {
			if l.Level == LevelError && strings.Contains(l.Message, "glut schema:") && strings.Contains(l.Message, "pipeline_source") {
				found = true
				if l.Line != wantLine {
					t.Errorf("%s schema error line = %d, want %d (message: %s)", via, l.Line, wantLine, l.Message)
				}
			}
		}
		if !found {
			t.Fatalf("%s: expected a schema error for pipeline_source, got %#v", via, lints)
		}
	}

	checkLine(t, Lint(path), "Lint()")

	tf, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	checkLine(t, LintParsed(tf), "LintParsed()")
}

func TestLintNoGlutDocumentReturnsNoLints(t *testing.T) {
	t.Run("single document", func(t *testing.T) {
		path := createTempYAML(t, "test_job:\n  script: echo ok\n")
		if errs := Lint(path); len(errs) != 0 {
			t.Fatalf("Lint() = %v, want no lints for a file without a .glut document", errs)
		}
	})

	t.Run("second document without .glut key", func(t *testing.T) {
		path := createTempYAML(t, "test_job:\n  script: echo ok\n---\nother: true\n")
		if errs := Lint(path); len(errs) != 0 {
			t.Fatalf("Lint() = %v, want no lints when the second document has no .glut key", errs)
		}
	})
}

func TestLintParsedMatchesLint(t *testing.T) {
	path := createTempYAML(t, testFile("test_job:\n  script: echo ok\n", `
name: "test"
assert:
  job:
    test-jbo:
      exit-status: 0
`))
	want := Lint(path)
	tf, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	got := LintParsed(tf)
	if len(got) != len(want) {
		t.Fatalf("LintParsed() returned %d lints, Lint() returned %d\ngot:  %+v\nwant: %+v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("lint[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestLintParsedDoesNotReReadFromDisk(t *testing.T) {
	path := createTempYAML(t, testFile("test_job:\n  script: echo ok\n", `
name: "test"
assert:
  job:
    test_job:
      exit-status: 0
`))
	tf, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("failed to remove file: %v", err)
	}
	// A clean, valid test file should produce zero lints. If LintParsed
	// re-read filePath from disk instead of using the in-memory TestFile,
	// it would fail with "cannot read file" here since the file is gone.
	if errs := LintParsed(tf); len(errs) != 0 {
		t.Errorf("LintParsed() after removing the source file = %v, want no lints (LintParsed must not touch disk)", errs)
	}
}

func TestLintTagAndBranchConflictReportedOnce(t *testing.T) {
	// The schema used to also reject tag+branch via a "not: {required: [...]}"
	// block, so a single mistake produced both a cryptic schema error and
	// this clear semantic one. Only the semantic rule should fire now.
	path := createTempYAML(t, testFile("test_job:\n  script: echo ok\n", `
name: "test"
setup:
  tag: "1.0"
  branch: "main"
`))
	lints := Lint(path)
	var errorLints int
	var sawMutuallyExclusive bool
	for _, e := range lints {
		if e.Level != LevelError {
			continue
		}
		errorLints++
		if strings.Contains(e.Message, "mutually exclusive") {
			sawMutuallyExclusive = true
		}
	}
	if errorLints != 1 || !sawMutuallyExclusive {
		t.Fatalf("Lint() reported %d error-level lints (want exactly 1, the semantic mutually-exclusive rule): %#v", errorLints, lints)
	}
}

func TestSemanticLintValidatesGlutMetadata(t *testing.T) {
	lints := SemanticLint("test.yml", map[string]interface{}{
		"setup": map[string]interface{}{
			"tag":    "1.0",
			"branch": "main",
		},
	})
	found := false
	for _, l := range lints {
		if l.Level == LevelError && strings.Contains(l.Message, "mutually exclusive") {
			found = true
		}
	}
	if !found {
		t.Fatalf("SemanticLint() did not report setup conflict: %#v", lints)
	}
}

func TestSemanticLintRejectsWhenWithPresentFalse(t *testing.T) {
	lints := SemanticLint("test.yml", map[string]interface{}{
		"assert": map[string]interface{}{
			"job": map[string]interface{}{
				"release:job": map[string]interface{}{
					"present": false,
					"when":    "manual",
				},
			},
		},
	})
	found := false
	for _, l := range lints {
		if l.Level == LevelError && strings.Contains(l.Message, `"when" cannot be combined with "present: false"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("SemanticLint() did not report when/present conflict: %#v", lints)
	}

	// present: true + when, and when alone, are both valid.
	for _, jobAssert := range []map[string]interface{}{
		{"present": true, "when": "manual"},
		{"when": "manual"},
	} {
		lints := SemanticLint("test.yml", map[string]interface{}{
			"assert": map[string]interface{}{
				"job": map[string]interface{}{"release:job": jobAssert},
			},
		})
		for _, l := range lints {
			if l.Level == LevelError {
				t.Fatalf("SemanticLint() unexpected error for %#v: %#v", jobAssert, lints)
			}
		}
	}
}

func TestLint_NoDynamicPipelineFalsePositives(t *testing.T) {
	tests := []struct {
		name     string
		pipeline string
		glut     string
	}{
		{
			name: "input interpolation job name with default",
			pipeline: `
spec:
  inputs:
    job_name:
      default: "build:container"

$[[ inputs.job_name ]]:
  script:
    - docker build .
`,
			glut: `
name: "component input job"
assert:
  job:
    build:container: {}
`,
		},
		{
			name: "remote component via include",
			pipeline: `
include:
  - component: my-group/my-component@1.0
    inputs:
      job_name: build:container
`,
			glut: `
name: "remote component"
assert:
  job:
    build:container: {}
`,
		},
		{
			name:     "present false asserts a job is absent even if never defined",
			pipeline: "test_job:\n  script: echo ok\n",
			glut: `
name: "absent job"
assert:
  job:
    removed-job:
      present: false
`,
		},
		{
			name:     "job defined in pipeline is a valid reference",
			pipeline: "test_job:\n  script: echo ok\n",
			glut: `
name: "existing job"
assert:
  job:
    test_job:
      exit-status: 0
`,
		},
		{
			name:     "pages is a real job name, not a reserved keyword",
			pipeline: "pages:\n  script: echo ok\n",
			glut: `
name: "pages job"
assert:
  job:
    pages:
      exit-status: 0
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := createTempYAML(t, testFile(tt.pipeline, tt.glut))
			errs := Lint(path)
			for _, e := range errs {
				if e.Level == LevelError {
					t.Errorf("Lint() reported unexpected error for dynamic pipeline: %s", e.Message)
				}
			}
		})
	}
}

func TestFriendlyYAMLMessage(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"cannot unmarshal !!map into string", "unexpected mapping where a string is expected"},
		{"cannot unmarshal !!seq into string", "unexpected sequence where a string is expected"},
		{"cannot unmarshal !!str into int", "unexpected string where int"},
		{"cannot unmarshal !!int into string", "unexpected integer where string"},
		{"cannot unmarshal !!bool into string", "unexpected boolean where string"},
		{"some unknown message", "some unknown message"},
	}
	for _, tt := range tests {
		got := friendlyYAMLMessage(tt.input)
		if !strings.Contains(got, tt.want) {
			t.Errorf("friendlyYAMLMessage(%q) = %q, want containing %q", tt.input, got, tt.want)
		}
	}
}

func TestFriendlyParseError(t *testing.T) {
	t.Run("non_type_error_returned_as_is", func(t *testing.T) {
		orig := errors.New("some error")
		got := friendlyParseError(orig)
		if got != orig { //nolint:errorlint // asserting reference identity (unchanged passthrough), not chain membership
			t.Errorf("expected original error, got %v", got)
		}
	})

	t.Run("yaml_type_error_is_friendly", func(t *testing.T) {
		// Trigger a real yaml.TypeError by unmarshaling wrong type
		type Wrapper struct {
			Name string `yaml:"name"`
		}
		var w Wrapper
		err := yaml.Unmarshal([]byte("name:\n  sub: value\n"), &w)
		if err == nil {
			t.Skip("yaml did not return type error")
		}
		got := friendlyParseError(err)
		if got == err { //nolint:errorlint // asserting reference identity (a new error was returned), not chain membership
			t.Errorf("expected friendly error, got original error")
		}
	})
}

func TestFileParseErrorUnwrap(t *testing.T) {
	inner := errors.New("inner error")
	fpe := &FileParseError{FilePath: "test.yml", Err: inner}
	if !errors.Is(fpe, inner) {
		t.Error("expected errors.Is to find inner error via Unwrap")
	}
}

func TestLintReportsSchemaAndSetupErrors(t *testing.T) {
	path := createTempYAML(t, testFile("test_job:\n  script: echo ok\n", `
name: "schema and setup"
setup:
  pipeline_source: "manual"
  tag: "1.0"
  branch: "main"
`))

	lints := Lint(path)
	var schemaErr bool
	var setupErr bool
	for _, lint := range lints {
		if lint.Level == LevelError && strings.Contains(lint.Message, "glut schema:") {
			schemaErr = true
		}
		if lint.Level == LevelError && strings.Contains(lint.Message, "mutually exclusive") {
			setupErr = true
		}
	}

	if !schemaErr || !setupErr {
		t.Fatalf("Lint() schema error = %v, setup error = %v, lints = %#v", schemaErr, setupErr, lints)
	}
}
