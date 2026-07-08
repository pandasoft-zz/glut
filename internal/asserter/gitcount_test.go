package asserter

import "testing"

func TestGitCommitCount(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	mustRunGit(t, work, "init", "-q", ".")
	mustRunGit(t, work, "config", "user.email", "test@example.com")
	mustRunGit(t, work, "config", "user.name", "Test User")
	mustRunGit(t, work, "commit", "-q", "--allow-empty", "-m", "first")
	mustRunGit(t, work, "commit", "-q", "--allow-empty", "-m", "second")

	bare := t.TempDir()
	mustRunGit(t, bare, "clone", "-q", "--bare", work, ".")

	if got, err := gitCommitCount(work, false); err != nil || got != 2 {
		t.Fatalf("gitCommitCount(worktree) = %d, %v; want 2", got, err)
	}
	if got, err := gitCommitCount(bare, true); err != nil || got != 2 {
		t.Fatalf("gitCommitCount(bare) = %d, %v; want 2", got, err)
	}

	if _, err := gitCommitCount(t.TempDir(), false); err == nil {
		t.Fatal("gitCommitCount outside a repo must fail")
	}
	if _, err := gitCommitCount(t.TempDir(), true); err == nil {
		t.Fatal("bare gitCommitCount outside a repo must fail")
	}
}
