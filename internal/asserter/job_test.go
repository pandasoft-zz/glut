package asserter

import (
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/executor"
)

func TestRunJobAssertsFailsWhenJobNotPresent(t *testing.T) {
	asserts := config.AssertConfig{
		Job: map[string]config.JobAssert{
			"missing-job": {},
		},
	}

	results := Run(asserts, AssertContext{
		JobOutputs: map[string]executor.JobOutput{},
	})

	if len(results) != 1 || results[0].Passed {
		t.Fatalf("expected one failing assertion for non-present job, got %+v", results)
	}
}

func TestRunJobAssertsOutput(t *testing.T) {
	asserts := config.AssertConfig{
		Job: map[string]config.JobAssert{
			"build": {
				Output: []any{"from-stdout", "from-stderr", "!/missing/"},
			},
		},
	}

	results := Run(asserts, AssertContext{
		JobOutputs: map[string]executor.JobOutput{
			"build": {
				ExitStatus: 0,
				Stdout:     "from-stdout\n",
				Stderr:     "from-stderr\n",
			},
		},
	})

	for _, result := range results {
		if !result.Passed {
			t.Fatalf("unexpected failure: %+v", result)
		}
	}
}

func TestRunJobAssertsOutputFailsWhenPatternMissing(t *testing.T) {
	asserts := config.AssertConfig{
		Job: map[string]config.JobAssert{
			"build": {
				Output: []any{"nowhere-to-be-found"},
			},
		},
	}

	results := Run(asserts, AssertContext{
		JobOutputs: map[string]executor.JobOutput{
			"build": {
				ExitStatus: 0,
				Stdout:     "stdout line\n",
				Stderr:     "stderr line\n",
			},
		},
	})

	if len(results) != 1 || results[0].Passed {
		t.Fatalf("expected one failing assertion, got %+v", results)
	}
}

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
