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

	var root map[string]interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("failed to parse yaml: %w", err)
	}

	glutVal, ok := root["glut"]
	if !ok {
		return nil, errMissingGlut
	}

	glutBytes, err := yaml.Marshal(glutVal)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal glut section: %w", err)
	}

	var glutSection GlutSection
	if err := yaml.Unmarshal(glutBytes, &glutSection); err != nil {
		return nil, fmt.Errorf("failed to parse glut section: %w", err)
	}

	delete(root, "glut")

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, fmt.Errorf("failed to encode pipeline yaml: %w", err)
	}
	enc.Close()

	return &TestFile{
		FilePath:     filePath,
		Glut:         glutSection,
		PipelineYAML: buf.String(),
	}, nil
}
