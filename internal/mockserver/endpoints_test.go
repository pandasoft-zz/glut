package mockserver

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
)

// --- /api/v4/user and /api/v4/users/:id ---

func TestCurrentUserEndpoint(t *testing.T) {
	t.Run("default user values", func(t *testing.T) {
		server := startTestServer(t, config.APISetupConfig{})

		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/user"), "token", nil)
		defer closeBody(t, resp.Body)

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		body := decodeObject(t, resp.Body)
		if body["id"] != float64(1) {
			t.Errorf("id = %v, want 1", body["id"])
		}
		if body["username"] != config.DefaultUserLogin {
			t.Errorf("username = %v, want %q", body["username"], config.DefaultUserLogin)
		}
		if body["email"] != config.DefaultUserEmail {
			t.Errorf("email = %v, want %q", body["email"], config.DefaultUserEmail)
		}
		if body["state"] != "active" {
			t.Errorf("state = %v, want active", body["state"])
		}
	})

	t.Run("custom user config", func(t *testing.T) {
		server := startTestServer(t, config.APISetupConfig{
			User: &config.UserConfig{
				ID:    42,
				Name:  "Alice",
				Email: "alice@example.com",
				Login: "alice",
			},
		})

		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/user"), "token", nil)
		defer closeBody(t, resp.Body)

		body := decodeObject(t, resp.Body)
		if body["id"] != float64(42) {
			t.Errorf("id = %v, want 42", body["id"])
		}
		if body["username"] != "alice" {
			t.Errorf("username = %v, want alice", body["username"])
		}
	})

	t.Run("user endpoint requires auth", func(t *testing.T) {
		server := startTestServer(t, config.APISetupConfig{})

		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/user"), "", nil)
		defer closeBody(t, resp.Body)

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})
}

func TestUserByIDEndpoint(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{
		User: &config.UserConfig{ID: 7, Name: "Bob", Login: "bob", Email: "bob@example.com"},
	})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/users/7"), "token", nil)
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodeObject(t, resp.Body)
	if body["id"] != float64(7) {
		t.Errorf("id = %v, want 7", body["id"])
	}
	if body["username"] != "bob" {
		t.Errorf("username = %v, want bob", body["username"])
	}
}

// --- /api/v4/groups/:id ---

func TestGroupEndpoint(t *testing.T) {
	t.Run("default group derived from project path", func(t *testing.T) {
		server := startTestServer(t, config.APISetupConfig{})

		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/groups/1"), "token", nil)
		defer closeBody(t, resp.Body)

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		body := decodeObject(t, resp.Body)
		if body["full_path"] != "test-group" {
			t.Errorf("full_path = %v, want test-group", body["full_path"])
		}
	})

	t.Run("custom group config", func(t *testing.T) {
		server := startTestServer(t, config.APISetupConfig{
			Group: &config.GroupConfig{
				ID:   99,
				Path: "platform/infra",
				Name: "Infra Team",
			},
		})

		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/groups/99"), "token", nil)
		defer closeBody(t, resp.Body)

		body := decodeObject(t, resp.Body)
		if body["id"] != float64(99) {
			t.Errorf("id = %v, want 99", body["id"])
		}
		if body["full_path"] != "platform/infra" {
			t.Errorf("full_path = %v, want platform/infra", body["full_path"])
		}
		if body["name"] != "Infra Team" {
			t.Errorf("name = %v, want Infra Team", body["name"])
		}
	})

	t.Run("group requires auth", func(t *testing.T) {
		server := startTestServer(t, config.APISetupConfig{})

		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/groups/1"), "", nil)
		defer closeBody(t, resp.Body)

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})
}

// --- Richer project response ---

func TestProjectResponseHasRicherFields(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{
		Project: &config.ProjectConfig{Path: "myorg/myapp", DefaultBranch: "trunk"},
	})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1"), "token", nil)
	defer closeBody(t, resp.Body)

	body := decodeObject(t, resp.Body)

	for _, field := range []string{"web_url", "http_url_to_repo", "ssh_url_to_repo", "visibility", "namespace", "created_at", "star_count"} {
		if _, ok := body[field]; !ok {
			t.Errorf("project response missing field %q", field)
		}
	}
	ns, _ := body["namespace"].(map[string]any)
	if ns["full_path"] != "myorg" {
		t.Errorf("namespace.full_path = %v, want myorg", ns["full_path"])
	}
}

