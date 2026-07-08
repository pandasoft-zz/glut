package runner

import "testing"

func TestGitHelpersFailOutsideRepo(t *testing.T) {
	t.Parallel()
	if _, _, err := gitHeads(t.TempDir()); err == nil {
		t.Fatal("gitHeads outside a repo must fail")
	}
	if _, _, err := gitHeadCommit(t.TempDir()); err == nil {
		t.Fatal("gitHeadCommit outside a repo must fail")
	}
}

