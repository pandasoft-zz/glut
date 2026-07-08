package parser

import "testing"

func TestIsReservedTopLevelKey(t *testing.T) {
	t.Parallel()
	for _, reserved := range []string{"stages", "variables", "workflow", "include", "default"} {
		if !IsReservedTopLevelKey(reserved) {
			t.Fatalf("IsReservedTopLevelKey(%q) = false, want true", reserved)
		}
	}
	// pages is a real, common GitLab CI job name — it must never be reserved.
	for _, job := range []string{"pages", "build", "deploy:prod"} {
		if IsReservedTopLevelKey(job) {
			t.Fatalf("IsReservedTopLevelKey(%q) = true, want false", job)
		}
	}
}
