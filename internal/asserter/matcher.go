package asserter

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/tidwall/gjson"
)

// matcherKeys is a read-only lookup table: the set of recognized matcher
// operator names. Never mutate it — all access is concurrent reads, and a
// package-level map that gets written to at runtime would be a shared-state
// hazard across goroutines evaluating asserts in parallel.
var matcherKeys = map[string]bool{
	"equal":             true,
	"have-prefix":       true,
	"have-suffix":       true,
	"contain-substring": true,
	"match-regexp":      true,
	"gt":                true,
	"ge":                true,
	"lt":                true,
	"le":                true,
	"contain-element":   true,
	"contain-elements":  true,
	"consist-of":        true,
	"have-len":          true,
	"have-key":          true,
	"semver-constraint": true,
	"gjson":             true,
	"and":               true,
	"or":                true,
	"not":               true,
}

type matchState struct {
	Passed   bool
	Expected any
	Actual   any
	// IsError marks a failure caused by a broken matcher configuration
	// (invalid regexp, invalid semver constraint, undecodable gjson
	// payload) rather than a genuine value mismatch. not/and/or must never
	// turn this into a pass — a config error means the test file itself is
	// broken, regardless of which branch of a logic operator it sits under.
	IsError bool
}

func matchValue(expected any, actual any) matchState {
	if expectedString, ok := expected.(string); ok {
		if actualString, actualOK := actual.(string); actualOK && isSpecialStringMatcher(expectedString) {
			// Scan per-line, same as a pattern list (matchTextPatterns), so
			// "/^a$/" behaves the same whether it appears as a scalar or as
			// the sole entry of a list — otherwise a scalar regexp matches
			// the whole multi-line text as one line, while ^/$ in a list
			// anchor to each line.
			lines, err := scanLines(actualString)
			if err != nil {
				// A scan failure is a broken input, not a mismatch: mark it
				// IsError so not/and/or cannot invert it into a pass.
				return matchState{Expected: expectedString, Actual: fmt.Sprintf("<failed to scan output: %v>", err), IsError: true}
			}
			matched, patErr := matchSinglePattern(expectedString, lines)
			if patErr != nil {
				return matchState{Expected: fmt.Sprintf("valid pattern %q", expectedString), Actual: patErr.Error(), IsError: true}
			}
			if matched {
				return matchState{Passed: true}
			}
			return matchState{Expected: expectedString, Actual: actualString}
		}
	}

	expectedMap, isMap := toStringMap(expected)
	if isMap && len(expectedMap) == 1 {
		for key, value := range expectedMap {
			if matcherKeys[key] {
				return runMatcher(key, value, actual)
			}
		}
	}

	if expectedSlice, ok := toSlice(expected); ok {
		actualSlice, actualOK := toSlice(actual)
		if !actualOK {
			return matchState{Expected: expected, Actual: actual}
		}
		// A bare list matches as a subset: every expected item must be
		// present, but actual may have extra items (see assert-syntax.md).
		// used-flags prevent a single actual item from satisfying more than
		// one expected item, so a duplicated expected value (e.g.
		// ["--flag", "--flag"]) requires two matching actual occurrences.
		used := make([]bool, len(actualSlice))
		for _, item := range expectedSlice {
			found := false
			for index, actualItem := range actualSlice {
				if used[index] {
					continue
				}
				if matchValue(item, actualItem).Passed {
					used[index] = true
					found = true
					break
				}
			}
			if !found {
				return matchState{Expected: expected, Actual: actual}
			}
		}
		return matchState{Passed: true}
	}

	if deepEqualValue(expected, actual) {
		return matchState{Passed: true}
	}
	return matchState{Expected: expected, Actual: actual}
}

func isSpecialStringMatcher(value string) bool {
	return strings.HasPrefix(value, "\\!") ||
		(strings.HasPrefix(value, "/") && strings.HasSuffix(value, "/")) ||
		(strings.HasPrefix(value, "!/") && strings.HasSuffix(value, "/"))
}

