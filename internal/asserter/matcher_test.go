package asserter

import (
	"strings"
	"testing"
)

func TestMatchValueAdvancedMatchers(t *testing.T) {
	tests := []struct {
		name     string
		expected any
		actual   any
		wantPass bool
	}{
		{name: "equal", expected: map[string]any{"equal": []any{"a", "b"}}, actual: []string{"a", "b"}, wantPass: true},
		{name: "have-prefix", expected: map[string]any{"have-prefix": "v"}, actual: "v1.2.3", wantPass: true},
		{name: "have-suffix", expected: map[string]any{"have-suffix": ".0"}, actual: "1.0", wantPass: true},
		{name: "contain-substring", expected: map[string]any{"contain-substring": "err"}, actual: "stderr", wantPass: true},
		{name: "match-regexp", expected: map[string]any{"match-regexp": "^ab[0-9]+$"}, actual: "ab42", wantPass: true},
		{name: "gt", expected: map[string]any{"gt": 2}, actual: 3, wantPass: true},
		{name: "ge", expected: map[string]any{"ge": 2}, actual: 2, wantPass: true},
		{name: "lt", expected: map[string]any{"lt": 10}, actual: 3, wantPass: true},
		{name: "le", expected: map[string]any{"le": 10}, actual: 10, wantPass: true},
		{name: "contain-element", expected: map[string]any{"contain-element": "b"}, actual: []string{"a", "b"}, wantPass: true},
		{name: "contain-elements", expected: map[string]any{"contain-elements": []any{"a", "b"}}, actual: []string{"b", "a", "c"}, wantPass: true},
		{name: "consist-of", expected: map[string]any{"consist-of": []any{"a", "b"}}, actual: []string{"b", "a"}, wantPass: true},
		{name: "have-len", expected: map[string]any{"have-len": 2}, actual: []string{"a", "b"}, wantPass: true},
		{name: "have-key", expected: map[string]any{"have-key": "name"}, actual: map[string]any{"name": "ok"}, wantPass: true},
		{name: "semver", expected: map[string]any{"semver-constraint": ">= 1.2.0, < 2.0.0"}, actual: "1.3.0", wantPass: true},
		{name: "gjson", expected: map[string]any{"gjson": map[string]any{"items.#": map[string]any{"gt": 0}}}, actual: `{"items":[1]}`, wantPass: true},
		{name: "and or not", expected: map[string]any{"and": []any{map[string]any{"not": 2}, map[string]any{"or": []any{0, 1}}}}, actual: 1, wantPass: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchValue(tt.expected, tt.actual)
			if got.Passed != tt.wantPass {
				t.Fatalf("Passed = %v, want %v (expected=%v actual=%v)", got.Passed, tt.wantPass, tt.expected, tt.actual)
			}
		})
	}
}

// TestScalarRegexpMatchesPerLineLikeATextPatternList guards against a
// scalar "/regexp/" (e.g. stdout: "/^a$/") being matched against the whole
// multi-line text as one blob — so ^/$ anchor to the start/end of the
// entire text — while the same pattern in a text pattern list
// (stdout: ["/^a$/"]) is scanned per line. matchTextPatterns falls through
// to matchValue for a scalar expected value, so both forms must behave
// identically.
func TestScalarRegexpMatchesPerLineLikeATextPatternList(t *testing.T) {
	actual := "first\na\nthird"

	scalar := matchValue("/^a$/", actual)
	if !scalar.Passed {
		t.Fatalf("scalar /^a$/ should match a line equal to \"a\", got %+v", scalar)
	}

	list := matchTextPatterns([]any{"/^a$/"}, actual)
	if !list.Passed {
		t.Fatalf("list [\"/^a$/\"] should match a line equal to \"a\", got %+v", list)
	}
}

func TestMatchTextPatterns(t *testing.T) {
	actual := "hello\nversion 12\n!literal"
	patterns := []any{"hello", "!missing", "/version \\d+/", "\\!literal"}
	if !matchTextPatterns(patterns, actual).Passed {
		t.Fatal("patterns should pass")
	}
	if matchTextPatterns([]any{"!/version \\d+/"}, actual).Passed {
		t.Fatal("negated regex should fail")
	}
}

// TestMatchTextPatternsSurvivesLineLongerThanDefaultScannerLimit guards
// against bufio.Scanner's default 64 KiB token limit silently stopping the
// scan partway through the output: a positive pattern on a later line must
// still be found, and a negated pattern on a later line must still fail the
// match (not pass vacuously because the scan never reached it).
func TestMatchTextPatternsSurvivesLineLongerThanDefaultScannerLimit(t *testing.T) {
	longLine := strings.Repeat("x", 128*1024) // well over bufio.MaxScanTokenSize
	actual := longLine + "\npanic: boom\n"

	if !matchTextPatterns([]any{"panic: boom"}, actual).Passed {
		t.Fatal("expected the line after the oversized line to still be matched")
	}
	if matchTextPatterns([]any{"!panic"}, actual).Passed {
		t.Fatal("expected the negated pattern to fail once the later line is actually scanned")
	}
}

