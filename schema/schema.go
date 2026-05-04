package schema

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

//go:embed glut.schema.json
var glutSchema string

type ValidationError struct {
	Field       string
	Description string
}

func ValidateGlut(value interface{}) ([]ValidationError, error) {
	schemaLoader := gojsonschema.NewStringLoader(glutSchema)
	documentLoader := gojsonschema.NewGoLoader(value)

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return nil, fmt.Errorf("failed to validate glut schema: %w", err)
	}
	if result.Valid() {
		return nil, nil
	}

	errors := make([]ValidationError, 0, len(result.Errors()))
	for _, validationErr := range result.Errors() {
		errors = append(errors, ValidationError{
			Field:       validationErr.Field(),
			Description: validationErr.Description(),
		})
	}
	return errors, nil
}

func FormatValidationError(validationErr ValidationError) string {
	if validationErr.Field == "" || validationErr.Field == "(root)" {
		return validationErr.Description
	}
	return strings.TrimSpace(validationErr.Field + ": " + validationErr.Description)
}
