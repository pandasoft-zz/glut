package asserter

import (
	"sort"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/executor"
	"github.com/pandasoft-zz/glut/internal/mockserver"
	"github.com/pandasoft-zz/glut/internal/mockwrapper"
)

type AssertResult struct {
	Path     string
	Passed   bool
	Expected string
	Actual   string
}

// GitOriginSource provides a filesystem path to a git origin repository.
// Path is called lazily — implementations may defer extraction until first use.
type GitOriginSource interface {
	Path() (string, error)
}

// FSOrigin is a GitOriginSource backed by an existing directory on disk.
type FSOrigin struct{ path string }

func NewFSOrigin(path string) GitOriginSource { return FSOrigin{path: path} }
func (f FSOrigin) Path() (string, error)      { return f.path, nil }

type AssertContext struct {
	WorkspacePath string
	OriginRepo    GitOriginSource
	JobOutputs    map[string]executor.JobOutput
	APICalls      []mockserver.APICall
	BinaryLogs    map[string][]mockwrapper.BinaryCall
}

func Run(asserts config.AssertConfig, ctx AssertContext) []AssertResult {
	var results []AssertResult

	results = append(results, runJobAsserts(asserts.Job, ctx)...)
	results = append(results, runArtifactAsserts(asserts.Artifacts, ctx)...)
	results = append(results, runGitAsserts(asserts.Git, ctx)...)
	results = append(results, runAPIAsserts(asserts.API, ctx)...)
	results = append(results, runBinaryAsserts(asserts.Binary, ctx)...)

	sort.SliceStable(results, func(i int, j int) bool {
		return results[i].Path < results[j].Path
	})
	return results
}
