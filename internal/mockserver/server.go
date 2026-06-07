package mockserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pandasoft-zz/glut/internal/config"
)

const defaultProjectPath = "test-group/test-project"

type Server struct {
	cfg      config.APISetupConfig
	store    *InMemoryStore
	recorder *Recorder

	mu         sync.Mutex
	http       *http.Server
	port       int
	listenAddr string
	started    bool
	stopped    bool
	serveErr   error
}

func New(cfg config.APISetupConfig) (*Server, error) {
	store := NewInMemoryStore()
	if cfg.Seed != nil {
		seedStore(seedMap(cfg.Seed), store)
	}

	return &Server{
		cfg:      cfg,
		store:    store,
		recorder: &Recorder{},
	}, nil
}

func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started && !s.stopped {
		return nil
	}

	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return fmt.Errorf("server start: listen on local port: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.record(s.handle))

	s.http = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.port = listener.Addr().(*net.TCPAddr).Port
	s.listenAddr = listener.Addr().String()
	s.started = true
	s.stopped = false

	go func() {
		if err := s.http.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.mu.Lock()
			s.serveErr = err
			s.mu.Unlock()
		}
	}()

	return nil
}

func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.started || s.stopped || s.http == nil {
		s.stopped = true
		s.mu.Unlock()
		return nil
	}
	server := s.http
	s.stopped = true
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	return nil
}

func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.port
}

func (s *Server) ListenAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.listenAddr
}

func (s *Server) Recorder() *Recorder {
	return s.recorder
}

func (s *Server) record(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(rec, http.StatusBadRequest, map[string]any{"message": "request parse failed"})
			s.recorder.Record(APICall{
				Method:     r.Method,
				Path:       r.URL.EscapedPath(),
				StatusCode: rec.statusCode,
				Timestamp:  time.Now(),
			})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		next(rec, r)

		s.recorder.Record(APICall{
			Method:      r.Method,
			Path:        r.URL.EscapedPath(),
			RequestBody: body,
			StatusCode:  rec.statusCode,
			Timestamp:   time.Now(),
		})
	}
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	path := r.URL.EscapedPath()
	if r.Method == http.MethodGet && path == "/api/v4/version" {
		writeJSON(w, http.StatusOK, map[string]any{
			"version":  "16.11.0",
			"revision": "mock",
		})
		return
	}

	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "401 Unauthorized"})
		return
	}
	if s.isWrite(r) && !s.hasWriteScope() {
		writeJSON(w, http.StatusForbidden, map[string]any{"message": "403 Forbidden"})
		return
	}

	if r.Method == http.MethodGet && path == "/api/v4/personal_access_tokens/self" {
		s.handleTokenSelf(w)
		return
	}

	if r.Method == http.MethodGet && path == "/api/v4/user" {
		s.handleCurrentUser(w)
		return
	}

	if r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v4/users/") {
		s.handleUserByID(w)
		return
	}

	if r.Method == http.MethodGet && strings.HasPrefix(path, "/api/v4/groups/") {
		s.handleGroup(w)
		return
	}

	projectID, rest, ok := s.projectPath(path)
	if !ok || !s.validProjectID(projectID) {
		writeNotFound(w)
		return
	}

	if rest == "" && r.Method == http.MethodGet {
		s.handleProject(w)
		return
	}

	if s.handleDedicatedProjectEndpoint(w, r, rest) {
		return
	}
	if s.handleResource(w, r, rest) {
		return
	}

	writeNotFound(w)
}

func (s *Server) handleCurrentUser(w http.ResponseWriter) {
	u := s.userConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         u.ID,
		"name":       u.Name,
		"username":   u.Login,
		"email":      u.Email,
		"state":      "active",
		"avatar_url": "",
		"web_url":    "",
	})
}

func (s *Server) handleUserByID(w http.ResponseWriter) {
	u := s.userConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         u.ID,
		"name":       u.Name,
		"username":   u.Login,
		"email":      u.Email,
		"state":      "active",
		"avatar_url": "",
		"web_url":    "",
	})
}