func TestProjectWebURLMatchesMockServer(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1"), "token", nil)
	defer closeBody(t, resp.Body)

	body := decodeObject(t, resp.Body)
	webURL, _ := body["web_url"].(string)
	want := serverURL(server, "/test-group/test-project")
	if webURL != want {
		t.Errorf("web_url = %q, want %q", webURL, want)
	}
	httpURL, _ := body["http_url_to_repo"].(string)
	wantHTTP := serverURL(server, "/test-group/test-project.git")
	if httpURL != wantHTTP {
		t.Errorf("http_url_to_repo = %q, want %q", httpURL, wantHTTP)
	}
}

func TestReadmeURLUsesDefaultBranch(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{
		Project: &config.ProjectConfig{DefaultBranch: "trunk"},
	})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1"), "token", nil)
	defer closeBody(t, resp.Body)

	body := decodeObject(t, resp.Body)
	readmeURL, _ := body["readme_url"].(string)
	if !contains(readmeURL, "/trunk/") {
		t.Errorf("readme_url = %q, should contain /trunk/", readmeURL)
	}
}

// --- Richer token self response ---

func TestTokenSelfHasRicherFields(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{
		User: &config.UserConfig{ID: 5},
	})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/personal_access_tokens/self"), "token", nil)
	defer closeBody(t, resp.Body)

	body := decodeObject(t, resp.Body)
	if body["user_id"] != float64(5) {
		t.Errorf("user_id = %v, want 5", body["user_id"])
	}
	if _, ok := body["created_at"]; !ok {
		t.Error("token self response missing created_at")
	}
	if _, ok := body["last_used_at"]; !ok {
		t.Error("token self response missing last_used_at")
	}
}

// --- Pagination headers ---

func TestListResponseIncludesPaginationHeaders(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{
		Seed: &config.APISeedConfig{
			Labels: []map[string]any{
				{"id": 1, "name": "bug"},
				{"id": 2, "name": "feature"},
			},
		},
	})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/labels"), "token", nil)
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Total") != "2" {
		t.Errorf("X-Total = %q, want 2", resp.Header.Get("X-Total"))
	}
	if resp.Header.Get("X-Total-Pages") != "1" {
		t.Errorf("X-Total-Pages = %q, want 1", resp.Header.Get("X-Total-Pages"))
	}
	if resp.Header.Get("X-Page") != "1" {
		t.Errorf("X-Page = %q, want 1", resp.Header.Get("X-Page"))
	}
	if resp.Header.Get("X-Per-Page") != "100" {
		t.Errorf("X-Per-Page = %q, want 100", resp.Header.Get("X-Per-Page"))
	}
}

func TestEmptyListPaginationHeaders(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/releases"), "token", nil)
	defer closeBody(t, resp.Body)

	if resp.Header.Get("X-Total") != "0" {
		t.Errorf("X-Total for empty list = %q, want 0", resp.Header.Get("X-Total"))
	}
}

// --- MR sub-endpoints ---

