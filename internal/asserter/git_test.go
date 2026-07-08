package asserter

import (
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pandasoft-zz/glut/internal/config"
)

func TestRunGitAsserts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	origin := filepath.Join(root, "origin.git")
	workspace := filepath.Join(root, "workspace")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, source, "init")
	mustRunGit(t, source, "config", "user.email", "alice@example.com")
	mustRunGit(t, source, "config", "user.name", "Alice Example")
	if err := os.WriteFile(filepath.Join(source, "config.txt"), []byte("version: 2.0.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, source, "add", "config.txt")
	mustRunGit(t, source, "commit", "-m", "chore: update config")
	mustRunGit(t, source, "init", "--bare", origin)
	mustRunGit(t, source, "remote", "add", "origin", origin)
	mustRunGit(t, source, "branch", "-M", "main")
	mustRunGit(t, source, "push", "-u", "origin", "main")
	mustRunGit(t, root, "--git-dir", origin, "symbolic-ref", "HEAD", "refs/heads/main")

	mustRunGit(t, root, "clone", origin, workspace)
	mustRunGit(t, workspace, "checkout", "-b", "feature/new-version")
	if err := os.WriteFile(filepath.Join(workspace, "local-only.tmp"), []byte("tmp"), 0644); err != nil {
		t.Fatal(err)
	}

	asserts := config.AssertConfig{
		Git: &config.GitAssert{
			Origin: &config.GitRepoAssert{
				Commits: 1,
				LastCommit: &config.GitLastCommitAssert{
					AuthorName:  "Alice Example",
					AuthorEmail: "alice@example.com",
					Message:     "/chore: update.*/",
				},
				File: map[string]config.ArtifactAssert{
					"config.txt": {
						Exists:   boolPtr(true),
						Contents: []any{"version: 2.0.0"},
					},
				},
			},
			Workspace: &config.GitRepoAssert{
				Branch: "feature/new-version",
				Clean:  boolPtr(false),
				File: map[string]config.ArtifactAssert{
					"local-only.tmp": {
						Exists: boolPtr(true),
					},
				},
			},
		},
	}

	results := Run(asserts, AssertContext{
		OriginRepo:    NewFSOrigin(origin),
		WorkspacePath: workspace,
	})

	for _, result := range results {
		if !result.Passed {
			t.Fatalf("unexpected failure: %+v", result)
		}
	}
}

// TestBareGitFileAssertsSizeMD5SHA256AndReport guards against
// runBareGitFileAssert silently dropping size/md5/sha256/report assertions
// on files read from the bare origin repo (they used to pass vacuously,
// since only exists/contents/mode/filetype were evaluated).
func TestBareGitFileAssertsSizeMD5SHA256AndReport(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	origin := filepath.Join(root, "origin.git")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, source, "init")
	mustRunGit(t, source, "config", "user.email", "alice@example.com")
	mustRunGit(t, source, "config", "user.name", "Alice Example")

	fileContent := []byte("version: 2.0.0\n")
	if err := os.WriteFile(filepath.Join(source, "config.txt"), fileContent, 0644); err != nil {
		t.Fatal(err)
	}
	dotenvContent := []byte("APP_VERSION=2.0.0\n")
	if err := os.WriteFile(filepath.Join(source, "build.env"), dotenvContent, 0644); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, source, "add", "config.txt", "build.env")
	mustRunGit(t, source, "commit", "-m", "chore: add config")
	mustRunGit(t, source, "init", "--bare", origin)
	mustRunGit(t, source, "remote", "add", "origin", origin)
	mustRunGit(t, source, "branch", "-M", "main")
	mustRunGit(t, source, "push", "-u", "origin", "main")
	mustRunGit(t, root, "--git-dir", origin, "symbolic-ref", "HEAD", "refs/heads/main")

	wantMD5 := fmt.Sprintf("%x", md5.Sum(fileContent))
	wantSHA256 := fmt.Sprintf("%x", sha256.Sum256(fileContent))

	asserts := config.AssertConfig{
		Git: &config.GitAssert{
			Origin: &config.GitRepoAssert{
				File: map[string]config.ArtifactAssert{
					"config.txt": {
						Size:   len(fileContent),
						MD5:    wantMD5,
						SHA256: wantSHA256,
					},
					"build.env": {
						Report: &config.ReportAssert{
							Format: "dotenv",
							Keys: map[string]any{
								"APP_VERSION": "2.0.0",
							},
						},
					},
				},
			},
		},
	}

	results := Run(asserts, AssertContext{
		OriginRepo: NewFSOrigin(origin),
	})
	for _, result := range results {
		if !result.Passed {
			t.Fatalf("unexpected failure: %+v", result)
		}
	}

	// A wrong hash must fail, not pass vacuously.
	badAsserts := config.AssertConfig{
		Git: &config.GitAssert{
			Origin: &config.GitRepoAssert{
				File: map[string]config.ArtifactAssert{
					"config.txt": {MD5: "0000000000000000000000000000000"},
				},
			},
		},
	}
	badResults := Run(badAsserts, AssertContext{OriginRepo: NewFSOrigin(origin)})
	if !anyFailed(badResults) {
		t.Fatalf("expected a wrong md5 to fail, got %+v", badResults)
	}

	// An uppercase digest (as pasted from another tool) must still match the
	// lowercase %x output the bare-blob checksum comparison produces.
	upperAsserts := config.AssertConfig{
		Git: &config.GitAssert{
			Origin: &config.GitRepoAssert{
				File: map[string]config.ArtifactAssert{
					"config.txt": {
						MD5:    strings.ToUpper(wantMD5),
						SHA256: strings.ToUpper(wantSHA256),
					},
				},
			},
		},
	}
	upperResults := Run(upperAsserts, AssertContext{OriginRepo: NewFSOrigin(origin)})
	for _, result := range upperResults {
		if !result.Passed {
			t.Fatalf("uppercase digest should match: %+v", result)
		}
	}
}

