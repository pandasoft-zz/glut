package schema

import "testing"

func TestValidateGlutValid(t *testing.T) {
	value := map[string]interface{}{
		"name": "simple",
		"setup": map[string]interface{}{
			"branch":          "main",
			"pipeline_source": "push",
		},
		"assert": map[string]interface{}{
			"job": map[string]interface{}{},
		},
	}

	errs, err := ValidateGlut(value)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no schema errors, got %v", errs)
	}
}

func TestValidateGlutInvalid(t *testing.T) {
	value := map[string]interface{}{
		"name": "bad",
		"setup": map[string]interface{}{
			"branch":          "main",
			"tag":             "v1.0.0",
			"pipeline_source": "unknown",
		},
		"extra": "nope",
	}

	errs, err := ValidateGlut(value)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("expected schema errors")
	}
}
