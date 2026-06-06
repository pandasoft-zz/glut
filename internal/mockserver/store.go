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

	updated := copyObject(s.objects[resource][index])
	mergeObject(updated, obj)
	updated[identifierFor(resource)] = id
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
	if _, ok := obj[identifier]; ok {
		return
	}
	if identifier == "id" || identifier == "iid" {
		obj[identifier] = len(s.objects[resource]) + 1
	}
}

func seedStore(cfgSeed map[string][]map[string]interface{}, store *InMemoryStore) {
	for resource, objects := range cfgSeed {
		for _, obj := range objects {
			store.Create(resource, obj)
		}
	}
}

func copyObject(obj map[string]any) map[string]any {
	copy := make(map[string]any, len(obj))
	for key, value := range obj {
		copy[key] = value
	}
	return copy
}

func mergeObject(dst map[string]any, src map[string]any) {
	for key, value := range src {
		dst[key] = value
	}
}