func (s *Server) handleGroup(w http.ResponseWriter) {
	g := s.groupConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         g.ID,
		"name":       g.Name,
		"path":       groupBaseName(g.Path),
		"full_path":  g.Path,
		"full_name":  g.Name,
		"visibility": "private",
		"web_url":    "",
	})
}

func (s *Server) handleTokenSelf(w http.ResponseWriter) {
	token := s.tokenConfig()
	u := s.userConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":           1,
		"name":         "glut token",
		"active":       token.Valid,
		"revoked":      !token.Valid,
		"scopes":       token.Scopes,
		"expires_at":   token.ExpiresAt,
		"user_id":      u.ID,
		"created_at":   "2024-01-01T00:00:00.000Z",
		"last_used_at": "2024-01-01T00:00:00.000Z",
	})
}

func (s *Server) handleProject(w http.ResponseWriter) {
	path := s.projectPathValue()
	namespace := namespaceFromProjectPath(path)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                         1,
		"path_with_namespace":        path,
		"name":                       projectName(path),
		"description":                "",
		"default_branch":             s.defaultBranch(),
		"visibility":                 "private",
		"ssh_url_to_repo":            "git@example.com:" + path + ".git",
		"http_url_to_repo":           "https://example.com/" + path + ".git",
		"web_url":                    "https://example.com/" + path,
		"readme_url":                 "https://example.com/" + path + "/-/blob/main/README.md",
		"archived":                   false,
		"empty_repo":                 false,
		"star_count":                 0,
		"forks_count":                0,
		"open_issues_count":          0,
		"packages_enabled":           true,
		"container_registry_enabled": true,
		"created_at":                 "2024-01-01T00:00:00.000Z",
		"last_activity_at":           "2024-01-01T00:00:00.000Z",
		"namespace": map[string]any{
			"id":        1,
			"name":      groupBaseName(namespace),
			"path":      groupBaseName(namespace),
			"kind":      "group",
			"full_path": namespace,
		},
		"permissions": map[string]any{
			"project_access": map[string]any{
				"access_level": s.projectAccessLevel(),
			},
			"group_access": nil,
		},
	})
}

func (s *Server) projectAccessLevel() int {
	if s.cfg.Project != nil && s.cfg.Project.AccessLevel != nil {
		return int(*s.cfg.Project.AccessLevel)
	}
	return int(config.AccessLevelMaintainer)
}

