package asserter

import (
	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/mockwrapper"
)

func runBinaryAsserts(asserts map[string]config.BinaryAssert, ctx AssertContext) []AssertResult {
	var results []AssertResult
	for _, name := range keysSorted(asserts) {
		basePath := quotedKeyPath("assert.binary", name)
		assert := asserts[name]
		calls := ctx.BinaryLogs[name]

		if assert.Called != nil {
			results = append(results, resultFromBool(basePath+".called", *assert.Called == (len(calls) > 0), *assert.Called, len(calls) > 0))
		}
		if assert.Times != nil {
			results = append(results, resultFromState(basePath+".times", matchValue(assert.Times, len(calls))))
		}
		for index, expectedCall := range assert.Calls {
			if index >= len(calls) {
				results = append(results, failResult(pathForIndex(basePath+".calls", index, ""), "call to exist", "missing"))
				continue
			}
			results = append(results, runSingleBinaryCallAssert(pathForIndex(basePath+".calls", index, ""), expectedCall, calls[index])...)
		}
		if assert.NeverCalledWith != nil {
			passed := true
			for _, call := range calls {
				if binaryCallMatches(*assert.NeverCalledWith, call) {
					passed = false
					break
				}
			}
			results = append(results, resultFromBool(basePath+".never-called-with", passed, "no call to match", "matched call found"))
		}
	}
	return results
}

func runSingleBinaryCallAssert(basePath string, assert config.BinaryCallAssert, call mockwrapper.BinaryCall) []AssertResult {
	var results []AssertResult
	if assert.Args != nil {
		results = append(results, resultFromState(basePath+".args", matchValue(assert.Args, call.Args)))
	}
	if assert.CWD != nil {
		results = append(results, resultFromState(basePath+".cwd", matchValue(assert.CWD, call.CWD)))
	}
	if assert.Stdin != nil {
		results = append(results, resultFromState(basePath+".stdin", matchValue(assert.Stdin, call.Stdin)))
	}
	return results
}

func binaryCallMatches(assert config.BinaryCallAssert, call mockwrapper.BinaryCall) bool {
	if assert.Args != nil && !matchValue(assert.Args, call.Args).Passed {
		return false
	}
	if assert.CWD != nil && !matchValue(assert.CWD, call.CWD).Passed {
		return false
	}
	if assert.Stdin != nil && !matchValue(assert.Stdin, call.Stdin).Passed {
		return false
	}
	return true
}
