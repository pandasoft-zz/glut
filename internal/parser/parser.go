package parser

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// Parse reads a YAML file, extracts the second-document .glut section, and returns the TestFile.
func Parse(filePath string) (*TestFile, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	pipelineDoc, glutDoc, err := splitTestDocuments(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse yaml: %w", err)
	}

	glutNode, ok := topLevelValue(documentRoot(glutDoc), ".glut")
	if !ok {
		return nil, errMissingGlut
	}

	var glutSection GlutSection
	if err := glutNode.Decode(&glutSection); err != nil {
		return nil, fmt.Errorf("failed to parse .glut metadata: %w", err)
	}
	glutRaw, err := nodeToMap(glutNode)
	if err != nil {
		return nil, fmt.Errorf("failed to read .glut metadata map: %w", err)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(pipelineDoc); err != nil {
		return nil, fmt.Errorf("failed to encode pipeline yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("failed to close pipeline yaml encoder: %w", err)
	}

	return &TestFile{
		FilePath:     filePath,
		Glut:         glutSection,
		GlutRaw:      glutRaw,
		PipelineYAML: buf.String(),
	}, nil
}

func splitTestDocuments(data []byte) (*yaml.Node, *yaml.Node, error) {
	docs, err := decodeDocuments(data)
	if err != nil {
		return nil, nil, err
	}
	if len(docs) < 2 {
		return nil, nil, errMissingGlut
	}
	if len(docs) > 2 {
		return nil, nil, fmt.Errorf("test file must contain exactly two YAML documents")
	}
	return docs[0], docs[1], nil
}

func decodeDocuments(data []byte) ([]*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var docs []*yaml.Node
	for {
		var doc yaml.Node
		if err := decoder.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if len(doc.Content) == 0 {
			continue
		}
		docs = append(docs, &doc)
	}
	return docs, nil
}

func documentRoot(document *yaml.Node) *yaml.Node {
	if document.Kind == yaml.DocumentNode && len(document.Content) > 0 {
		return document.Content[0]
	}
	return document
}

func topLevelValue(root *yaml.Node, key string) (*yaml.Node, bool) {
	if root == nil || root.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i < len(root.Content)-1; i += 2 {
		keyNode := root.Content[i]
		valueNode := root.Content[i+1]
		if keyNode.Value == key {
			return valueNode, true
		}
	}
	return nil, false
}

func nodeToMap(node *yaml.Node) (map[string]interface{}, error) {
	var out map[string]interface{}
	if node == nil {
		return nil, fmt.Errorf("yaml node is nil")
	}
	if err := node.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
