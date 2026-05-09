package parser

import (
	"fmt"
	"os"

	glutschema "github.com/pandasoft-zz/glut/schema"
	"gopkg.in/yaml.v3"
)

var gitlabTopLevelKeywords = map[string]bool{
	"stages":        true,
	"variables":     true,
	"image":         true,
	"before_script": true,
	"after_script":  true,
	"cache":         true,
	"services":      true,
	"workflow":      true,
	"include":       true,
	"default":       true,
	"pages":         true,
}

// Lint runs static analysis on a GLUT test file.
func Lint(filePath string) []LintError {
	pipelineRoot, glutMap, lints := readLintInput(filePath)
	if len(lints) > 0 {
		return lints
	}

	lints = append(lints, lintSchema(filePath, glutMap)...)
	lints = append(lints, lintGlutKeys(filePath, glutMap)...)
	lints = append(lints, lintGlutName(filePath, glutMap)...)
	lints = append(lints, lintAssertSection(filePath, pipelineRoot, glutMap)...)
	lints = append(lints, lintStages(filePath, pipelineRoot)...)
	lints = append(lints, lintSetup(filePath, glutMap)...)
	return lints
}

func SemanticLint(testFile TestFile) []LintError {
	pipelineRoot, err := decodePipelineMap(testFile.PipelineYAML)
	if err != nil {
		return []LintError{{
			File:    testFile.FilePath,
			Level:   LevelError,
			Message: fmt.Sprintf("invalid pipeline yaml: %v", err),
		}}
	}

	lints := append([]LintError(nil), lintGlutName(testFile.FilePath, testFile.GlutRaw)...)
	lints = append(lints, lintAssertSection(testFile.FilePath, pipelineRoot, testFile.GlutRaw)...)
	lints = append(lints, lintStages(testFile.FilePath, pipelineRoot)...)
	lints = append(lints, lintSetup(testFile.FilePath, testFile.GlutRaw)...)
	return lints
}

func decodePipelineMap(pipelineYAML string) (map[string]interface{}, error) {
	var root map[string]interface{}
	if err := yaml.Unmarshal([]byte(pipelineYAML), &root); err != nil {
		return nil, err
	}
	if root == nil {
		root = map[string]interface{}{}
	}
	return root, nil
}

func lintSchema(filePath string, glutMap map[string]interface{}) []LintError {
	validationErrors, err := glutschema.ValidateGlut(glutMap)
	if err != nil {
		return []LintError{{File: filePath, Level: LevelError, Message: err.Error()}}
	}

	lints := make([]LintError, 0, len(validationErrors))
	for _, validationErr := range validationErrors {
		lints = append(lints, LintError{
			File:    filePath,
			Level:   LevelError,
			Message: "glut schema: " + glutschema.FormatValidationError(validationErr),
		})
	}
	return lints
}

func readLintInput(filePath string) (map[string]interface{}, map[string]interface{}, []LintError) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, []LintError{{File: filePath, Level: LevelError, Message: fmt.Sprintf("cannot read file: %v", err)}}
	}

	pipelineDoc, glutDoc, err := splitTestDocuments(data)
	if err != nil {
		if err == errMissingGlut {
			return nil, nil, nil
		}
		return nil, nil, []LintError{{File: filePath, Level: LevelError, Message: fmt.Sprintf("invalid yaml: %v", err)}}
	}

	pipelineRoot, err := nodeToMap(documentRoot(pipelineDoc))
	if err != nil {
		return nil, nil, []LintError{{File: filePath, Level: LevelError, Message: fmt.Sprintf("invalid pipeline yaml: %v", err)}}
	}

	glutNode, ok := topLevelValue(documentRoot(glutDoc), ".glut")
	if !ok {
		return nil, nil, nil
	}

	glutMap, err := nodeToMap(glutNode)
	if err != nil {
		return nil, nil, []LintError{{File: filePath, Level: LevelError, Message: ".glut metadata is not a map"}}
	}

	return pipelineRoot, glutMap, nil
}

func lintGlutKeys(filePath string, glutMap map[string]interface{}) []LintError {
	var lints []LintError
	for key := range glutMap {
		if key != "name" && key != "setup" && key != "assert" {
			lints = append(lints, LintError{File: filePath, Level: LevelError, Message: fmt.Sprintf("unknown key in .glut metadata: %s", key)})
		}
	}
	return lints
}

func lintGlutName(filePath string, glutMap map[string]interface{}) []LintError {
	name, hasName := glutMap["name"]
	if !hasName || name == "" {
		return []LintError{{File: filePath, Level: LevelWarning, Message: "missing .glut.name"}}
	}
	return nil
}

func lintAssertSection(filePath string, root map[string]interface{}, glutMap map[string]interface{}) []LintError {
	var lints []LintError
	assertVal, hasAssert := glutMap["assert"]
	if !hasAssert {
		return []LintError{{File: filePath, Level: LevelWarning, Message: ".glut.assert is empty"}}
	}

	assertMap, ok := assertVal.(map[string]interface{})
	if ok && len(assertMap) == 0 {
		lints = append(lints, LintError{File: filePath, Level: LevelWarning, Message: ".glut.assert is empty"})
	}
	if !ok {
		return lints
	}

	jobVal, ok := assertMap["job"]
	if !ok {
		return lints
	}
	jobMap, ok := jobVal.(map[string]interface{})
	if !ok {
		return lints
	}
	for jobName := range jobMap {
		if _, jobExists := root[jobName]; !jobExists {
			lints = append(lints, LintError{File: filePath, Level: LevelError, Message: fmt.Sprintf("assert.job references non-existent job '%s'", jobName)})
		}
	}
	return lints
}

func lintStages(filePath string, root map[string]interface{}) []LintError {
	stages, hasStages := readStages(root)
	if !hasStages {
		return nil
	}

	var lints []LintError
	for key, val := range root {
		if gitlabTopLevelKeywords[key] {
			continue
		}
		jobMap, ok := val.(map[string]interface{})
		if !ok {
			continue
		}
		stageVal, hasStage := jobMap["stage"]
		stageStr, ok := stageVal.(string)
		if !hasStage || !ok {
			continue
		}
		if !containsString(stages, stageStr) {
			lints = append(lints, LintError{File: filePath, Level: LevelError, Message: fmt.Sprintf("job '%s' has stage '%s' which is not in stages block", key, stageStr)})
		}
	}
	return lints
}

func readStages(root map[string]interface{}) ([]string, bool) {
	stagesVal, hasStages := root["stages"]
	if !hasStages {
		return nil, false
	}

	var stages []string
	stageList, ok := stagesVal.([]interface{})
	if !ok {
		return stages, true
	}
	for _, stage := range stageList {
		if str, ok := stage.(string); ok {
			stages = append(stages, str)
		}
	}
	return stages, true
}

func lintSetup(filePath string, glutMap map[string]interface{}) []LintError {
	setupVal, ok := glutMap["setup"]
	if !ok {
		return nil
	}
	setupMap, ok := setupVal.(map[string]interface{})
	if !ok {
		return nil
	}

	var lints []LintError
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
	return lints
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