func runMatcher(name string, expected any, actual any) matchState {
	switch name {
	case "equal":
		if deepEqualValue(expected, actual) {
			return matchState{Passed: true}
		}
	case "have-prefix":
		actualString, ok := actual.(string)
		expectedString, expectedOK := expected.(string)
		if ok && expectedOK && strings.HasPrefix(actualString, expectedString) {
			return matchState{Passed: true}
		}
	case "have-suffix":
		actualString, ok := actual.(string)
		expectedString, expectedOK := expected.(string)
		if ok && expectedOK && strings.HasSuffix(actualString, expectedString) {
			return matchState{Passed: true}
		}
	case "contain-substring":
		actualString, ok := actual.(string)
		expectedString, expectedOK := expected.(string)
		if ok && expectedOK && strings.Contains(actualString, expectedString) {
			return matchState{Passed: true}
		}
	case "match-regexp":
		actualString, ok := actual.(string)
		expectedString, expectedOK := expected.(string)
		if ok && expectedOK {
			pattern, err := regexp.Compile(expectedString)
			if err != nil {
				return matchState{Expected: fmt.Sprintf("valid regexp %q", expectedString), Actual: err.Error(), IsError: true}
			}
			if pattern.MatchString(actualString) {
				return matchState{Passed: true}
			}
		}
	case "gt", "ge", "lt", "le":
		expectedNumber, expectedOK := toFloat64(expected)
		actualNumber, actualOK := toFloat64(actual)
		if expectedOK && actualOK {
			passed := false
			switch name {
			case "gt":
				passed = actualNumber > expectedNumber
			case "ge":
				passed = actualNumber >= expectedNumber
			case "lt":
				passed = actualNumber < expectedNumber
			case "le":
				passed = actualNumber <= expectedNumber
			}
			if passed {
				return matchState{Passed: true}
			}
		}
	case "contain-element":
		actualSlice, ok := toSlice(actual)
		if ok {
			for _, item := range actualSlice {
				if matchValue(expected, item).Passed {
					return matchState{Passed: true}
				}
			}
		}
	case "contain-elements":
		expectedSlice, expectedOK := toSlice(expected)
		actualSlice, actualOK := toSlice(actual)
		if expectedOK && actualOK {
			// used-flags prevent a single actual item from satisfying more
			// than one expected item (same reuse flaw as the bare-list
			// matcher above; consist-of below already does this).
			used := make([]bool, len(actualSlice))
			allFound := true
			for _, expectedItem := range expectedSlice {
				found := false
				for index, actualItem := range actualSlice {
					if used[index] {
						continue
					}
					if matchValue(expectedItem, actualItem).Passed {
						used[index] = true
						found = true
						break
					}
				}
				if !found {
					allFound = false
					break
				}
			}
			if allFound {
				return matchState{Passed: true}
			}
		}
	case "consist-of":
		expectedSlice, expectedOK := toSlice(expected)
		actualSlice, actualOK := toSlice(actual)
		if expectedOK && actualOK && len(expectedSlice) == len(actualSlice) {
			used := make([]bool, len(actualSlice))
			matched := true
			for _, expectedItem := range expectedSlice {
				found := false
				for index, actualItem := range actualSlice {
					if used[index] {
						continue
					}
					if matchValue(expectedItem, actualItem).Passed {
						used[index] = true
						found = true
						break
					}
				}
				if !found {
					matched = false
					break
				}
			}
			if matched {
				return matchState{Passed: true}
			}
		}
	case "have-len":
		expectedNumber, expectedOK := toFloat64(expected)
		if expectedOK {
			length, ok := lengthOf(actual)
			if ok && float64(length) == expectedNumber {
				return matchState{Passed: true}
			}
		}
	case "have-key":
		actualMap, ok := toStringMap(actual)
		expectedString, expectedOK := expected.(string)
		if ok && expectedOK {
			_, exists := actualMap[expectedString]
			if exists {
				return matchState{Passed: true}
			}
		}
	case "semver-constraint":
		actualString, ok := actual.(string)
		expectedString, expectedOK := expected.(string)
		if ok && expectedOK {
			constraint, err := semver.NewConstraint(expectedString)
			if err != nil {
				return matchState{Expected: fmt.Sprintf("valid semver constraint %q", expectedString), Actual: err.Error(), IsError: true}
			}
			version, err := semver.NewVersion(actualString)
			if err == nil && constraint.Check(version) {
				return matchState{Passed: true}
			}
		}
	case "gjson":
		paths, ok := toStringMap(expected)
		if ok {
			payload, err := asJSONBytes(actual)
			if err != nil {
				return matchState{Expected: expected, Actual: err.Error(), IsError: true}
			}
			for path, childExpected := range paths {
				value := gjson.GetBytes(payload, path)
				if !value.Exists() {
					return matchState{Expected: fmt.Sprintf("gjson path %s to exist", path), Actual: "missing"}
				}
				if !matchValue(childExpected, gjsonValue(value)).Passed {
					return matchState{Expected: map[string]any{path: childExpected}, Actual: map[string]any{path: gjsonValue(value)}}
				}
			}
			return matchState{Passed: true}
		}
	case "and":
		items, ok := toSlice(expected)
		if ok {
			for _, item := range items {
				state := matchValue(item, actual)
				if !state.Passed {
					return state
				}
			}
			return matchState{Passed: true}
		}
	case "or":
		items, ok := toSlice(expected)
		if ok {
			last := matchState{Expected: expected, Actual: actual}
			sawError := false
			for _, item := range items {
				state := matchValue(item, actual)
				if state.Passed {
					return matchState{Passed: true}
				}
				if state.IsError {
					sawError = true
				}
				last = state
			}
			// A broken branch anywhere in the or must not be lost just because
			// a later branch was a plain mismatch — otherwise not: {or: [...]}
			// could invert a config error into a pass.
			last.IsError = last.IsError || sawError
			return last
		}
	case "not":
		state := matchValue(expected, actual)
		if state.IsError {
			// A broken matcher configuration (invalid regexp, invalid
			// semver constraint, ...) must never be inverted into a pass —
			// that would mask a broken test file behind "not" instead of
			// surfacing the config error.
			return state
		}
		if !state.Passed {
			return matchState{Passed: true}
		}
		return matchState{Expected: map[string]any{"not": expected}, Actual: actual}
	}

	return matchState{Expected: map[string]any{name: expected}, Actual: actual}
}

func lengthOf(value any) (int, bool) {
	// A nil value (e.g. a JSON null field) has a length of zero, matching
	// have-len: 0 the same way an empty string/slice/map does — treating
	// nil as "unmeasurable" made that specific, common case fail.
	if value == nil {
		return 0, true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map, reflect.String:
		return reflected.Len(), true
	default:
		return 0, false
	}
}

func asJSONBytes(value any) ([]byte, error) {
	switch typed := value.(type) {
	case string:
		return []byte(typed), nil
	case []byte:
		return typed, nil
	default:
		return json.Marshal(value)
	}
}

func gjsonValue(value gjson.Result) any {
	if !value.Exists() {
		return nil
	}
	switch value.Type {
	case gjson.String:
		return value.String()
	case gjson.Number:
		return value.Num
	case gjson.True:
		return true
	case gjson.False:
		return false
	case gjson.JSON:
		var out any
		if err := json.Unmarshal([]byte(value.Raw), &out); err == nil {
			return out
		}
		return value.Raw
	default:
		return nil
	}
}
