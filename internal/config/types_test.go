package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestStringSliceUnmarshal(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  StringSlice
	}{
		{
			name:  "sequence of strings",
			input: "- cmd1\n- cmd2\n",
			want:  StringSlice{"cmd1", "cmd2"},
		},
		{
			name:  "literal block scalar |",
			input: "|\n  cmd1\n  cmd2\n",
			want:  StringSlice{"cmd1\ncmd2\n"},
		},
		{
			name:  "folded block scalar >",
			input: ">\n  single command\n",
			want:  StringSlice{"single command\n"},
		},
		{
			name:  "literal strip |-",
			input: "|-\n  cmd1\n  cmd2",
			want:  StringSlice{"cmd1\ncmd2"},
		},
		{
			name:  "plain inline string",
			input: `"api"`,
			want:  StringSlice{"api"},
		},
		{
			name:  "empty string",
			input: `""`,
			want:  nil,
		},
	}

	errorCases := []struct {
		name  string
		input string
	}{
		{
			name:  "mapping node returns error",
			input: "key: value\n",
		},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			var got StringSlice
			if err := yaml.Unmarshal([]byte(tc.input), &got); err == nil {
				t.Fatal("expected error for mapping node, got nil")
			}
		})
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got StringSlice
			if err := yaml.Unmarshal([]byte(tc.input), &got); err != nil {
				t.Fatalf("UnmarshalYAML(%q) error = %v", tc.input, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d; got %v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