// TestMatchValueBareListDoesNotReuseActualItems guards against a duplicated
// expected item matching the same actual element twice: ["--flag", "--flag"]
// must not pass against a single "--flag" in the actual list.
func TestMatchValueBareListDoesNotReuseActualItems(t *testing.T) {
	got := matchValue([]any{"--flag", "--flag"}, []string{"--flag"})
	if got.Passed {
		t.Fatalf("expected duplicated expected item to require two actual matches, got %+v", got)
	}

	got = matchValue([]any{"--flag", "--flag"}, []string{"--flag", "--flag"})
	if !got.Passed {
		t.Fatalf("expected two actual occurrences to satisfy two expected items, got %+v", got)
	}
}

// TestContainElementsDoesNotReuseActualItems guards against the same
// duplicate-reuse flaw in contain-elements.
func TestContainElementsDoesNotReuseActualItems(t *testing.T) {
	got := matchValue(map[string]any{"contain-elements": []any{"a", "a"}}, []string{"a"})
	if got.Passed {
		t.Fatalf("expected duplicated expected item to require two actual matches, got %+v", got)
	}

	got = matchValue(map[string]any{"contain-elements": []any{"a", "a"}}, []string{"a", "a", "b"})
	if !got.Passed {
		t.Fatalf("expected two actual occurrences to satisfy two expected items, got %+v", got)
	}
}

// TestNotDoesNotInvertMatcherConfigErrors guards against not: {match-regexp:
// "("} always passing: an invalid regexp is a broken test file, not a
// genuine mismatch, so not must not invert it into a pass.
func TestNotDoesNotInvertMatcherConfigErrors(t *testing.T) {
	got := matchValue(map[string]any{"not": map[string]any{"match-regexp": "("}}, "anything")
	if got.Passed {
		t.Fatalf("expected not to refuse to invert an invalid-regexp config error, got %+v", got)
	}

	got = matchValue(map[string]any{"not": map[string]any{"semver-constraint": "not a constraint??"}}, "1.0.0")
	if got.Passed {
		t.Fatalf("expected not to refuse to invert an invalid-semver-constraint config error, got %+v", got)
	}

	// The /re/ slash syntax reports compile errors too, not only the
	// match-regexp operator, so not: "/(/" must not invert into a pass.
	got = matchValue(map[string]any{"not": "/(/"}, "anything")
	if got.Passed {
		t.Fatalf("expected not to refuse to invert an invalid slash-regexp config error, got %+v", got)
	}

	// A broken branch anywhere in an or must survive to the enclosing not,
	// even when a later branch is a plain (non-error) mismatch.
	got = matchValue(map[string]any{"not": map[string]any{"or": []any{map[string]any{"match-regexp": "("}, "no-such-value"}}}, "anything")
	if got.Passed {
		t.Fatalf("expected not to refuse to invert an or containing a config error, got %+v", got)
	}
}

// TestMatchValueConfigErrorsAreMarked guards against matcher config errors
// losing their IsError marker, which not/and/or rely on to avoid treating
// them as ordinary mismatches.
func TestMatchValueConfigErrorsAreMarked(t *testing.T) {
	got := matchValue(map[string]any{"match-regexp": "("}, "anything")
	if got.Passed || !got.IsError {
		t.Fatalf("expected invalid regexp to be a marked error failure, got %+v", got)
	}

	got = matchValue(map[string]any{"semver-constraint": "not a constraint??"}, "1.0.0")
	if got.Passed || !got.IsError {
		t.Fatalf("expected invalid semver constraint to be a marked error failure, got %+v", got)
	}

	// A genuine mismatch (not a config error) must not be marked.
	got = matchValue(map[string]any{"match-regexp": "^abc$"}, "xyz")
	if got.Passed || got.IsError {
		t.Fatalf("expected a genuine regexp mismatch to not be marked as an error, got %+v", got)
	}
}

