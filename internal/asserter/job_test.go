package asserter

import (
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/executor"
)

func TestRunJobAsserts(t *testing.T) {
	presentFalse := false
	asserts := config.AssertConfig{
		Job: map[string]config.JobAssert{
			"build:image": {
				ExitStatus: 0,
				Stdout:     []any{"Building image", "!FATAL", "/tag: [a-z0-9]+/"},
				Stderr:     []any{"!/^Error:/"},
			},
			"skip-me": {
				Present: &presentFalse,
			},
		},
	}

	results := Run(asserts, AssertContext{
		JobOutputs: map[string]executor.JobOutput{
			"build:image": {
				ExitStatus: 0,
				Stdout:     "Building image\ntag: abc123\n",
				Stderr:     "warn only\n",
			},
		},
	})

	for _, result := range results {
		if !result.Passed {
			t.Fatalf("unexpected failure: %+v", result)
		}
	}
}
