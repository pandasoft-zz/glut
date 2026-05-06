package schema

import (
	"strings"
	"testing"
)

func TestValidateGlutValid(t *testing.T) {
	value := map[string]any{
		"name": "full",
		"setup": map[string]any{
			"branch":          "main",
			"pipeline_source": "push",
			"mocks": map[string]any{
				"binaries": map[string]any{
					"release-cli": map[string]any{
						"executable": "#!/bin/sh\necho ok\n",
					},
				},
			},
		},
		"assert": map[string]any{
			"job": map[string]any{
				"build:image": map[string]any{
					"exit-status": 0,
					"present":     true,
					"stdout":      []any{"Building image", "!FATAL", "/tag: [a-z0-9]+/"},
					"stderr":      map[string]any{"not": "panic"},
				},
			},
			"artifacts": map[string]any{
				"output.json": map[string]any{
					"exists": true,
					"contents": map[string]any{
						"gjson": map[string]any{
							"items.#": map[string]any{"gt": 0},
						},
					},
					"filetype": "file",
				},
			},
			"git": map[string]any{
				"workspace": map[string]any{
					"branch": "feature/test",
					"clean":  true,
					"last-commit": map[string]any{
						"message": map[string]any{"match-regexp": "^feat:"},
					},
				},
			},
			"api": map[string]any{
				"POST /api/v4/projects/*/releases": map[string]any{
					"called": true,
					"times":  1,
					"body": map[string]any{
						"labels": map[string]any{
							"contain-elements": []any{"frontend", "ready"},
						},
						"gjson": map[string]any{
							"assets.links.#": map[string]any{"gt": 0},
						},
					},
				},
			},
			"binary": map[string]any{
				"release-cli": map[string]any{
					"called": true,
					"times":  map[string]any{"ge": 1},
					"calls": []any{
						map[string]any{
							"args": map[string]any{"contain-element": "semantic-release"},
							"cwd":  map[string]any{"have-suffix": "/builds"},
						},
					},
					"never-called-with": map[string]any{
						"args": map[string]any{"contain-element": "--dry-run"},
					},
				},
			},
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
	tests := []struct {
		name        string
		value       map[string]any
		wantSnippet string
	}{
		{
			name: "invalid root and setup fields",
			value: map[string]any{
				"name": "bad",
				"setup": map[string]any{
					"branch":          "main",
					"tag":             "v1.0.0",
					"pipeline_source": "unknown",
				},
				"extra": "nope",
			},
			wantSnippet: "additional property",
		},
		{
			name: "invalid matcher shape",
			value: map[string]any{
				"assert": map[string]any{
					"job": map[string]any{
						"build": map[string]any{
							"exit-status": map[string]any{
								"gt": "zero",
							},
						},
					},
				},
			},
			wantSnippet: "invalid",
		},
		{
			name: "invalid artifact filetype",
			value: map[string]any{
				"assert": map[string]any{
					"artifacts": map[string]any{
						"out.txt": map[string]any{
							"filetype": "fifo",
						},
					},
				},
			},
			wantSnippet: "must be one of",
		},
		{
			name: "invalid binary call field",
			value: map[string]any{
				"assert": map[string]any{
					"binary": map[string]any{
						"release-cli": map[string]any{
							"calls": []any{
								map[string]any{
									"bad": true,
								},
							},
						},
					},
				},
			},
			wantSnippet: "additional property",
		},
		{
			name: "invalid mocks executable type",
			value: map[string]any{
				"setup": map[string]any{
					"mocks": map[string]any{
						"binaries": map[string]any{
							"release-cli": map[string]any{
								"executable": true,
							},
						},
					},
				},
			},
			wantSnippet: "invalid type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs, err := ValidateGlut(tt.value)
			if err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
			if len(errs) == 0 {
				t.Fatal("expected schema errors")
			}
			if tt.wantSnippet != "" && !hasErrorSnippet(errs, tt.wantSnippet) {
				t.Fatalf("expected error containing %q, got %v", tt.wantSnippet, errs)
			}
		})
	}
}

func TestFormatValidationError(t *testing.T) {
	tests := []struct {
		name string
		err  ValidationError
		want string
	}{
		{
			name: "root",
			err: ValidationError{
				Field:       "(root)",
				Description: "must be valid",
			},
			want: "must be valid",
		},
		{
			name: "field",
			err: ValidationError{
				Field:       "assert.job",
				Description: "is invalid",
			},
			want: "assert.job: is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatValidationError(tt.err)
			if got != tt.want {
				t.Fatalf("FormatValidationError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func hasErrorSnippet(errs []ValidationError, snippet string) bool {
	for _, err := range errs {
		if strings.Contains(strings.ToLower(err.Description), strings.ToLower(snippet)) {
			return true
		}
		if strings.Contains(strings.ToLower(err.Field), strings.ToLower(snippet)) {
			return true
		}
	}
	return false
}
