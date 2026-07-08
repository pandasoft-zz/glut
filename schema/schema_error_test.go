package schema

import "testing"

func TestValidateGlutRejectsUnmarshalableInput(t *testing.T) {
	t.Parallel()
	// A function value cannot be marshaled to JSON, so validation itself must
	// fail — this pins the error branch, not a validation-errors result.
	if _, err := ValidateGlut(map[string]interface{}{"name": func() {}}); err == nil {
		t.Fatal("ValidateGlut() with unmarshalable input must return an error")
	}
}