func (s *Server) handleDedicatedProjectEndpoint(w http.ResponseWriter, r *http.Request, rest string) bool {
	// GET /repository/commits/:sha/merge_requests
	if r.Method == http.MethodGet && strings.HasSuffix(rest, "/merge_requests") && strings.Contains(rest, "/repository/commits/") {
		writeJSONList(w, []map[string]any{})
		return true
	}

	// GET /repository/commits/:sha/statuses
	if r.Method == http.MethodGet && strings.HasSuffix(rest, "/statuses") && strings.Contains(rest, "/repository/commits/") {
		writeJSONList(w, []map[string]any{})
		return true
	}

	// GET /repository/commits (list)
	if r.Method == http.MethodGet && rest == "/repository/commits" {
		writeJSONList(w, []map[string]any{})
		return true
	}

	// GET /repository/commits/:sha (single commit, no nested sub-path)
	if r.Method == http.MethodGet && strings.HasPrefix(rest, "/repository/commits/") {
		sha := strings.TrimPrefix(rest, "/repository/commits/")
		if !strings.Contains(sha, "/") {
			end := min(8, len(sha))
			writeJSON(w, http.StatusOK, map[string]any{
				"id":             sha,
				"short_id":       sha[:end],
				"title":          "mock commit",
				"message":        "mock commit",
				"author_name":    config.DefaultUserName,
				"author_email":   config.DefaultUserEmail,
				"committed_date": "2024-01-01T00:00:00.000Z",
				"created_at":     "2024-01-01T00:00:00.000Z",
			})
			return true
		}
	}

	// POST /repository/commits
	if r.Method == http.MethodPost && rest == "/repository/commits" {
		body, ok := readJSONBody(w, r)
		if !ok {
			return true
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":             "mock-commit-sha",
			"short_id":       "mock-com",
			"title":          body["commit_message"],
			"message":        body["commit_message"],
			"committed_date": time.Now().UTC().Format(time.RFC3339),
		})
		return true
	}

	// POST /statuses/:sha
	if r.Method == http.MethodPost && strings.HasPrefix(rest, "/statuses/") {
		body, ok := readJSONBody(w, r)
		if !ok {
			return true
		}
		sha := strings.TrimPrefix(rest, "/statuses/")
		state, _ := body["state"].(string)
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":          1,
			"sha":         sha,
			"state":       state,
			"name":        body["name"],
			"target_url":  body["target_url"],
			"description": body["description"],
			"created_at":  time.Now().UTC().Format(time.RFC3339),
		})
		return true
	}

	// POST /pipeline (trigger pipeline, singular path)
	if r.Method == http.MethodPost && rest == "/pipeline" {
		body, ok := readJSONBody(w, r)
		if !ok {
			return true
		}
		ref, _ := body["ref"].(string)
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":         1,
			"status":     "pending",
			"ref":        ref,
			"sha":        "mock-pipeline-sha",
			"created_at": time.Now().UTC().Format(time.RFC3339),
			"web_url":    "",
		})
		return true
	}

	// POST /pipelines/:id/retry or /cancel
	if r.Method == http.MethodPost && strings.Contains(rest, "/pipelines/") &&
		(strings.HasSuffix(rest, "/retry") || strings.HasSuffix(rest, "/cancel")) {
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":     1,
			"status": "pending",
		})
		return true
	}

	// GET /pipelines/:id/jobs
	if r.Method == http.MethodGet && strings.Contains(rest, "/pipelines/") && strings.HasSuffix(rest, "/jobs") {
		writeJSONList(w, s.store.List("jobs"))
		return true
	}

	// GET /merge_requests/:iid/notes
	if r.Method == http.MethodGet && strings.Contains(rest, "/merge_requests/") && strings.HasSuffix(rest, "/notes") {
		writeJSONList(w, []map[string]any{})
		return true
	}

	// POST /merge_requests/:iid/notes
	if r.Method == http.MethodPost && strings.HasSuffix(rest, "/notes") && strings.Contains(rest, "/merge_requests/") {
		body, ok := readJSONBody(w, r)
		if !ok {
			return true
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":   1,
			"body": body["body"],
		})
		return true
	}

	// POST /merge_requests/:iid/approve
	if r.Method == http.MethodPost && strings.HasSuffix(rest, "/approve") && strings.Contains(rest, "/merge_requests/") {
		writeJSON(w, http.StatusCreated, map[string]any{
			"approved": true,
		})
		return true
	}

	// POST /merge_requests/:iid/unapprove
	if r.Method == http.MethodPost && strings.HasSuffix(rest, "/unapprove") && strings.Contains(rest, "/merge_requests/") {
		writeJSON(w, http.StatusCreated, map[string]any{
			"approved": false,
		})
		return true
	}

	// PUT /merge_requests/:iid/merge
	if r.Method == http.MethodPut && strings.HasSuffix(rest, "/merge") && strings.Contains(rest, "/merge_requests/") {
		writeJSON(w, http.StatusOK, map[string]any{
			"state":      "merged",
			"merged_at":  time.Now().UTC().Format(time.RFC3339),
			"merge_commit_sha": "mock-merge-sha",
		})
		return true
	}

	// GET /merge_requests/:iid/approvals
	if r.Method == http.MethodGet && strings.HasSuffix(rest, "/approvals") && strings.Contains(rest, "/merge_requests/") {
		writeJSON(w, http.StatusOK, map[string]any{
			"approvals_required": 1,
			"approvals_left":     1,
			"approved":           false,
			"approved_by":        []any{},
		})
		return true
	}

	// GET /merge_requests/:iid/changes
	if r.Method == http.MethodGet && strings.HasSuffix(rest, "/changes") && strings.Contains(rest, "/merge_requests/") {
		writeJSON(w, http.StatusOK, map[string]any{
			"changes":       []any{},
			"changes_count": "0",
		})
		return true
	}

	// GET /merge_requests/:iid/discussions
	if r.Method == http.MethodGet && strings.HasSuffix(rest, "/discussions") && strings.Contains(rest, "/merge_requests/") {
		writeJSONList(w, []map[string]any{})
		return true
	}

	// GET /issues/:iid/notes
	if r.Method == http.MethodGet && strings.Contains(rest, "/issues/") && strings.HasSuffix(rest, "/notes") {
		writeJSONList(w, []map[string]any{})
		return true
	}

	// POST /issues/:iid/notes
	if r.Method == http.MethodPost && strings.Contains(rest, "/issues/") && strings.HasSuffix(rest, "/notes") {
		body, ok := readJSONBody(w, r)
		if !ok {
			return true
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":   1,
			"body": body["body"],
		})
		return true
	}

	return false
}

