package asserter

import (
	"github.com/pandasoft-zz/glut/internal/config"
)

func runJobAsserts(asserts map[string]config.JobAssert, ctx AssertContext) []AssertResult {
	var results []AssertResult
	for _, jobName := range keysSorted(asserts) {
		basePath := quotedKeyPath("assert.job", jobName)
		jobAssert := asserts[jobName]
		output, present := ctx.JobOutputs[jobName]

		if jobAssert.Present != nil {
			results = append(results, resultFromBool(basePath+".present", *jobAssert.Present == present, *jobAssert.Present, present))
		}
		if !present {
			continue
		}

		if jobAssert.ExitStatus != nil {
			state := matchValue(jobAssert.ExitStatus, output.ExitStatus)
			results = append(results, resultFromState(basePath+".exit-status", state))
		}
		if jobAssert.Stdout != nil {
			state := matchTextPatterns(jobAssert.Stdout, output.Stdout)
			results = append(results, resultFromState(basePath+".stdout", state))
		}
		if jobAssert.Stderr != nil {
			state := matchTextPatterns(jobAssert.Stderr, output.Stderr)
			results = append(results, resultFromState(basePath+".stderr", state))
		}
	}
	return results
}

func resultFromState(path string, state matchState) AssertResult {
	if state.Passed {
		return passResult(path)
	}
	return failResult(path, state.Expected, state.Actual)
}

func resultFromBool(path string, passed bool, expected any, actual any) AssertResult {
	if passed {
		return passResult(path)
	}
	return failResult(path, expected, actual)
}
