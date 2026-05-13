package parser

import (
	"fmt"
	"os"

	glutschema "github.com/pandasoft-zz/glut/schema"
)

// Lint runs static analysis on a GLUT test file.
func Lint(filePath string) []LintError {
	_, glutMap, lints := readLintInput(filePath)
	if len(lints) > 0 {
		return lints
	}

	lints = append(lints, lintSchema(filePath, glutMap)...)
	lints = append(lints, lintGlutKeys(filePath, glutMap)...)
	lints = append(lints, lintGlutName(filePath, glutMap)...)
	lints = append(lints, lintAssertSection(filePath, glutMap)...)
	lints = append(lints, lintSetup(filePath, glutMap)...)
	return lints
}

// SemanticLint validates the .glut: metadata section of a parsed test file.
func SemanticLint(filePath string, glutRaw map[string]interface{}) []LintError {
	lints := append([]LintError(nil), lintGlutName(filePath, glutRaw)...)
	lints = append(lints, lintAssertSection(filePath, glutRaw)...)
	lints = append(lints, lintSetup(filePath, glutRaw)...)
	return lints
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

func lintAssertSection(filePath string, glutMap map[string]interface{}) []LintError {
	var lints []LintError
	assertVal, hasAssert := glutMap["assert"]
	if !hasAssert {
		return []LintError{{File: filePath, Level: LevelWarning, Message: ".glut.assert is empty"}}
	}

	assertMap, ok := assertVal.(map[string]interface{})
	if ok && len(assertMap) == 0 {
		lints = append(lints, LintError{File: filePath, Level: LevelWarning, Message: ".glut.assert is empty"})
	}
	return lints
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

