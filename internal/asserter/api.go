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
	method, path, query := splitEndpointPattern(endpoint)
	var out []mockserver.APICall
	for _, call := range calls {
		if call.Method != method {
			continue
		}
		if !pathMatches(path, call.Path) {
			continue
		}
		// An endpoint pattern with no "?query" matches any query string; one
		// with a literal query requires an exact match, so e.g.
		// "GET /pipelines?ref=main" can be distinguished from "?ref=dev".
		if query != "" && call.Query != query {
			continue
		}
		out = append(out, call)
	}
	return out
}

func splitEndpointPattern(endpoint string) (method string, path string, query string) {
	method, rest, ok := strings.Cut(endpoint, " ")
	if !ok {
		return endpoint, "", ""
	}
	path, query, _ = strings.Cut(rest, "?")
	return method, path, query
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
	// A pure `gjson:` assert queries the raw body bytes directly and never
	// needs the body decoded as a map, so a JSON array body (or any other
	// non-object top level) can still be asserted on. Unmarshalling into a
	// map unconditionally made every gjson-only assert fail on those bodies.
	if gjsonValue, ok := expected["gjson"]; ok && len(expected) == 1 {
		return matchValue(map[string]any{"gjson": gjsonValue}, body).Passed
	}

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
