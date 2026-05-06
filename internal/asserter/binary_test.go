package asserter

import (
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/mockwrapper"
)

func TestRunBinaryAsserts(t *testing.T) {
	asserts := config.AssertConfig{
		Binary: map[string]config.BinaryAssert{
			"release-cli": {
				Called: boolPtr(true),
				Times:  2,
				Calls: []config.BinaryCallAssert{
					{Args: map[string]any{"contain-element": "semantic-release"}},
					{Args: []any{"--version"}},
				},
				NeverCalledWith: &config.BinaryCallAssert{
					Args: map[string]any{"contain-element": "--dry-run"},
				},
			},
		},
	}

	results := Run(asserts, AssertContext{
		BinaryLogs: map[string][]mockwrapper.BinaryCall{
			"release-cli": {
				{Args: []string{"semantic-release"}, CWD: "/builds/project"},
				{Args: []string{"--version"}, CWD: "/builds/project"},
			},
		},
	})

	for _, result := range results {
		if !result.Passed {
			t.Fatalf("unexpected failure: %+v", result)
		}
	}
}

func TestRunBinaryAssertsFailOnNeverCalledWith(t *testing.T) {
	asserts := config.AssertConfig{
		Binary: map[string]config.BinaryAssert{
			"release-cli": {
				NeverCalledWith: &config.BinaryCallAssert{
					Args: map[string]any{"contain-element": "--dry-run"},
				},
			},
		},
	}

	results := Run(asserts, AssertContext{
		BinaryLogs: map[string][]mockwrapper.BinaryCall{
			"release-cli": {
				{Args: []string{"semantic-release", "--dry-run"}},
			},
		},
	})

	if len(results) != 1 || results[0].Passed {
		t.Fatalf("expected one failed result, got %+v", results)
	}
}
