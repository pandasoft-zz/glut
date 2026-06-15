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
				Executed:   true,
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
				Executed:   true,
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
				Executed:   true,
			},
		},
	})

	for _, result := range results {
		if !result.Passed {
			t.Fatalf("unexpected failure: %+v", result)
		}
	}
}

func TestRunJobAssertsWhen(t *testing.T) {
	presentTrue := true
	t.Run("matches evaluated when", func(t *testing.T) {
		asserts := config.AssertConfig{
			Job: map[string]config.JobAssert{
				"release": {Present: &presentTrue, When: "manual"},
			},
		}
		results := Run(asserts, AssertContext{
			JobOutputs: map[string]executor.JobOutput{
				"release": {Present: true, When: "manual"},
			},
		})
		for _, result := range results {
			if !result.Passed {
				t.Fatalf("unexpected failure: %+v", result)
			}
		}
	})

	t.Run("fails on when mismatch", func(t *testing.T) {
		asserts := config.AssertConfig{
			Job: map[string]config.JobAssert{
				"release": {When: "manual"},
			},
		}
		results := Run(asserts, AssertContext{
			JobOutputs: map[string]executor.JobOutput{
				"release": {Present: true, When: "on_success", Executed: true},
			},
		})
		failed := 0
		for _, result := range results {
			if !result.Passed {
				failed++
				if result.Path != `assert.job."release".when` {
					t.Fatalf("unexpected failing path: %+v", result)
				}
			}
		}
		if failed != 1 {
			t.Fatalf("expected one failing assertion, got %+v", results)
		}
	})

	t.Run("absent job fails on present only", func(t *testing.T) {
		asserts := config.AssertConfig{
			Job: map[string]config.JobAssert{
				"release": {When: "manual"},
			},
		}
		results := Run(asserts, AssertContext{
			JobOutputs: map[string]executor.JobOutput{},
		})
		if len(results) != 1 || results[0].Passed {
			t.Fatalf("expected single .present failure, got %+v", results)
		}
	})
}

func TestRunJobAssertsFailFieldAssertsOnNotExecutedJob(t *testing.T) {
	// A present-but-not-executed job (when: manual) has zero-value outputs;
	// exit-status: 0 or negation patterns must not pass against them.
	asserts := config.AssertConfig{
		Job: map[string]config.JobAssert{
			"release": {ExitStatus: 0, Stdout: []any{"!FATAL"}},
		},
	}
	results := Run(asserts, AssertContext{
		JobOutputs: map[string]executor.JobOutput{
			"release": {Present: true, When: "manual"},
		},
	})
	failed := 0
	for _, result := range results {
		if !result.Passed {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("expected one failing assertion for not-executed job, got %+v", results)
	}
}