func (s *Server) handleResource(w http.ResponseWriter, r *http.Request, rest string) bool {
	for resource, base := range resourceBases {
		if rest == base {
			switch r.Method {
			case http.MethodGet:
				writeJSONList(w, s.store.List(resource))
			case http.MethodPost:
				body, ok := readJSONBody(w, r)
				if !ok {
					return true
				}
				writeJSON(w, http.StatusCreated, s.store.Create(resource, body))
			default:
				writeNotFound(w)
			}
			return true
		}

		prefix := base + "/"
		if strings.HasPrefix(rest, prefix) {
			id, err := url.PathUnescape(strings.TrimPrefix(rest, prefix))
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"message": "request parse failed"})
				return true
			}
			switch r.Method {
			case http.MethodGet:
				obj, ok := s.store.Get(resource, id)
				if !ok {
					writeNotFound(w)
					return true
				}
				writeJSON(w, http.StatusOK, obj)
			case http.MethodPut:
				body, ok := readJSONBody(w, r)
				if !ok {
					return true
				}
				obj, ok := s.store.Update(resource, id, body)
				if !ok {
					writeNotFound(w)
					return true
				}
				writeJSON(w, http.StatusOK, obj)
			case http.MethodDelete:
				if !s.store.Delete(resource, id) {
					writeNotFound(w)
					return true
				}
				writeJSON(w, http.StatusOK, map[string]any{"message": "200 OK"})
			default:
				writeNotFound(w)
			}
			return true
		}
	}
	return false
}

func (s *Server) authorized(r *http.Request) bool {
	token := s.tokenConfig()
	if !token.Valid {
		return false
	}
	return hasAuthHeader(r) && s.hasReadScope()
}

