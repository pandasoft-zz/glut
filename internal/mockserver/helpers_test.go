package mockserver

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
)

func TestServerHelperCoverage(t *testing.T) {
	t.Run("server helpers", func(t *testing.T) {
		server, err := New(config.APISetupConfig{
			Project: &config.ProjectConfig{
				Path:          "group/sub/project",
				DefaultBranch: "release",
			},
			Seed: &config.APISeedConfig{
				Labels: []map[string]any{{"id": 1, "name": "ready"}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		if server.Recorder() == nil {
			t.Fatal("expected recorder")
		}
		if got := server.projectPathValue(); got != "group/sub/project" {
			t.Fatalf("projectPathValue() = %q", got)
		}
		if got := server.defaultBranch(); got != "release" {
			t.Fatalf("defaultBranch() = %q", got)
		}
		if got := projectName("group/sub/project"); got != "project" {
			t.Fatalf("projectName() = %q", got)
		}
		if !server.validProjectID("1") {
			t.Fatal("expected project id 1 to be valid")
		}
		if !server.validProjectID(url.PathEscape("group/sub/project")) {
			t.Fatal("expected escaped project path to be valid")
		}
		if _, rest, ok := server.projectPath("/api/v4/projects/1/releases"); !ok || rest != "/releases" {
			t.Fatalf("projectPath() = %q, %v", rest, ok)
		}
		if _, _, ok := server.projectPath("/wrong"); ok {
			t.Fatal("unexpected projectPath match")
		}
		if cfg := (&Server{}).tokenConfig(); !cfg.Valid {
			t.Fatalf("default token config = %+v", cfg)
		}

		labels := server.store.List("labels")
		if len(labels) != 1 || labels[0]["name"] != "ready" {
			t.Fatalf("seeded labels = %#v", labels)
		}
	})

	t.Run("resource helpers", func(t *testing.T) {
		if got := identifierFor("custom"); got != "id" {
			t.Fatalf("identifierFor(custom) = %q", got)
		}
		if got := identifierFor("variables"); got != "key" {
			t.Fatalf("identifierFor(variables) = %q", got)
		}
		if got := defaultObject("releases"); got["description"] != "" {
			t.Fatalf("defaultObject(releases) = %#v", got)
		}
		if got := defaultObject("merge_requests"); got["state"] != "opened" {
			t.Fatalf("defaultObject(merge_requests) = %#v", got)
		}
		if got := defaultObject("repository/tags"); got["name"] != "" {
			t.Fatalf("defaultObject(repository/tags) = %#v", got)
		}
		if got := defaultObject("repository/branches"); got["protected"] != false {
			t.Fatalf("defaultObject(repository/branches) = %#v", got)
		}
		if got := defaultObject("labels"); got["color"] != "#000000" {
			t.Fatalf("defaultObject(labels) = %#v", got)
		}
		if got := defaultObject("variables"); got["key"] != "" {
			t.Fatalf("defaultObject(variables) = %#v", got)
		}
		if got := defaultObject("custom"); got["id"] != 0 {
			t.Fatalf("defaultObject(custom) = %#v", got)
		}
	})

	t.Run("read body helpers", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		body, ok := readJSONBody(rec, req)
		if !ok || len(body) != 0 {
			t.Fatalf("readJSONBody(nil) = %#v, %v", body, ok)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/x", bytes.NewBufferString(""))
		body, ok = readJSONBody(rec, req)
		if !ok || len(body) != 0 {
			t.Fatalf("readJSONBody(empty) = %#v, %v", body, ok)
		}

		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/x", bytes.NewBufferString("{bad"))
		body, ok = readJSONBody(rec, req)
		if ok || body != nil || rec.Code != http.StatusBadRequest {
			t.Fatalf("readJSONBody(invalid) = %#v, %v, status=%d", body, ok, rec.Code)
		}
	})

	t.Run("write helpers", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writeNotFound(rec)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("writeNotFound() status = %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		writeJSON(rec, http.StatusCreated, map[string]any{"ok": true})
		if rec.Code != http.StatusCreated {
			t.Fatalf("writeJSON() status = %d", rec.Code)
		}

		wrapped := &statusRecorder{ResponseWriter: rec, statusCode: http.StatusOK}
		wrapped.WriteHeader(http.StatusAccepted)
		if wrapped.statusCode != http.StatusAccepted {
			t.Fatalf("statusRecorder status = %d", wrapped.statusCode)
		}
	})
}

func TestServerEndpointsCoverage(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{
		Project: &config.ProjectConfig{
			Path:          "group/project",
			DefaultBranch: "release",
		},
	})

	t.Run("bearer token auth", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, serverURL(server, "/api/v4/projects/1"), nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer closeBody(t, resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("project response uses custom values", func(t *testing.T) {
		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1"), "test-token", nil)
		defer closeBody(t, resp.Body)
		body := decodeObject(t, resp.Body)
		if body["default_branch"] != "release" || body["path_with_namespace"] != "group/project" {
			t.Fatalf("project body = %#v", body)
		}
	})

	t.Run("dedicated endpoints", func(t *testing.T) {
		commitResp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/repository/commits"), "test-token", map[string]any{
			"commit_message": "feat: add release",
		})
		defer closeBody(t, commitResp.Body)
		if commitResp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", commitResp.StatusCode)
		}

		noteResp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/merge_requests/12/notes"), "test-token", map[string]any{
			"body": "looks good",
		})
		defer closeBody(t, noteResp.Body)
		if noteResp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", noteResp.StatusCode)
		}

		approveResp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/merge_requests/12/approve"), "test-token", nil)
		defer closeBody(t, approveResp.Body)
		if approveResp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", approveResp.StatusCode)
		}
	})

	t.Run("resource edge cases", func(t *testing.T) {
		resp := doRequest(t, http.MethodPatch, serverURL(server, "/api/v4/projects/1/releases"), "test-token", nil)
		defer closeBody(t, resp.Body)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}

		badReq, err := http.NewRequest(http.MethodPost, serverURL(server, "/api/v4/projects/1/releases"), bytes.NewBufferString("{bad"))
		if err != nil {
			t.Fatal(err)
		}
		badReq.Header.Set("PRIVATE-TOKEN", "test-token")
		badReq.Header.Set("Content-Type", "application/json")
		badResp, err := http.DefaultClient.Do(badReq)
		if err != nil {
			t.Fatal(err)
		}
		defer closeBody(t, badResp.Body)
		if badResp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", badResp.StatusCode)
		}

		rec := httptest.NewRecorder()
		req := &http.Request{Method: http.MethodGet}
		if !server.handleResource(rec, req, "/releases/bad%2") {
			t.Fatal("expected handleResource to handle invalid escaped id")
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})

	calls := server.Recorder().Calls()
	if len(calls) == 0 {
		t.Fatal("expected recorded API calls")
	}
}

func TestRecorderHelperCoverage(t *testing.T) {
	recorder := &Recorder{}
	recorder.Record(APICall{Method: http.MethodGet, Path: "/api/v4/projects/1"})

	if matches := recorder.MatchingCalls(http.MethodPost, "/api/v4/projects/*"); len(matches) != 0 {
		t.Fatalf("expected no matches, got %+v", matches)
	}
	if pathMatches("/a/*/c", "/a/b/c") != true {
		t.Fatal("expected wildcard path match")
	}
	if pathMatches("/a/b", "/a/b/c") {
		t.Fatal("expected path length mismatch to fail")
	}
	if parts := splitPath("/"); parts != nil {
		t.Fatalf("splitPath(root) = %#v", parts)
	}
}

func TestWriteJSONWithFailingWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(failingResponseWriter{ResponseWriter: rec}, http.StatusOK, map[string]any{
		"broken": make(chan int),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status to stay 200 after late write failure, got %d", rec.Code)
	}
}

type failingResponseWriter struct {
	http.ResponseWriter
}

func (w failingResponseWriter) Write(data []byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func (w failingResponseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w failingResponseWriter) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
}
