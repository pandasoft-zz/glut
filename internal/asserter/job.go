package asserter

import (
	"fmt"

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
			if !*jobAssert.Present {
				// Expected the job to be absent — skip field assertions regardless of actual state.
				continue
			}
		} else if !present {
			results = append(results, failResult(basePath+".present", true, false))
		}
		if !present {
			continue
		}

		if jobAssert.When != "" {
			results = append(results, resultFromBool(basePath+".when", jobAssert.When == output.When, jobAssert.When, output.When))
		}

		hasFieldAsserts := jobAssert.ExitStatus != nil || jobAssert.Stdout != nil || jobAssert.Stderr != nil || jobAssert.Output != nil
		if hasFieldAsserts && !output.Executed {
			// A present-but-not-executed job (e.g. when: manual) has zero-value
			// outputs; matching against them would pass spuriously.
			results = append(results, failResult(basePath, "job executed", fmt.Sprintf("job present but not executed (when: %s)", output.When)))
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
		if jobAssert.Output != nil {
			combined := output.Stdout + "\n" + output.Stderr
			state := matchTextPatterns(jobAssert.Output, combined)
			results = append(results, resultFromState(basePath+".output", state))
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