func TestMergeRequestNotesStoredAndRetrieved(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	t.Run("GET on fresh MR returns empty list", func(t *testing.T) {
		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/merge_requests/5/notes"), "token", nil)
		defer closeBody(t, resp.Body)

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var notes []any
		if err := json.NewDecoder(resp.Body).Decode(&notes); err != nil {
			t.Fatal(err)
		}
		if len(notes) != 0 {
			t.Errorf("expected empty list, got %d notes", len(notes))
		}
	})

	t.Run("POST stores note, subsequent GET returns it", func(t *testing.T) {
		postResp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/merge_requests/5/notes"), "token", map[string]any{
			"body": "first comment",
		})
		defer closeBody(t, postResp.Body)

		if postResp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", postResp.StatusCode)
		}
		created := decodeObject(t, postResp.Body)
		if created["body"] != "first comment" {
			t.Errorf("note body = %v", created["body"])
		}
		if created["id"] != float64(1) {
			t.Errorf("first note id = %v, want 1", created["id"])
		}

		getResp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/merge_requests/5/notes"), "token", nil)
		defer closeBody(t, getResp.Body)

		var notes []map[string]any
		if err := json.NewDecoder(getResp.Body).Decode(&notes); err != nil {
			t.Fatal(err)
		}
		if len(notes) != 1 {
			t.Fatalf("expected 1 note, got %d", len(notes))
		}
		if notes[0]["body"] != "first comment" {
			t.Errorf("retrieved note body = %v", notes[0]["body"])
		}
	})

	t.Run("second POST gets auto-incremented ID", func(t *testing.T) {
		postResp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/merge_requests/5/notes"), "token", map[string]any{
			"body": "second comment",
		})
		defer closeBody(t, postResp.Body)

		created := decodeObject(t, postResp.Body)
		if created["id"] != float64(2) {
			t.Errorf("second note id = %v, want 2", created["id"])
		}
	})

	t.Run("notes are isolated per MR iid", func(t *testing.T) {
		doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/merge_requests/9/notes"), "token", map[string]any{
			"body": "note on MR 9",
		})

		respMR5 := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/merge_requests/5/notes"), "token", nil)
		defer closeBody(t, respMR5.Body)
		var notesMR5 []any
		_ = json.NewDecoder(respMR5.Body).Decode(&notesMR5)

		respMR9 := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/merge_requests/9/notes"), "token", nil)
		defer closeBody(t, respMR9.Body)
		var notesMR9 []any
		_ = json.NewDecoder(respMR9.Body).Decode(&notesMR9)

		if len(notesMR5) == len(notesMR9) && len(notesMR9) == 0 {
			t.Error("notes were not isolated between MRs")
		}
		if len(notesMR9) != 1 {
			t.Errorf("MR 9 notes = %d, want 1", len(notesMR9))
		}
	})
}

func TestMergeRequestApprovals(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/merge_requests/3/approvals"), "token", nil)
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodeObject(t, resp.Body)
	if body["approved"] != false {
		t.Errorf("approved = %v, want false", body["approved"])
	}
	if _, ok := body["approvals_required"]; !ok {
		t.Error("missing approvals_required")
	}
}

func TestMergeRequestUnapprove(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	resp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/merge_requests/3/unapprove"), "token", nil)
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	body := decodeObject(t, resp.Body)
	if body["approved"] != false {
		t.Errorf("approved = %v, want false", body["approved"])
	}
}

func TestMergeRequestMerge(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	resp := doRequest(t, http.MethodPut, serverURL(server, "/api/v4/projects/1/merge_requests/3/merge"), "token", nil)
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodeObject(t, resp.Body)
	if body["state"] != "merged" {
		t.Errorf("state = %v, want merged", body["state"])
	}
	if body["merge_commit_sha"] == nil || body["merge_commit_sha"] == "" {
		t.Error("merge_commit_sha should be set")
	}
}

func TestMergeRequestChanges(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/merge_requests/3/changes"), "token", nil)
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodeObject(t, resp.Body)
	if _, ok := body["changes"]; !ok {
		t.Error("missing changes field")
	}
}

func TestMergeRequestDiscussions(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/merge_requests/3/discussions"), "token", nil)
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var discussions []any
	if err := json.NewDecoder(resp.Body).Decode(&discussions); err != nil {
		t.Fatal(err)
	}
}

// --- Issue notes ---

