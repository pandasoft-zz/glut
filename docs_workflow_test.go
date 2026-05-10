package glut_test

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDocsWorkflowYAMLParses(t *testing.T) {
	data, err := os.ReadFile(".github/workflows/docs.yml")
	if err != nil {
		t.Fatal(err)
	}

	var workflow map[string]any
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatalf("docs workflow yaml did not parse: %v", err)
	}
	if workflow["name"] != "Docs" {
		t.Fatalf("workflow name = %v, want Docs", workflow["name"])
	}
}
