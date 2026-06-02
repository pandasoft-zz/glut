package mockserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
)

func TestServerStartsAndServesVersion(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/version"), "", nil)
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := decodeObject(t, resp.Body)
	if body["version"] == "" {
		t.Fatalf("expected version in response")
	}
}

func TestTokenAuthSuccessAndFailure(t *testing.T) {
	t.Run("private token succeeds", func(t *testing.T) {
		server := startTestServer(t, config.APISetupConfig{})

		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1"), "test-token", nil)
		defer closeBody(t, resp.Body)

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("missing token fails", func(t *testing.T) {
		server := startTestServer(t, config.APISetupConfig{})

		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1"), "", nil)
		defer closeBody(t, resp.Body)

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid token config fails", func(t *testing.T) {
		server := startTestServer(t, config.APISetupConfig{
			Token: &config.TokenConfig{Valid: false},
		})

		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1"), "test-token", nil)
		defer closeBody(t, resp.Body)

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("write needs api scope when scopes are set", func(t *testing.T) {
		server := startTestServer(t, config.APISetupConfig{
			Token: &config.TokenConfig{Valid: true, Scopes: []string{"read_api"}},
		})

		resp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/releases"), "test-token", map[string]any{
			"tag_name": "v1.0.0",
		})
		defer closeBody(t, resp.Body)

		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("read api scope allows read requests", func(t *testing.T) {
		server := startTestServer(t, config.APISetupConfig{
			Token: &config.TokenConfig{Valid: true, Scopes: []string{"read_api"}},
		})

		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1"), "test-token", nil)
		defer closeBody(t, resp.Body)

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("api scope allows write requests", func(t *testing.T) {
		server := startTestServer(t, config.APISetupConfig{
			Token: &config.TokenConfig{Valid: true, Scopes: []string{"api"}},
		})

		resp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/releases"), "test-token", map[string]any{
			"tag_name": "v1.0.0",
		})
		defer closeBody(t, resp.Body)

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d", resp.StatusCode)
		}
	})

	t.Run("write repository scope does not authenticate API requests", func(t *testing.T) {
		server := startTestServer(t, config.APISetupConfig{
			Token: &config.TokenConfig{Valid: true, Scopes: []string{"write_repository"}},
		})

		resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1"), "test-token", nil)
		defer closeBody(t, resp.Body)

		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})
}

func TestPersonalAccessTokenResponse(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{
		Token: &config.TokenConfig{
			Valid:     true,
			ExpiresAt: "2026-01-01T00:00:00Z",
			Scopes:    []string{"api"},
		},
	})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/personal_access_tokens/self"), "test-token", nil)
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := decodeObject(t, resp.Body)
	if body["active"] != true {
		t.Fatalf("expected active token")
	}
	if body["expires_at"] != "2026-01-01T00:00:00Z" {
		t.Fatalf("unexpected expires_at: %v", body["expires_at"])
	}
}

func TestProjectResponseIncludesPermissionsWithDefaultAccessLevel(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1"), "test-token", nil)
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := decodeObject(t, resp.Body)
	permissions, _ := body["permissions"].(map[string]any)
	projectAccess, _ := permissions["project_access"].(map[string]any)
	if projectAccess["access_level"] != float64(40) {
		t.Fatalf("expected default access_level 40, got %v", projectAccess["access_level"])
	}
	if permissions["group_access"] != nil {
		t.Fatalf("expected group_access nil, got %v", permissions["group_access"])
	}
}

func TestProjectResponseRespectsConfiguredAccessLevel(t *testing.T) {
	level := config.AccessLevelGuest
	server := startTestServer(t, config.APISetupConfig{
		Project: &config.ProjectConfig{AccessLevel: &level},
	})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1"), "test-token", nil)
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := decodeObject(t, resp.Body)
	permissions, _ := body["permissions"].(map[string]any)
	projectAccess, _ := permissions["project_access"].(map[string]any)
	if projectAccess["access_level"] != float64(10) {
		t.Fatalf("expected access_level 10, got %v", projectAccess["access_level"])
	}
}

func TestProjectLookupByIDAndPath(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	for _, projectID := range []string{"1", url.PathEscape("test-group/test-project")} {
		t.Run(projectID, func(t *testing.T) {
			resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/"+projectID), "test-token", nil)
			defer closeBody(t, resp.Body)

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}

			body := decodeObject(t, resp.Body)
			if body["path_with_namespace"] != "test-group/test-project" {
				t.Fatalf("unexpected project path: %v", body["path_with_namespace"])
			}
		})
	}
}

func TestReleaseCRUD(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	createResp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/releases"), "test-token", map[string]any{
		"tag_name": "v1.0.0",
		"name":     "Release 1.0.0",
	})
	defer closeBody(t, createResp.Body)
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", createResp.StatusCode)
	}

	getResp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/releases/v1.0.0"), "test-token", nil)
	defer closeBody(t, getResp.Body)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", getResp.StatusCode)
	}

	created := decodeObject(t, getResp.Body)
	if created["name"] != "Release 1.0.0" {
		t.Fatalf("unexpected release name: %v", created["name"])
	}

	updateResp := doRequest(t, http.MethodPut, serverURL(server, "/api/v4/projects/1/releases/v1.0.0"), "test-token", map[string]any{
		"name": "Release 1.0.1",
	})
	defer closeBody(t, updateResp.Body)
	if updateResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", updateResp.StatusCode)
	}

	updated := decodeObject(t, updateResp.Body)
	if updated["name"] != "Release 1.0.1" {
		t.Fatalf("unexpected release name: %v", updated["name"])
	}

	deleteResp := doRequest(t, http.MethodDelete, serverURL(server, "/api/v4/projects/1/releases/v1.0.0"), "test-token", nil)
	defer closeBody(t, deleteResp.Body)
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", deleteResp.StatusCode)
	}

	missingResp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/releases/v1.0.0"), "test-token", nil)
	defer closeBody(t, missingResp.Body)
	if missingResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", missingResp.StatusCode)
	}
}

