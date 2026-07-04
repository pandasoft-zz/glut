package parser

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/pandasoft-zz/glut/internal/config"
	glutschema "github.com/pandasoft-zz/glut/schema"
	"gopkg.in/yaml.v3"
)

// Lint runs static analysis on a GLUT test file, reading and parsing it from
// disk. Prefer LintParsed when a *TestFile is already available (e.g. from
// ParseDir) to avoid re-reading and re-parsing the same file.
func Lint(filePath string) []LintError {
	pipelineRoot, glutMap, glutNode, found, lints := readLintInput(filePath)
	if len(lints) > 0 {
		return lints
	}
	if !found {
		return nil
	}
	return runLintChecks(filePath, glutMap, pipelineRoot, fieldLines(glutNode))
}

// LintParsed runs the same static analysis as Lint, reusing a *TestFile
// already produced by Parse/ParseDir instead of re-reading the file from
// disk.
func LintParsed(tf *TestFile) []LintError {
	var pipelineRoot map[string]interface{}
	if err := yaml.Unmarshal([]byte(tf.PipelineYAML), &pipelineRoot); err != nil {
		return []LintError{{File: tf.FilePath, Level: LevelError, Message: fmt.Sprintf("invalid pipeline yaml: %v", err)}}
	}
	return runLintChecks(tf.FilePath, tf.GlutRaw, pipelineRoot, fieldLines(tf.GlutNode))
}

func runLintChecks(filePath string, glutMap map[string]interface{}, pipelineRoot map[string]interface{}, lines map[string]int) []LintError {
	var lints []LintError
	lints = append(lints, lintSchema(filePath, glutMap, lines)...)
	lints = append(lints, lintGlutKeys(filePath, glutMap)...)
	lints = append(lints, lintGlutName(filePath, glutMap)...)
	lints = append(lints, lintAssertSection(filePath, glutMap)...)
	lints = append(lints, lintAssertJobsExistInPipeline(filePath, glutMap, pipelineRoot)...)
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

// fieldLines maps each dotted field path under a .glut mapping node (e.g.
// "setup.pipeline_source") to its source line number, so schema errors
// (which report the same dotted-path format) can carry a Line. Returns an
// empty map for a nil node.
func fieldLines(node *yaml.Node) map[string]int {
	lines := make(map[string]int)
	collectFieldLines(node, "", lines)
	return lines
}

func collectFieldLines(node *yaml.Node, prefix string, lines map[string]int) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		path := keyNode.Value
		if prefix != "" {
			path = prefix + "." + path
		}
		if _, exists := lines[path]; !exists {
			lines[path] = keyNode.Line
		}
		collectFieldLines(valueNode, path, lines)
	}
}

func lintSchema(filePath string, glutMap map[string]interface{}, lines map[string]int) []LintError {
	validationErrors, err := glutschema.ValidateGlut(glutMap)
	if err != nil {
		return []LintError{{File: filePath, Level: LevelError, Message: err.Error()}}
	}

	lints := make([]LintError, 0, len(validationErrors))
	for _, validationErr := range validationErrors {
		lints = append(lints, LintError{
			File:    filePath,
			Level:   LevelError,
			Line:    lines[validationErr.Field],
			Message: "glut schema: " + glutschema.FormatValidationError(validationErr),
		})
	}
	return lints
}

// readLintInput reads and parses filePath, returning the pipeline root and
// .glut metadata maps needed by the lint checks, plus the .glut yaml.Node
// (for source line lookups). found is false when the file has no .glut
// document at all (not a GLUT test file), in which case Lint stops without
// validating a nil map against the schema.
func readLintInput(filePath string) (pipelineRoot map[string]interface{}, glutMap map[string]interface{}, glutNode *yaml.Node, found bool, lints []LintError) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, nil, false, []LintError{{File: filePath, Level: LevelError, Message: fmt.Sprintf("cannot read file: %v", err)}}
	}

	pipelineDoc, glutDoc, err := splitTestDocuments(data)
	if err != nil {
		if errors.Is(err, errMissingGlut) {
			return nil, nil, nil, false, nil
		}
		return nil, nil, nil, false, []LintError{{File: filePath, Level: LevelError, Message: fmt.Sprintf("invalid yaml: %v", err)}}
	}

	pipelineRoot, err = nodeToMap(documentRoot(pipelineDoc))
	if err != nil {
		return nil, nil, nil, false, []LintError{{File: filePath, Level: LevelError, Message: fmt.Sprintf("invalid pipeline yaml: %v", err)}}
	}

	glutNode, ok := topLevelValue(documentRoot(glutDoc), ".glut")
	if !ok {
		return nil, nil, nil, false, nil
	}

	glutMap, err = nodeToMap(glutNode)
	if err != nil {
		return nil, nil, nil, false, []LintError{{File: filePath, Level: LevelError, Message: ".glut metadata is not a map"}}
	}

	return pipelineRoot, glutMap, glutNode, true, nil
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
	if !hasAssert || assertVal == nil {
		return []LintError{{File: filePath, Level: LevelWarning, Message: ".glut.assert is empty"}}
	}

	assertMap, ok := assertVal.(map[string]interface{})
	if ok && len(assertMap) == 0 {
		lints = append(lints, LintError{File: filePath, Level: LevelWarning, Message: ".glut.assert is empty"})
	}
	if ok {
		lints = append(lints, lintJobAsserts(filePath, assertMap)...)
	}
	return lints
}