func TestIssueNotesStoredAndRetrieved(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	t.Run("GET on fresh issue returns empty list", func(t *testing.T) {
		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/issues/1/notes"), "token", nil)
		defer closeBody(t, resp.Body)

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var notes []any
		if err := json.NewDecoder(resp.Body).Decode(&notes); err != nil {
			t.Fatal(err)
		}
		if len(notes) != 0 {
			t.Errorf("expected empty list, got %d items", len(notes))
		}
	})

	t.Run("POST stores note, GET retrieves it", func(t *testing.T) {
		postResp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/issues/1/notes"), "token", map[string]any{
			"body": "tracked in backlog",
		})
		defer closeBody(t, postResp.Body)

		if postResp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", postResp.StatusCode)
		}

		getResp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/issues/1/notes"), "token", nil)
		defer closeBody(t, getResp.Body)

		var notes []map[string]any
		if err := json.NewDecoder(getResp.Body).Decode(&notes); err != nil {
			t.Fatal(err)
		}
		if len(notes) != 1 {
			t.Fatalf("expected 1 note, got %d", len(notes))
		}
		if notes[0]["body"] != "tracked in backlog" {
			t.Errorf("note body = %v", notes[0]["body"])
		}
	})

	t.Run("MR and issue note stores are independent", func(t *testing.T) {
		doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/merge_requests/1/notes"), "token", map[string]any{"body": "mr note"})
		doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/issues/1/notes"), "token", map[string]any{"body": "issue note 2"})

		mrResp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/merge_requests/1/notes"), "token", nil)
		defer closeBody(t, mrResp.Body)
		var mrNotes []any
		_ = json.NewDecoder(mrResp.Body).Decode(&mrNotes)

		issueResp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/issues/1/notes"), "token", nil)
		defer closeBody(t, issueResp.Body)
		var issueNotes []any
		_ = json.NewDecoder(issueResp.Body).Decode(&issueNotes)

		if len(mrNotes) != 1 {
			t.Errorf("MR notes = %d, want 1", len(mrNotes))
		}
		// issue already had 1 note from previous subtest, plus 1 more = 2
		if len(issueNotes) != 2 {
			t.Errorf("issue notes = %d, want 2", len(issueNotes))
		}
	})
}

// --- Pipeline sub-endpoints ---

func TestPipelineRetryAndCancel(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	t.Run("retry returns pipeline", func(t *testing.T) {
		resp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/pipelines/5/retry"), "token", nil)
		defer closeBody(t, resp.Body)

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", resp.StatusCode)
		}
		body := decodeObject(t, resp.Body)
		if body["status"] != "pending" {
			t.Errorf("status = %v, want pending", body["status"])
		}
	})

	t.Run("cancel returns pipeline", func(t *testing.T) {
		resp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/pipelines/5/cancel"), "token", nil)
		defer closeBody(t, resp.Body)

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", resp.StatusCode)
		}
	})
}

func TestPipelineTrigger(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	resp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/pipeline"), "token", map[string]any{
		"ref": "main",
	})
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	body := decodeObject(t, resp.Body)
	if body["ref"] != "main" {
		t.Errorf("ref = %v, want main", body["ref"])
	}
	if body["status"] != "pending" {
		t.Errorf("status = %v, want pending", body["status"])
	}
}

func TestPipelineJobsFilteredByPipelineID(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{
		Seed: &config.APISeedConfig{
			Jobs: []map[string]any{
				{"id": 1, "name": "build", "pipeline_id": 10},
				{"id": 2, "name": "test", "pipeline_id": 10},
				{"id": 3, "name": "deploy", "pipeline_id": 20},
				{"id": 4, "name": "lint"},
			},
		},
	})

	t.Run("returns jobs for requested pipeline", func(t *testing.T) {
		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/pipelines/10/jobs"), "token", nil)
		defer closeBody(t, resp.Body)

		var jobs []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
			t.Fatal(err)
		}
		// pipeline 10 jobs + job without pipeline_id
		if len(jobs) != 3 {
			t.Errorf("expected 3 jobs for pipeline 10, got %d", len(jobs))
		}
	})

	t.Run("jobs without pipeline_id appear in all pipeline lists", func(t *testing.T) {
		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/pipelines/20/jobs"), "token", nil)
		defer closeBody(t, resp.Body)

		var jobs []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
			t.Fatal(err)
		}
		// pipeline 20 job + job without pipeline_id
		if len(jobs) != 2 {
			t.Errorf("expected 2 jobs for pipeline 20, got %d", len(jobs))
		}
	})

	t.Run("unknown pipeline returns only jobs without pipeline_id", func(t *testing.T) {
		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/pipelines/99/jobs"), "token", nil)
		defer closeBody(t, resp.Body)

		var jobs []map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
			t.Fatal(err)
		}
		if len(jobs) != 1 {
			t.Errorf("expected 1 job (no-pipeline_id) for unknown pipeline, got %d", len(jobs))
		}
	})
}

