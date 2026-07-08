package mockserver

import (
	"fmt"
	"sync"
)

type InMemoryStore struct {
	mu      sync.Mutex
	objects map[string][]map[string]any
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		objects: make(map[string][]map[string]any),
	}
}

func (s *InMemoryStore) List(resource string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := s.objects[resource]
	copies := make([]map[string]any, len(items))
	for i, item := range items {
		copies[i] = copyObject(item)
	}
	return copies
}

func (s *InMemoryStore) Get(resource string, id any) (map[string]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := s.findLocked(resource, id)
	if index < 0 {
		return nil, false
	}
	return copyObject(s.objects[resource][index]), true
}

func (s *InMemoryStore) Create(resource string, obj map[string]any) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	created := defaultObject(resource)
	mergeObject(created, obj)
	s.setDefaultIdentifierLocked(resource, created)
	s.objects[resource] = append(s.objects[resource], copyObject(created))
	return copyObject(created)
}

func (s *InMemoryStore) Update(resource string, id any, obj map[string]any) (map[string]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := s.findLocked(resource, id)
	if index < 0 {
		return nil, false
	}

	// id is the raw URL path segment (always a string), which does not match
	// the type the identifier was stored as (e.g. int for pipelines/jobs).
	// Preserve the stored value's original type instead of overwriting it,
	// so a client decoding the response into an int does not fail after PUT.
	identifier := identifierFor(resource)
	originalID := s.objects[resource][index][identifier]

	updated := copyObject(s.objects[resource][index])
	mergeObject(updated, obj)
	updated[identifier] = originalID
	s.objects[resource][index] = copyObject(updated)
	return copyObject(updated), true
}

func (s *InMemoryStore) Delete(resource string, id any) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	index := s.findLocked(resource, id)
	if index < 0 {
		return false
	}

	items := s.objects[resource]
	s.objects[resource] = append(items[:index], items[index+1:]...)
	return true
}

func (s *InMemoryStore) findLocked(resource string, id any) int {
	identifier := identifierFor(resource)
	for i, item := range s.objects[resource] {
		if fmt.Sprint(item[identifier]) == fmt.Sprint(id) {
			return i
		}
	}
	return -1
}

func (s *InMemoryStore) setDefaultIdentifierLocked(resource string, obj map[string]any) {
	identifier := identifierFor(resource)
	if identifier != "id" && identifier != "iid" {
		return
	}
	if v, ok := obj[identifier]; ok && !isUnsetIdentifier(v) {
		return
	}
	obj[identifier] = len(s.objects[resource]) + 1
}

// isUnsetIdentifier reports whether v is the numeric zero value defaultObject
// pre-seeds for "id"/"iid" fields. Zero is never a real GitLab identifier, so
// treating it as unset lets the auto-increment branch below actually run.
func isUnsetIdentifier(v any) bool {
	switch n := v.(type) {
	case int:
		return n == 0
	case int64:
		return n == 0
	case float64:
		return n == 0
	default:
		return false
	}
}

func seedStore(cfgSeed map[string][]map[string]interface{}, store *InMemoryStore) {
	for resource, objects := range cfgSeed {
		for _, obj := range objects {
			store.Create(resource, obj)
		}
	}
}

// copyObject deep-copies obj so the store and every caller holding a
// returned object never share mutable state: mutating a nested map or slice
// on a copy handed out by List/Get/Create/Update must not corrupt the
// store's own data, or a caller in a different goroutine's future request.
func copyObject(obj map[string]any) map[string]any {
	dst := make(map[string]any, len(obj))
	for key, value := range obj {
		dst[key] = deepCopyValue(value)
	}
	return dst
}

func deepCopyValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return copyObject(v)
	case []any:
		dst := make([]any, len(v))
		for i, item := range v {
			dst[i] = deepCopyValue(item)
		}
		return dst
	default:
		return value
	}
}

func mergeObject(dst map[string]any, src map[string]any) {
	for key, value := range src {
		dst[key] = value
	}
}

