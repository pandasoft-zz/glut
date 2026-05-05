package asserter

import "testing"

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
