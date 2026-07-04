package workspace

import (
	"strings"
	"testing"
)

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

// TestSlugifyTruncatesToGitLabLimit guards against CI_COMMIT_REF_SLUG/
// CI_PROJECT_PATH_SLUG diverging from real GitLab for a long branch name:
// real GitLab truncates *_SLUG values to 63 bytes, and slugify implemented
// every other transform except that truncation.
func TestSlugifyTruncatesToGitLabLimit(t *testing.T) {
	longBranch := "feature/" + strings.Repeat("a", 100)
	got := slugify(longBranch)
	if len(got) > 63 {
		t.Fatalf("slugify(%q) = %q (%d bytes), want at most 63 bytes", longBranch, got, len(got))
	}
	want := "feature-" + strings.Repeat("a", 55)
	if got != want {
		t.Fatalf("slugify(%q) = %q, want %q", longBranch, got, want)
	}

	// A truncation that lands exactly on a trailing separator must still be
	// trimmed, matching the untruncated trim-dash behavior.
	trailingDash := strings.Repeat("a", 62) + "/b"
	if got := slugify(trailingDash); strings.HasSuffix(got, "-") {
		t.Fatalf("slugify(%q) = %q, must not end with a trailing dash after truncation", trailingDash, got)
	}
}
