package parser

import (
	"fmt"
	"io/ioutil"

	"gopkg.in/yaml.v3"
)

var gitlabTopLevelKeywords = map[string]bool{
	"glut":          true,
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
	root, lints := readLintRoot(filePath)
	if len(lints) > 0 {
		return lints
	}

	glutVal, ok := root["glut"]
	if !ok {
		return nil
	}

	glutMap, ok := glutVal.(map[string]interface{})
	if !ok {
		return []LintError{{File: filePath, Level: LevelError, Message: "glut: section is not a map"}}
	}

	lints = append(lints, lintGlutKeys(filePath, glutMap)...)
	lints = append(lints, lintGlutName(filePath, glutMap)...)
	lints = append(lints, lintAssertSection(filePath, root, glutMap)...)
	lints = append(lints, lintStages(filePath, root)...)
	lints = append(lints, lintSetup(filePath, glutMap)...)
	return lints
}

func readLintRoot(filePath string) (map[string]interface{}, []LintError) {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, []LintError{{File: filePath, Level: LevelError, Message: fmt.Sprintf("cannot read file: %v", err)}}
	}

	var root map[string]interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, []LintError{{File: filePath, Level: LevelError, Message: fmt.Sprintf("invalid yaml: %v", err)}}
	}
	return root, nil
}

func lintGlutKeys(filePath string, glutMap map[string]interface{}) []LintError {
	var lints []LintError
	for key := range glutMap {
		if key != "name" && key != "setup" && key != "assert" {
			lints = append(lints, LintError{File: filePath, Level: LevelError, Message: fmt.Sprintf("unknown key in glut: section: %s", key)})
		}
	}
	return lints
}

func lintGlutName(filePath string, glutMap map[string]interface{}) []LintError {
	name, hasName := glutMap["name"]
	if !hasName || name == "" {
		return []LintError{{File: filePath, Level: LevelWarning, Message: "missing glut.name"}}
	}
	return nil
}

func lintAssertSection(filePath string, root map[string]interface{}, glutMap map[string]interface{}) []LintError {
	var lints []LintError
	assertVal, hasAssert := glutMap["assert"]
	if !hasAssert {
		return []LintError{{File: filePath, Level: LevelWarning, Message: "glut.assert is empty"}}
	}

	assertMap, ok := assertVal.(map[string]interface{})
	if ok && len(assertMap) == 0 {
		lints = append(lints, LintError{File: filePath, Level: LevelWarning, Message: "glut.assert is empty"})
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
