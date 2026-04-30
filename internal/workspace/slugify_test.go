package workspace

import "testing"

func TestSlugify(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"feature/my-feature", "feature-my-feature"},
		{"1.2.0", "1-2-0"},
		{"main", "main"},
		{"fix/Bug_123", "fix-bug-123"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := slugify(tt.input); got != tt.expected {
				t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
