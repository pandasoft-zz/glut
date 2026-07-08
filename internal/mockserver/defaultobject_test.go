package mockserver

import "testing"

// TestCreateAssignsIdentifierForEverySeededResource walks every resource kind
// the store pre-seeds defaults for and checks that Create assigns a non-zero
// identifier — covering each defaultObject arm and pinning the id contract.
func TestCreateAssignsIdentifierForEverySeededResource(t *testing.T) {
	t.Parallel()
	resources := []string{
		"releases", "merge_requests", "issues", "labels", "milestones",
		"pipelines", "jobs", "environments", "deployments", "hooks",
		"tags", "branches", "variables",
	}
	for _, resource := range resources {
		t.Run(resource, func(t *testing.T) {
			t.Parallel()
			store := NewInMemoryStore()
			created := store.Create(resource, map[string]any{"name": "x"})
			identifier := identifierFor(resource)
			if identifier == "id" || identifier == "iid" {
				if v, ok := created[identifier]; !ok || isUnsetIdentifier(v) {
					t.Fatalf("Create(%s) identifier %s = %v, want auto-assigned non-zero", resource, identifier, v)
				}
			}
			second := store.Create(resource, map[string]any{"name": "y"})
			if identifier == "id" || identifier == "iid" {
				if second[identifier] == created[identifier] {
					t.Fatalf("Create(%s) second identifier %v must differ from first %v", resource, second[identifier], created[identifier])
				}
			}
		})
	}
}