// --- Commit endpoints ---

func TestRepositoryCommitsList(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/repository/commits"), "token", nil)
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Total") == "" {
		t.Error("missing X-Total header on commits list")
	}
	var commits []any
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryCommitSingle(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/repository/commits/abc123def456"), "token", nil)
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodeObject(t, resp.Body)
	if body["id"] != "abc123def456" {
		t.Errorf("commit id = %v, want abc123def456", body["id"])
	}
	if body["short_id"] != "abc123de" {
		t.Errorf("short_id = %v, want abc123de", body["short_id"])
	}
}

func TestRepositoryCommitSingleShortSHA(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/repository/commits/abc"), "token", nil)
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodeObject(t, resp.Body)
	if body["short_id"] != "abc" {
		t.Errorf("short_id = %v, want abc", body["short_id"])
	}
}

func TestCommitStatusStoredAndRetrieved(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})
	sha := "abc123"

	t.Run("GET before any POST returns empty list", func(t *testing.T) {
		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/repository/commits/"+sha+"/statuses"), "token", nil)
		defer closeBody(t, resp.Body)

		var statuses []any
		if err := json.NewDecoder(resp.Body).Decode(&statuses); err != nil {
			t.Fatal(err)
		}
		if len(statuses) != 0 {
			t.Errorf("expected empty, got %d", len(statuses))
		}
	})

	t.Run("POST stores status, GET retrieves it", func(t *testing.T) {
		postResp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/statuses/"+sha), "token", map[string]any{
			"state":       "success",
			"name":        "ci/unit-tests",
			"target_url":  "https://ci.example.com/jobs/1",
			"description": "All tests passed",
		})
		defer closeBody(t, postResp.Body)

		if postResp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", postResp.StatusCode)
		}
		created := decodeObject(t, postResp.Body)
		if created["sha"] != sha {
			t.Errorf("sha = %v, want %s", created["sha"], sha)
		}
		if created["state"] != "success" {
			t.Errorf("state = %v, want success", created["state"])
		}
		if created["id"] != float64(1) {
			t.Errorf("first status id = %v, want 1", created["id"])
		}

		getResp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/repository/commits/"+sha+"/statuses"), "token", nil)
		defer closeBody(t, getResp.Body)

		var statuses []map[string]any
		if err := json.NewDecoder(getResp.Body).Decode(&statuses); err != nil {
			t.Fatal(err)
		}
		if len(statuses) != 1 {
			t.Fatalf("expected 1 status, got %d", len(statuses))
		}
		if statuses[0]["name"] != "ci/unit-tests" {
			t.Errorf("status name = %v", statuses[0]["name"])
		}
	})

	t.Run("second POST gets auto-incremented ID", func(t *testing.T) {
		postResp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/statuses/"+sha), "token", map[string]any{
			"state": "failed",
			"name":  "ci/lint",
		})
		defer closeBody(t, postResp.Body)

		created := decodeObject(t, postResp.Body)
		if created["id"] != float64(2) {
			t.Errorf("second status id = %v, want 2", created["id"])
		}
	})

	t.Run("statuses are isolated per SHA", func(t *testing.T) {
		otherSHA := "def456"
		doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/statuses/"+otherSHA), "token", map[string]any{
			"state": "success", "name": "ci/build",
		})

		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/repository/commits/"+otherSHA+"/statuses"), "token", nil)
		defer closeBody(t, resp.Body)

		var statuses []any
		_ = json.NewDecoder(resp.Body).Decode(&statuses)
		if len(statuses) != 1 {
			t.Errorf("other SHA statuses = %d, want 1", len(statuses))
		}
	})
}

// --- Jobs resource CRUD ---

