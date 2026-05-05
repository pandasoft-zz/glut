package asserter

import (
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
	"github.com/pandasoft-zz/glut/internal/mockserver"
)

func TestRunAPIAsserts(t *testing.T) {
	asserts := config.AssertConfig{
		API: map[string]config.APICallAssert{
			"POST /api/v4/projects/*/releases": {
				Called: boolPtr(true),
				Times:  1,
				Body: map[string]any{
					"name": "/.+/",
					"labels": map[string]any{
						"contain-elements": []any{"frontend", "ready"},
					},
					"gjson": map[string]any{
						"assets.links.#": map[string]any{"gt": 0},
					},
				},
			},
			"DELETE /api/v4/projects/*/releases/*": {
				Called: boolPtr(false),
			},
		},
	}

	results := Run(asserts, AssertContext{
		APICalls: []mockserver.APICall{
			{
				Method:      "POST",
				Path:        "/api/v4/projects/1/releases",
				RequestBody: []byte(`{"name":"release","labels":["frontend","ready"],"assets":{"links":[{"name":"binary"}]}}`),
			},
		},
	})

	for _, result := range results {
		if !result.Passed {
			t.Fatalf("unexpected failure: %+v", result)
		}
	}
}
