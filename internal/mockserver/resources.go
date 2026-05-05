package mockserver

var resourceBases = map[string]string{
	"releases":            "/releases",
	"merge_requests":      "/merge_requests",
	"repository/tags":     "/repository/tags",
	"repository/branches": "/repository/branches",
	"labels":              "/labels",
	"milestones":          "/milestones",
	"issues":              "/issues",
	"hooks":               "/hooks",
	"variables":           "/variables",
	"deployments":         "/deployments",
	"environments":        "/environments",
	"pipelines":           "/pipelines",
}

var resourceIdentifiers = map[string]string{
	"releases":            "tag_name",
	"merge_requests":      "iid",
	"repository/tags":     "name",
	"repository/branches": "name",
	"labels":              "id",
	"milestones":          "id",
	"issues":              "iid",
	"hooks":               "id",
	"variables":           "key",
	"deployments":         "id",
	"environments":        "id",
	"pipelines":           "id",
}

func identifierFor(resource string) string {
	if identifier, ok := resourceIdentifiers[resource]; ok {
		return identifier
	}
	return "id"
}

func defaultObject(resource string) map[string]any {
	switch resource {
	case "releases":
		return map[string]any{
			"tag_name":    "",
			"name":        "",
			"description": "",
		}
	case "merge_requests":
		return map[string]any{
			"iid":    0,
			"title":  "",
			"state":  "opened",
			"labels": []any{},
		}
	case "repository/tags":
		return map[string]any{
			"name":    "",
			"message": "",
		}
	case "repository/branches":
		return map[string]any{
			"name":      "",
			"protected": false,
		}
	case "labels":
		return map[string]any{
			"id":    0,
			"name":  "",
			"color": "#000000",
		}
	case "variables":
		return map[string]any{
			"key":   "",
			"value": "",
		}
	default:
		return map[string]any{
			identifierFor(resource): 0,
		}
	}
}