func TestSeedDataIsAvailableBeforeFirstRequest(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{
		Seed: &config.APISeedConfig{
			Releases: []map[string]interface{}{
				{
					"tag_name": "v1.2.3",
					"name":     "Seed release",
				},
			},
		},
	})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/releases/v1.2.3"), "test-token", nil)
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := decodeObject(t, resp.Body)
	if body["name"] != "Seed release" {
		t.Fatalf("unexpected release name: %v", body["name"])
	}
}

func TestRecorderReturnsCopyAndMatchesWildcardPaths(t *testing.T) {
	recorder := &Recorder{}
	recorder.Record(APICall{
		Method:      http.MethodPost,
		Path:        "/api/v4/projects/1/releases",
		RequestBody: []byte(`{"tag_name":"v1.0.0"}`),
		StatusCode:  http.StatusCreated,
	})

	calls := recorder.Calls()
	calls[0].RequestBody[0] = '['

	freshCalls := recorder.Calls()
	if string(freshCalls[0].RequestBody) != `{"tag_name":"v1.0.0"}` {
		t.Fatalf("recorder did not return a body copy")
	}

	matches := recorder.MatchingCalls(http.MethodPost, "/api/v4/projects/*/releases")
	if len(matches) != 1 {
		t.Fatalf("expected one matching call, got %d", len(matches))
	}
}

func TestUnknownEndpointReturnsNotFound(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/unknown"), "test-token", nil)
	defer closeBody(t, resp.Body)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}

	body := decodeObject(t, resp.Body)
	if body["message"] != "404 Not Found" {
		t.Fatalf("unexpected message: %v", body["message"])
	}
}

func TestConcurrentRequestsAreSafe(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp := doRequest(t, http.MethodPost, serverURL(server, "/api/v4/projects/1/releases"), "test-token", map[string]any{
				"tag_name": fmt.Sprintf("v1.0.%d", i),
				"name":     fmt.Sprintf("Release %d", i),
			})
			defer closeBody(t, resp.Body)
			if resp.StatusCode != http.StatusCreated {
				t.Errorf("expected 201, got %d", resp.StatusCode)
			}
		}(i)
	}
	wg.Wait()

	resp := doRequest(t, http.MethodGet, serverURL(server, "/api/v4/projects/1/releases"), "test-token", nil)
	defer closeBody(t, resp.Body)

	var releases []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		t.Fatalf("failed to decode releases: %v", err)
	}
	if len(releases) != 20 {
		t.Fatalf("expected 20 releases, got %d", len(releases))
	}
}

// TestServerDoesNotBindToLocalhostOnly verifies BUG-3: the mock API server must listen on
// all network interfaces so Docker sibling containers can reach it via glut-mock or the
// bridge IP. Binding to 127.0.0.1 makes the server unreachable from inside Docker containers.
func TestServerDoesNotBindToLocalhostOnly(t *testing.T) {
	server := startTestServer(t, config.APISetupConfig{})
	addr := server.ListenAddr()
	if strings.HasPrefix(addr, "127.0.0.1") {
		t.Errorf("server listens on %q — Docker containers cannot reach 127.0.0.1; server must bind to 0.0.0.0", addr)
	}
}

func startTestServer(t *testing.T, cfg config.APISetupConfig) *Server {
	t.Helper()

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Stop(); err != nil {
			t.Fatalf("failed to stop server: %v", err)
		}
		if err := server.Stop(); err != nil {
			t.Fatalf("second stop failed: %v", err)
		}
	})
	return server
}

func serverURL(server *Server, path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", server.Port(), path)
}

func doRequest(t *testing.T, method string, requestURL string, token string, body map[string]any) *http.Response {
	t.Helper()

	var requestBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to encode request body: %v", err)
		}
		requestBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, requestURL, requestBody)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("PRIVATE-TOKEN", token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func decodeObject(t *testing.T, body io.Reader) map[string]any {
	t.Helper()

	var obj map[string]any
	if err := json.NewDecoder(body).Decode(&obj); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return obj
}

func closeBody(t *testing.T, body io.Closer) {
	t.Helper()

	if err := body.Close(); err != nil {
		t.Fatalf("failed to close response body: %v", err)
	}
}