func hasAuthHeader(r *http.Request) bool {
	return r.Header.Get("PRIVATE-TOKEN") != "" || strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func (s *Server) hasReadScope() bool {
	token := s.tokenConfig()
	if len(token.Scopes) == 0 {
		return true
	}
	for _, scope := range token.Scopes {
		if scope == "api" || scope == "read_api" {
			return true
		}
	}
	return false
}

func (s *Server) hasWriteScope() bool {
	token := s.tokenConfig()
	if len(token.Scopes) == 0 {
		return true
	}
	for _, scope := range token.Scopes {
		if scope == "api" {
			return true
		}
	}
	return false
}

func (s *Server) isWrite(r *http.Request) bool {
	return r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete || r.Method == http.MethodPatch
}

func (s *Server) tokenConfig() config.TokenConfig {
	if s.cfg.Token == nil {
		return config.TokenConfig{Valid: true}
	}
	return *s.cfg.Token
}

func (s *Server) userConfig() config.UserConfig {
	if s.cfg.User != nil {
		u := *s.cfg.User
		if u.ID == 0 {
			u.ID = 1
		}
		if u.Name == "" {
			u.Name = config.DefaultUserName
		}
		if u.Login == "" {
			u.Login = config.DefaultUserLogin
		}
		if u.Email == "" {
			u.Email = config.DefaultUserEmail
		}
		return u
	}
	return config.UserConfig{
		ID:    1,
		Name:  config.DefaultUserName,
		Login: config.DefaultUserLogin,
		Email: config.DefaultUserEmail,
	}
}

func (s *Server) groupConfig() config.GroupConfig {
	if s.cfg.Group != nil {
		g := *s.cfg.Group
		if g.ID == 0 {
			g.ID = 1
		}
		if g.Path == "" {
			g.Path = namespaceFromProjectPath(s.projectPathValue())
		}
		if g.Name == "" {
			g.Name = groupBaseName(g.Path)
		}
		return g
	}
	gPath := namespaceFromProjectPath(s.projectPathValue())
	return config.GroupConfig{
		ID:   1,
		Path: gPath,
		Name: groupBaseName(gPath),
	}
}

func (s *Server) projectPath(path string) (string, string, bool) {
	const prefix = "/api/v4/projects/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}

	after := strings.TrimPrefix(path, prefix)
	for _, id := range s.acceptedProjectIDs() {
		if after == id {
			return id, "", true
		}
		if strings.HasPrefix(after, id+"/") {
			return id, strings.TrimPrefix(after, id), true
		}
	}
	return "", "", false
}

func (s *Server) validProjectID(projectID string) bool {
	for _, id := range s.acceptedProjectIDs() {
		if projectID == id {
			return true
		}
	}
	return false
}

func (s *Server) acceptedProjectIDs() []string {
	return []string{"1", url.PathEscape(s.projectPathValue())}
}

func (s *Server) projectPathValue() string {
	if s.cfg.Project != nil && s.cfg.Project.Path != "" {
		return s.cfg.Project.Path
	}
	return defaultProjectPath
}

func (s *Server) defaultBranch() string {
	if s.cfg.Project != nil && s.cfg.Project.DefaultBranch != "" {
		return s.cfg.Project.DefaultBranch
	}
	return config.DefaultBranchName
}

func seedMap(seed *config.APISeedConfig) map[string][]map[string]interface{} {
	return map[string][]map[string]interface{}{
		"releases":       seed.Releases,
		"merge_requests": seed.MergeRequests,
		"labels":         seed.Labels,
		"pipelines":      seed.Pipelines,
		"jobs":           seed.Jobs,
	}
}

func readJSONBody(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	if r.Body == nil {
		return map[string]any{}, true
	}

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "request parse failed"})
		return nil, false
	}
	if body == nil {
		body = map[string]any{}
	}
	return body, true
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value) // headers already sent; nothing useful to do on error
}

func writeJSONList(w http.ResponseWriter, items []map[string]any) {
	total := len(items)
	w.Header().Set("X-Total", strconv.Itoa(total))
	w.Header().Set("X-Total-Pages", "1")
	w.Header().Set("X-Page", "1")
	w.Header().Set("X-Per-Page", "100")
	w.Header().Set("X-Next-Page", "")
	w.Header().Set("X-Prev-Page", "")
	writeJSON(w, http.StatusOK, items)
}

func writeNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]any{"message": "404 Not Found"})
}

func projectName(path string) string {
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func namespaceFromProjectPath(projectPath string) string {
	idx := strings.LastIndex(projectPath, "/")
	if idx < 0 {
		return projectPath
	}
	return projectPath[:idx]
}

func groupBaseName(path string) string {
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusRecorder) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}
