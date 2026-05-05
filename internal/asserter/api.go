package asserter

import (
	"encoding/json"
	"strings"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/mockserver"
)

func runAPIAsserts(asserts map[string]config.APICallAssert, ctx AssertContext) []AssertResult {
	var results []AssertResult
	for _, endpoint := range keysSorted(asserts) {
		basePath := quotedKeyPath("assert.api", endpoint)
		assert := asserts[endpoint]
		calls := filterAPICalls(ctx.APICalls, endpoint)

		if assert.Called != nil {
			results = append(results, resultFromBool(basePath+".called", *assert.Called == (len(calls) > 0), *assert.Called, len(calls) > 0))
		}
		if assert.Times != nil {
			results = append(results, resultFromState(basePath+".times", matchValue(assert.Times, len(calls))))
		}
		if len(assert.Body) > 0 {
			results = append(results, runAPIBodyAssert(basePath+".body", calls, assert.Body)...)
		}
	}
	return results
}

func filterAPICalls(calls []mockserver.APICall, endpoint string) []mockserver.APICall {
	method, path := splitEndpointPattern(endpoint)
	var out []mockserver.APICall
	for _, call := range calls {
		if call.Method != method {
			continue
		}
		if pathMatches(path, call.Path) {
			out = append(out, call)
		}
	}
	return out
}

func splitEndpointPattern(endpoint string) (string, string) {
	method, path, ok := strings.Cut(endpoint, " ")
	if !ok {
		return endpoint, ""
	}
	return method, path
}

func pathMatches(pattern string, actual string) bool {
	patternParts := splitPath(pattern)
	actualParts := splitPath(actual)
	if len(patternParts) != len(actualParts) {
		return false
	}
	for index := range patternParts {
		if patternParts[index] == "*" {
			continue
		}
		if patternParts[index] != actualParts[index] {
			return false
		}
	}
	return true
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func runAPIBodyAssert(basePath string, calls []mockserver.APICall, expected map[string]any) []AssertResult {
	if len(calls) == 0 {
		return []AssertResult{failResult(basePath, expected, "no matching API call")}
	}

	for _, call := range calls {
		if apiBodyMatches(call.RequestBody, expected) {
			return []AssertResult{passResult(basePath)}
		}
	}
	return []AssertResult{failResult(basePath, expected, "no matching call body")}
}

func apiBodyMatches(body []byte, expected map[string]any) bool {
	var actual map[string]any
	if err := json.Unmarshal(body, &actual); err != nil {
		return false
	}

	for key, expectedValue := range expected {
		if key == "gjson" {
			if !matchValue(map[string]any{"gjson": expectedValue}, body).Passed {
				return false
			}
			continue
		}
		actualValue, ok := actual[key]
		if !ok || !matchValue(expectedValue, actualValue).Passed {
			return false
		}
	}
	return true
}
