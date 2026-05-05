package mockserver

import (
	"strings"
	"sync"
	"time"
)

type APICall struct {
	Method      string
	Path        string
	RequestBody []byte
	StatusCode  int
	Timestamp   time.Time
}

type Recorder struct {
	mu    sync.Mutex
	calls []APICall
}

func (r *Recorder) Record(call APICall) {
	r.mu.Lock()
	defer r.mu.Unlock()

	call.RequestBody = append([]byte(nil), call.RequestBody...)
	r.calls = append(r.calls, call)
}

func (r *Recorder) Calls() []APICall {
	r.mu.Lock()
	defer r.mu.Unlock()

	calls := make([]APICall, len(r.calls))
	for i, call := range r.calls {
		call.RequestBody = append([]byte(nil), call.RequestBody...)
		calls[i] = call
	}
	return calls
}

func (r *Recorder) MatchingCalls(method string, pathPattern string) []APICall {
	r.mu.Lock()
	defer r.mu.Unlock()

	var matches []APICall
	for _, call := range r.calls {
		if call.Method != method {
			continue
		}
		if !pathMatches(pathPattern, call.Path) {
			continue
		}
		call.RequestBody = append([]byte(nil), call.RequestBody...)
		matches = append(matches, call)
	}
	return matches
}

func pathMatches(pattern string, path string) bool {
	patternParts := splitPath(pattern)
	pathParts := splitPath(path)
	if len(patternParts) != len(pathParts) {
		return false
	}

	for i := range patternParts {
		if patternParts[i] == "*" {
			continue
		}
		if patternParts[i] != pathParts[i] {
			return false
		}
	}
	return true
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}
