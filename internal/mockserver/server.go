package mockserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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
		_ = s.http.Serve(listener)
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

func (s *Server) handleTokenSelf(w http.ResponseWriter) {
	token := s.tokenConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         1,
		"name":       "glut token",
		"active":     token.Valid,
		"revoked":    !token.Valid,
		"scopes":     token.Scopes,
		"expires_at": token.ExpiresAt,
	})
}

func (s *Server) handleProject(w http.ResponseWriter) {
	path := s.projectPathValue()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":                  1,
		"path_with_namespace": path,
		"name":                projectName(path),
		"default_branch":      s.defaultBranch(),
	})
}

func (s *Server) handleDedicatedProjectEndpoint(w http.ResponseWriter, r *http.Request, rest string) bool {
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

	if r.Method == http.MethodPost && strings.HasSuffix(rest, "/approve") && strings.Contains(rest, "/merge_requests/") {
		writeJSON(w, http.StatusCreated, map[string]any{
			"approved": true,
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
				writeJSON(w, http.StatusOK, s.store.List(resource))
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
				writeJSON(w, http.StatusOK, map[string]any{"message": "202 Accepted"})
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
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, fmt.Sprintf("response write: %v", err), http.StatusInternalServerError)
	}
}

func writeNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]any{"message": "404 Not Found"})
}

func projectName(path string) string {
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