func lintJobAsserts(filePath string, assertMap map[string]interface{}) []LintError {
	jobVal, ok := assertMap["job"]
	if !ok {
		return nil
	}
	jobMap, ok := jobVal.(map[string]interface{})
	if !ok {
		return nil
	}

	jobNames := make([]string, 0, len(jobMap))
	for name := range jobMap {
		jobNames = append(jobNames, name)
	}
	sort.Strings(jobNames)

	var lints []LintError
	for _, jobName := range jobNames {
		jobAssert, ok := jobMap[jobName].(map[string]interface{})
		if !ok {
			continue
		}
		present, hasPresent := jobAssert["present"].(bool)
		_, hasWhen := jobAssert["when"]
		if hasPresent && !present && hasWhen {
			lints = append(lints, LintError{
				File:    filePath,
				Level:   LevelError,
				Message: fmt.Sprintf(".glut.assert.job.%s: \"when\" cannot be combined with \"present: false\" (an absent job has no when value)", jobName),
			})
		}
	}
	return lints
}

// gitlabReservedTopLevelKeys are pipeline keys that are never job definitions.
// "pages" is intentionally absent: it is a real, common GitLab CI job name, so
// treating it as reserved would make assert.job.pages a false lint error and
// drop it from doctor coverage.
var gitlabReservedTopLevelKeys = map[string]bool{
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
}

// IsReservedTopLevelKey reports whether key is a GitLab CI top-level keyword
// that never denotes a job (stages, variables, workflow, ...). It is the single
// source of truth for both the missing-job lint and doctor coverage, so the two
// cannot drift apart on which keys count as jobs.
func IsReservedTopLevelKey(key string) bool {
	return gitlabReservedTopLevelKeys[key]
}

// pipelineJobNamesFromRoot returns the set of real job names defined at the
// top level of a parsed pipeline document. It reports dynamic=true when the
// pipeline pulls in jobs from elsewhere (include:) or is itself a CI/CD
// component definition (spec:), since job names cannot be resolved statically
// in either case.
func pipelineJobNamesFromRoot(pipelineRoot map[string]interface{}) (jobs map[string]struct{}, dynamic bool) {
	if _, hasInclude := pipelineRoot["include"]; hasInclude {
		return nil, true
	}
	if _, hasSpec := pipelineRoot["spec"]; hasSpec {
		return nil, true
	}

	jobs = make(map[string]struct{}, len(pipelineRoot))
	for key := range pipelineRoot {
		if strings.HasPrefix(key, ".") || gitlabReservedTopLevelKeys[key] {
			continue
		}
		jobs[key] = struct{}{}
	}
	return jobs, false
}

// lintAssertJobsExistInPipeline flags assert.job entries that reference a job
// name absent from the pipeline document — almost always a typo that would
// otherwise only surface as a confusing run-time failure.
func lintAssertJobsExistInPipeline(filePath string, glutMap map[string]interface{}, pipelineRoot map[string]interface{}) []LintError {
	assertVal, ok := glutMap["assert"]
	if !ok {
		return nil
	}
	assertMap, ok := assertVal.(map[string]interface{})
	if !ok {
		return nil
	}
	jobVal, ok := assertMap["job"]
	if !ok {
		return nil
	}
	jobMap, ok := jobVal.(map[string]interface{})
	if !ok {
		return nil
	}

	pipelineJobs, dynamic := pipelineJobNamesFromRoot(pipelineRoot)
	if dynamic {
		return nil
	}

	jobNames := make([]string, 0, len(jobMap))
	for name := range jobMap {
		jobNames = append(jobNames, name)
	}
	sort.Strings(jobNames)

	var lints []LintError
	for _, jobName := range jobNames {
		if _, ok := pipelineJobs[jobName]; ok {
			continue
		}
		jobAssert, _ := jobMap[jobName].(map[string]interface{})
		if present, ok := jobAssert["present"].(bool); ok && !present {
			// Asserting a job is absent is valid even when it was never defined.
			continue
		}
		lints = append(lints, LintError{
			File:    filePath,
			Level:   LevelError,
			Message: fmt.Sprintf(".glut.assert.job.%s references a job that is not defined in the pipeline", jobName),
		})
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

	if source, ok := setupMap["pipeline_source"].(string); ok && source == config.PipelineSourceMR {
		if _, hasMR := setupMap["merge_request"]; !hasMR {
			lints = append(lints, LintError{File: filePath, Level: LevelError, Message: "setup.pipeline_source is merge_request_event but setup.merge_request is missing"})
		}
	}
	return lints
}

