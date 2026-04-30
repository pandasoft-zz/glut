package parser

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

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
		return nil, errors.New("file does not contain glut: key")
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

// ParseDir recursively finds all *.yml and *.yaml files and parses them.
func ParseDir(dirPath string) ([]*TestFile, []error) {
	var files []*TestFile
	var errs []error

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		
		ext := filepath.Ext(path)
		if !info.IsDir() && (ext == ".yml" || ext == ".yaml") {
			tf, parseErr := Parse(path)
			if parseErr != nil {
				if parseErr.Error() != "file does not contain glut: key" {
					errs = append(errs, fmt.Errorf("%s: %w", path, parseErr))
				}
			} else {
				files = append(files, tf)
			}
		}
		return nil
	})

	if err != nil {
		errs = append(errs, err)
	}

	return files, errs
}

// Lint runs static analysis on a GLUT test file.
func Lint(filePath string) []LintError {
	var lints []LintError

	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return []LintError{{File: filePath, Level: LevelError, Message: fmt.Sprintf("cannot read file: %v", err)}}
	}

	var root map[string]interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return []LintError{{File: filePath, Level: LevelError, Message: fmt.Sprintf("invalid yaml: %v", err)}}
	}

	glutVal, ok := root["glut"]
	if !ok {
		return nil // Not a glut file
	}

	glutMap, ok := glutVal.(map[string]interface{})
	if !ok {
		return []LintError{{File: filePath, Level: LevelError, Message: "glut: section is not a map"}}
	}

	// Unknown keys
	for k := range glutMap {
		if k != "name" && k != "setup" && k != "assert" {
			lints = append(lints, LintError{File: filePath, Level: LevelError, Message: fmt.Sprintf("unknown key in glut: section: %s", k)})
		}
	}

	// Missing name
	name, hasName := glutMap["name"]
	if !hasName || name == "" {
		lints = append(lints, LintError{File: filePath, Level: LevelWarning, Message: "missing glut.name"})
	}

	// Empty assert
	assertVal, hasAssert := glutMap["assert"]
	if !hasAssert {
		lints = append(lints, LintError{File: filePath, Level: LevelWarning, Message: "glut.assert is empty"})
	} else if assertMap, ok := assertVal.(map[string]interface{}); ok && len(assertMap) == 0 {
		lints = append(lints, LintError{File: filePath, Level: LevelWarning, Message: "glut.assert is empty"})
	}

	// Stages
	stagesVal, hasStages := root["stages"]
	var stages []string
	if hasStages {
		if stageList, ok := stagesVal.([]interface{}); ok {
			for _, s := range stageList {
				if str, ok := s.(string); ok {
					stages = append(stages, str)
				}
			}
		}
	}

	// assert.job references non-existent job
	if hasAssert {
		if assertMap, ok := assertVal.(map[string]interface{}); ok {
			if jobVal, ok := assertMap["job"]; ok {
				if jobMap, ok := jobVal.(map[string]interface{}); ok {
					for jobName := range jobMap {
						if _, jobExists := root[jobName]; !jobExists {
							lints = append(lints, LintError{File: filePath, Level: LevelError, Message: fmt.Sprintf("assert.job references non-existent job '%s'", jobName)})
						}
					}
				}
			}
		}
	}

	// Stage missing in stages block
	for k, v := range root {
		// Ignore common top-level keywords
		if k == "glut" || k == "stages" || k == "variables" || k == "image" || k == "before_script" || k == "after_script" || k == "cache" || k == "services" || k == "workflow" || k == "include" || k == "default" || k == "pages" {
			continue
		}
		if jobMap, ok := v.(map[string]interface{}); ok {
			if stageVal, hasStage := jobMap["stage"]; hasStage {
				if stageStr, ok := stageVal.(string); ok && hasStages {
					found := false
					for _, s := range stages {
						if s == stageStr {
							found = true
							break
						}
					}
					if !found {
						lints = append(lints, LintError{File: filePath, Level: LevelError, Message: fmt.Sprintf("job '%s' has stage '%s' which is not in stages block", k, stageStr)})
					}
				}
			}
		}
	}

	// Setup checks
	if setupVal, ok := glutMap["setup"]; ok {
		if setupMap, ok := setupVal.(map[string]interface{}); ok {
			_, hasTag := setupMap["tag"]
			_, hasBranch := setupMap["branch"]
			if hasTag && hasBranch {
				lints = append(lints, LintError{File: filePath, Level: LevelError, Message: "setup.tag and setup.branch are mutually exclusive"})
			}

			if source, ok := setupMap["pipeline_source"].(string); ok && source == "merge_request_event" {
				if _, hasMR := setupMap["merge_request"]; !hasMR {
					lints = append(lints, LintError{File: filePath, Level: LevelError, Message: "setup.pipeline_source is merge_request_event but setup.merge_request is missing"})
				}
			}
		}
	}

	return lints
}