// TestBareGitFileMissingFileReportsOncePerAssertedField guards against a
// missing bare-git file with `exists: true` (plus another field) reporting
// the same "file missing" fact twice — once under basePath+".exists" and
// again under a duplicate, unsuffixed basePath failure — unlike
// runArtifactAssert's local-file path, which reports one failure per field
// that was actually asserted.
func TestBareGitFileMissingFileReportsOncePerAssertedField(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	origin := filepath.Join(root, "origin.git")

	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, source, "init")
	mustRunGit(t, source, "config", "user.email", "alice@example.com")
	mustRunGit(t, source, "config", "user.name", "Alice Example")
	mustRunGit(t, source, "commit", "--allow-empty", "-m", "init")
	mustRunGit(t, source, "init", "--bare", origin)
	mustRunGit(t, source, "remote", "add", "origin", origin)
	mustRunGit(t, source, "branch", "-M", "main")
	mustRunGit(t, source, "push", "-u", "origin", "main")
	mustRunGit(t, root, "--git-dir", origin, "symbolic-ref", "HEAD", "refs/heads/main")

	t.Run("exists true alone reports exactly one failure", func(t *testing.T) {
		results := Run(config.AssertConfig{
			Git: &config.GitAssert{
				Origin: &config.GitRepoAssert{
					File: map[string]config.ArtifactAssert{
						"never-created.txt": {Exists: boolPtr(true)},
					},
				},
			},
		}, AssertContext{OriginRepo: NewFSOrigin(origin)})

		failed := failedResults(results)
		if len(failed) != 1 {
			t.Fatalf("expected exactly 1 failure for a missing file with only exists: true, got %d: %+v", len(failed), failed)
		}
	})

	t.Run("exists true plus contents reports one failure per field", func(t *testing.T) {
		results := Run(config.AssertConfig{
			Git: &config.GitAssert{
				Origin: &config.GitRepoAssert{
					File: map[string]config.ArtifactAssert{
						"never-created.txt": {
							Exists:   boolPtr(true),
							Contents: "anything",
						},
					},
				},
			},
		}, AssertContext{OriginRepo: NewFSOrigin(origin)})

		failed := failedResults(results)
		if len(failed) != 2 {
			t.Fatalf("expected exactly 2 failures (.exists and .contents), got %d: %+v", len(failed), failed)
		}
		var sawExists, sawContents bool
		for _, r := range failed {
			if strings.HasSuffix(r.Path, ".exists") {
				sawExists = true
			}
			if strings.HasSuffix(r.Path, ".contents") {
				sawContents = true
			}
		}
		if !sawExists || !sawContents {
			t.Fatalf("expected one .exists and one .contents failure, got %+v", failed)
		}
	})
}

func failedResults(results []AssertResult) []AssertResult {
	var failed []AssertResult
	for _, r := range results {
		if !r.Passed {
			failed = append(failed, r)
		}
	}
	return failed
}

func noSignGitEnv() []string {
	filtered := make([]string, 0, len(os.Environ())+5)
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch {
		case key == "GIT_CONFIG_NOSYSTEM",
			key == "GIT_CONFIG_GLOBAL",
			key == "GIT_CONFIG_SYSTEM",
			key == "GIT_CONFIG_COUNT",
			key == "GIT_DIR",
			key == "GIT_WORK_TREE",
			key == "GIT_INDEX_FILE",
			strings.HasPrefix(key, "GIT_CONFIG_KEY_"),
			strings.HasPrefix(key, "GIT_CONFIG_VALUE_"):
			// filtered out; replaced below
		default:
			filtered = append(filtered, kv)
		}
	}
	return append(filtered,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=commit.gpgSign",
		"GIT_CONFIG_VALUE_0=false",
	)
}

func mustRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = noSignGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run git %s in %s: %v; output: %s", strings.Join(args, " "), dir, err, strings.TrimSpace(string(out)))
	}
}

func boolPtr(value bool) *bool {
	return &value
}
