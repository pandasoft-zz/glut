package parser

import (
	"bytes"
	"fmt"
	"io/ioutil"

	"gopkg.in/yaml.v3"
)

// Parse reads a YAML file, extracts the glut: section, and returns the TestFile.
func Parse(filePath string) (*TestFile, error) {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("failed to parse yaml: %w", err)
	}

	root := documentRoot(&document)
	if root == nil || root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("yaml root must be a mapping")
	}

	glutNode, ok := removeTopLevelKey(root, "glut")
	if !ok {
		return nil, errMissingGlut
	}

	var glutSection GlutSection
	if err := glutNode.Decode(&glutSection); err != nil {
		return nil, fmt.Errorf("failed to parse glut section: %w", err)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&document); err != nil {
		return nil, fmt.Errorf("failed to encode pipeline yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("failed to close pipeline yaml encoder: %w", err)
	}

	return &TestFile{
		FilePath:     filePath,
		Glut:         glutSection,
		PipelineYAML: buf.String(),
	}, nil
}

func documentRoot(document *yaml.Node) *yaml.Node {
	if document.Kind == yaml.DocumentNode && len(document.Content) > 0 {
		return document.Content[0]
	}
	return document
}

func removeTopLevelKey(root *yaml.Node, key string) (*yaml.Node, bool) {
	for i := 0; i < len(root.Content)-1; i += 2 {
		keyNode := root.Content[i]
		valueNode := root.Content[i+1]
		if keyNode.Value == key {
			root.Content = append(root.Content[:i], root.Content[i+2:]...)
			return valueNode, true
		}
	}
	return nil, false
}
