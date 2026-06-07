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
	"jobs":                "/jobs",
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
	"jobs":                "id",
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
			"assets": map[string]any{
				"count": 0,
				"links": []any{},
			},
		}
	case "merge_requests":
		return map[string]any{
			"iid":                 0,
			"title":               "",
			"state":               "opened",
			"labels":              []any{},
			"source_branch":       "",
			"target_branch":       "",
			"web_url":             "",
			"author":              map[string]any{"id": 1, "username": "test-user"},
			"draft":               false,
			"merge_status":        "can_be_merged",
			"merge_commit_sha":    nil,
		}
	case "repository/tags":
		return map[string]any{
			"name":    "",
			"message": "",
			"commit": map[string]any{
				"id": "",
			},
		}
	case "repository/branches":
		return map[string]any{
			"name":      "",
			"protected": false,
			"merged":    false,
			"default":   false,
			"commit": map[string]any{
				"id": "",
			},
		}
	case "labels":
		return map[string]any{
			"id":    0,
			"name":  "",
			"color": "#000000",
		}
	case "variables":
		return map[string]any{
			"key":              "",
			"value":            "",
			"variable_type":    "env_var",
			"protected":        false,
			"masked":           false,
			"environment_scope": "*",
		}
	case "issues":
		return map[string]any{
			"iid":    0,
			"title":  "",
			"state":  "opened",
			"labels": []any{},
			"web_url": "",
			"author": map[string]any{"id": 1, "username": "test-user"},
		}
	case "pipelines":
		return map[string]any{
			"id":         0,
			"status":     "success",
			"ref":        "",
			"sha":        "",
			"web_url":    "",
			"created_at": "",
			"updated_at": "",
		}
	case "jobs":
		return map[string]any{
			"id":         0,
			"name":       "",
			"stage":      "",
			"status":     "success",
			"ref":        "",
			"web_url":    "",
			"created_at": "",
		}
	case "environments":
		return map[string]any{
			"id":           0,
			"name":         "",
			"slug":         "",
			"state":        "available",
			"external_url": nil,
		}
	case "deployments":
		return map[string]any{
			"id":          0,
			"status":      "success",
			"ref":         "",
			"sha":         "",
			"created_at":  "",
			"updated_at":  "",
		}
	default:
		return map[string]any{
			identifierFor(resource): 0,
		}
	}
}