func TestJobsResourceCRUD(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	createResp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/jobs"), "token", map[string]any{
		"name":   "build",
		"stage":  "build",
		"status": "success",
	})
	defer closeBody(t, createResp.Body)
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", createResp.StatusCode)
	}

	created := decodeObject(t, createResp.Body)
	if created["name"] != "build" {
		t.Errorf("name = %v, want build", created["name"])
	}

	listResp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/jobs"), "token", nil)
	defer closeBody(t, listResp.Body)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", listResp.StatusCode)
	}
	var jobs []map[string]any
	if err := json.NewDecoder(listResp.Body).Decode(&jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}
}

// --- Seeded pipelines and jobs ---

func TestSeedPipelinesAndJobs(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{
		Seed: &config.APISeedConfig{
			Pipelines: []map[string]any{
				{"id": 10, "status": "success", "ref": "main"},
			},
			Jobs: []map[string]any{
				{"id": 1, "name": "lint", "stage": "test", "status": "success"},
			},
		},
	})

	pResp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/pipelines/10"), "token", nil)
	defer closeBody(t, pResp.Body)
	if pResp.StatusCode != http.StatusOK {
		t.Fatalf("pipeline: expected 200, got %d", pResp.StatusCode)
	}
	p := decodeObject(t, pResp.Body)
	if p["ref"] != "main" {
		t.Errorf("ref = %v, want main", p["ref"])
	}

	jResp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/jobs/1"), "token", nil)
	defer closeBody(t, jResp.Body)
	if jResp.StatusCode != http.StatusOK {
		t.Fatalf("job: expected 200, got %d", jResp.StatusCode)
	}
	j := decodeObject(t, jResp.Body)
	if j["name"] != "lint" {
		t.Errorf("name = %v, want lint", j["name"])
	}
}

// --- Richer resource defaults ---

func TestMergeRequestDefaultFields(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	resp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/merge_requests"), "token", map[string]any{
		"title":         "Add feature",
		"source_branch": "feature/x",
		"target_branch": "main",
	})
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	body := decodeObject(t, resp.Body)
	if body["state"] != "opened" {
		t.Errorf("state = %v, want opened", body["state"])
	}
	if body["source_branch"] != "feature/x" {
		t.Errorf("source_branch = %v, want feature/x", body["source_branch"])
	}
}

func TestVariableDefaultFields(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	resp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/variables"), "token", map[string]any{
		"key":   "MY_VAR",
		"value": "secret",
	})
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	body := decodeObject(t, resp.Body)
	if body["variable_type"] != "env_var" {
		t.Errorf("variable_type = %v, want env_var", body["variable_type"])
	}
}

// --- groupConfig defaults ---

func TestGroupConfigDefaults(t *testing.T) {
	server, err := New(config.APISetupConfig{})
	if err != nil {
		t.Fatal(err)
	}

	g := server.groupConfig()
	if g.ID != 1 {
		t.Errorf("default group ID = %d, want 1", g.ID)
	}
	if g.Path != "test-group" {
		t.Errorf("default group path = %q, want test-group", g.Path)
	}

	customPath := config.GroupConfig{Path: "org/team"}
	server2, _ := New(config.APISetupConfig{Group: &customPath})
	g2 := server2.groupConfig()
	if g2.Name != "team" {
		t.Errorf("group name from path = %q, want team", g2.Name)
	}
}

// --- pathSegment helper ---

func TestPathSegmentHelper(t *testing.T) {
	cases := []struct {
		rest    string
		n       int
		want    string
	}{
		{"/merge_requests/5/notes", 0, "merge_requests"},
		{"/merge_requests/5/notes", 1, "5"},
		{"/merge_requests/5/notes", 2, "notes"},
		{"/pipelines/10/jobs", 1, "10"},
		{"/repository/commits/abc123/statuses", 2, "abc123"},
		{"/merge_requests/5/notes", 9, ""},
	}
	for _, c := range cases {
		got := pathSegment(c.rest, c.n)
		if got != c.want {
			t.Errorf("pathSegment(%q, %d) = %q, want %q", c.rest, c.n, got, c.want)
		}
	}
}

// contains is a simple substring helper for tests.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