// TestMatchValueAdvancedMatchersNegativeCases mirrors
// TestMatchValueAdvancedMatchers' operator coverage but with genuine
// mismatches: the positive-only table there could not have caught a matcher
// that always returns Passed: true regardless of input.
func TestMatchValueAdvancedMatchersNegativeCases(t *testing.T) {
	tests := []struct {
		name     string
		expected any
		actual   any
	}{
		{name: "equal", expected: map[string]any{"equal": []any{"a", "b"}}, actual: []string{"a", "c"}},
		{name: "have-prefix", expected: map[string]any{"have-prefix": "v"}, actual: "1.2.3"},
		{name: "have-suffix", expected: map[string]any{"have-suffix": ".0"}, actual: "1.1"},
		{name: "contain-substring", expected: map[string]any{"contain-substring": "err"}, actual: "stdout"},
		{name: "match-regexp", expected: map[string]any{"match-regexp": "^ab[0-9]+$"}, actual: "abc"},
		{name: "gt", expected: map[string]any{"gt": 2}, actual: 2},
		{name: "ge", expected: map[string]any{"ge": 2}, actual: 1},
		{name: "lt", expected: map[string]any{"lt": 10}, actual: 10},
		{name: "le", expected: map[string]any{"le": 10}, actual: 11},
		{name: "contain-element", expected: map[string]any{"contain-element": "z"}, actual: []string{"a", "b"}},
		{name: "contain-elements", expected: map[string]any{"contain-elements": []any{"a", "z"}}, actual: []string{"b", "a", "c"}},
		{name: "consist-of", expected: map[string]any{"consist-of": []any{"a", "b"}}, actual: []string{"a", "b", "c"}},
		{name: "have-len", expected: map[string]any{"have-len": 2}, actual: []string{"a", "b", "c"}},
		{name: "have-key", expected: map[string]any{"have-key": "missing"}, actual: map[string]any{"name": "ok"}},
		{name: "semver", expected: map[string]any{"semver-constraint": ">= 1.2.0, < 2.0.0"}, actual: "2.0.0"},
		{name: "gjson", expected: map[string]any{"gjson": map[string]any{"items.#": map[string]any{"gt": 0}}}, actual: `{"items":[]}`},
		{name: "and or not", expected: map[string]any{"and": []any{map[string]any{"not": 2}, map[string]any{"or": []any{0, 1}}}}, actual: 2},
		// Type mismatches: the matcher must fail (not panic or vacuously
		// pass) when actual is not the kind of value the operator expects.
		{name: "gt on non-numeric", expected: map[string]any{"gt": 2}, actual: "not a number"},
		{name: "have-prefix on non-string", expected: map[string]any{"have-prefix": "v"}, actual: 123},
		{name: "contain-element on non-slice", expected: map[string]any{"contain-element": "a"}, actual: "a"},
		{name: "have-key on non-map", expected: map[string]any{"have-key": "name"}, actual: []string{"name"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchValue(tt.expected, tt.actual)
			if got.Passed {
				t.Fatalf("expected a mismatch to fail, got Passed=true (expected=%v actual=%v)", tt.expected, tt.actual)
			}
		})
	}
}

// TestMatchValueEmptyAndUnicodePatterns covers two edge cases the
// wantPass-only table never exercised: an empty pattern/string and non-ASCII
// content, both plausible in real job output.
func TestMatchValueEmptyAndUnicodePatterns(t *testing.T) {
	if !matchValue(map[string]any{"contain-substring": ""}, "anything").Passed {
		t.Fatal("an empty contain-substring should match any string")
	}
	if matchValue(map[string]any{"equal": ""}, "not empty").Passed {
		t.Fatal("empty equal should not match a non-empty string")
	}
	if !matchValue(map[string]any{"equal": ""}, "").Passed {
		t.Fatal("empty equal should match an empty string")
	}

	if !matchValue(map[string]any{"contain-substring": "日本語"}, "hello 日本語 world").Passed {
		t.Fatal("unicode contain-substring should match")
	}
	// have-len matches Go's own len() semantics for strings: byte length, not
	// rune count. "日本語" is 3 runes but 9 bytes (3 bytes per CJK character).
	if !matchValue(map[string]any{"have-len": 9}, "日本語").Passed {
		t.Fatal("have-len should count bytes, consistent with Go's len() on a string")
	}
	if !matchValue("/^日本語$/", "日本語").Passed {
		t.Fatal("unicode regexp should match")
	}
}

// TestConsistOfRejectsDuplicateMismatch guards against consist-of's
// used-item tracking allowing a duplicated expected value to match the same
// actual element twice, which would let ["a", "a"] wrongly pass against a
// single-element ["a"] actual (same class of bug as the bare-list and
// contain-elements duplicate-reuse tests above, for the exact-match operator).
func TestConsistOfRejectsDuplicateMismatch(t *testing.T) {
	got := matchValue(map[string]any{"consist-of": []any{"a", "a"}}, []string{"a"})
	if got.Passed {
		t.Fatalf("expected duplicated consist-of item to require two actual occurrences, got %+v", got)
	}

	got = matchValue(map[string]any{"consist-of": []any{"a", "a"}}, []string{"a", "a"})
	if !got.Passed {
		t.Fatalf("expected two actual occurrences to satisfy two duplicated consist-of items, got %+v", got)
	}
}

func TestMatchValueDirectArraySubset(t *testing.T) {
	got := matchValue([]any{"a", "c"}, []string{"a", "b", "c"})
	if !got.Passed {
		t.Fatalf("expected subset match to pass, got %+v", got)
	}
}
