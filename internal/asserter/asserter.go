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

type AssertContext struct {
	WorkspacePath  string
	OriginRepoPath string
	JobOutputs     map[string]executor.JobOutput
	APICalls       []mockserver.APICall
	BinaryLogs     map[string][]mockwrapper.BinaryCall
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
