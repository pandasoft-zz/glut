package asserter

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tidwall/gjson"
)

func TestHelperFunctions(t *testing.T) {
	t.Run("describeValue", func(t *testing.T) {
		tests := []struct {
			name  string
			value any
			want  string
		}{
			{name: "nil", value: nil, want: "null"},
			{name: "string", value: "hello", want: "hello"},
			{name: "error", value: errors.New("boom"), want: "boom"},
			{name: "json", value: map[string]any{"a": 1}, want: `{"a":1}`},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if got := describeValue(tt.value); got != tt.want {
					t.Fatalf("describeValue() = %q, want %q", got, tt.want)
				}
			})
		}
	})

	t.Run("toFloat64", func(t *testing.T) {
		values := []any{int8(1), int16(2), int32(3), int64(4), uint(5), uint8(6), uint16(7), uint32(8), uint64(9), float32(1.5), float64(2.5), json.Number("3.5")}
		for _, value := range values {
			if _, ok := toFloat64(value); !ok {
				t.Fatalf("toFloat64(%T) should convert", value)
			}
		}
		if _, ok := toFloat64(json.Number("bad")); ok {
			t.Fatal("invalid json.Number should fail")
		}
		if _, ok := toFloat64("nope"); ok {
			t.Fatal("string should not convert")
		}
	})

	t.Run("toSlice", func(t *testing.T) {
		if _, ok := toSlice(nil); ok {
			t.Fatal("nil should not convert to slice")
		}
		if got, ok := toSlice([]string{"a", "b"}); !ok || len(got) != 2 {
			t.Fatalf("slice conversion failed: %v %v", got, ok)
		}
		if got, ok := toSlice([2]int{1, 2}); !ok || len(got) != 2 {
			t.Fatalf("array conversion failed: %v %v", got, ok)
		}
		if _, ok := toSlice("not-slice"); ok {
			t.Fatal("string should not convert to slice")
		}
	})

	t.Run("toStringMap", func(t *testing.T) {
		if _, ok := toStringMap(nil); ok {
			t.Fatal("nil should not convert to map")
		}
		if got, ok := toStringMap(map[string]int{"a": 1}); !ok || got["a"] != 1 {
			t.Fatalf("map conversion failed: %v %v", got, ok)
		}
		if _, ok := toStringMap(map[int]string{1: "a"}); ok {
			t.Fatal("non-string key map should not convert")
		}
	})

	t.Run("deepEqualValue", func(t *testing.T) {
		if !deepEqualValue([]any{1, "a"}, []any{1.0, "a"}) {
			t.Fatal("slice deep equal should pass")
		}
		if !deepEqualValue(map[string]any{"a": 1}, map[string]any{"a": 1.0}) {
			t.Fatal("map deep equal should pass")
		}
		if deepEqualValue(map[string]any{"a": 1}, map[string]any{"b": 1}) {
			t.Fatal("different maps should fail")
		}
	})

	t.Run("joinWorkspacePath", func(t *testing.T) {
		root := t.TempDir()
		if got, err := joinWorkspacePath(root, "."); err != nil || got != root {
			t.Fatalf("dot path failed: %q %v", got, err)
		}
		if _, err := joinWorkspacePath(root, filepath.Join(root, "abs.txt")); err == nil {
			t.Fatal("absolute path should fail")
		}
		if _, err := joinWorkspacePath(root, ".."); err == nil {
			t.Fatal("parent path should fail")
		}
	})

	// TestJoinWorkspacePathRejectsSymlinkEscape guards against a pipeline-created
	// symlink pointing outside the workspace being followed by
	// contents/md5/sha256 asserts — the lexical-only checks above pass for a
	// symlink named e.g. "link" that itself points at ../../etc/passwd, since
	// "link" alone has no ".." component.
	t.Run("joinWorkspacePath rejects a symlink escaping the workspace", func(t *testing.T) {
		root := t.TempDir()
		outsideDir := t.TempDir()
		outsideFile := filepath.Join(outsideDir, "secret.txt")
		if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
			t.Fatal(err)
		}
		linkPath := filepath.Join(root, "link")
		if err := os.Symlink(outsideFile, linkPath); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}

		if _, err := joinWorkspacePath(root, "link"); err == nil {
			t.Fatal("expected joinWorkspacePath to reject a symlink escaping the workspace")
		}
	})

	t.Run("joinWorkspacePath allows a symlink that stays inside the workspace", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
		linkPath := filepath.Join(root, "link")
		if err := os.Symlink(filepath.Join(root, "real.txt"), linkPath); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}

		got, err := joinWorkspacePath(root, "link")
		if err != nil {
			t.Fatalf("expected an in-workspace symlink to be allowed, got error: %v", err)
		}
		if got != linkPath {
			t.Fatalf("joinWorkspacePath() = %q, want %q", got, linkPath)
		}
	})

	t.Run("pathForIndex", func(t *testing.T) {
		if got := pathForIndex("base", 2, "field"); got != "base[2].field" {
			t.Fatalf("pathForIndex with field = %q", got)
		}
		if got := pathForIndex("base", 2, ""); got != "base[2]" {
			t.Fatalf("pathForIndex without field = %q", got)
		}
	})

	t.Run("lengthOf", func(t *testing.T) {
		if length, ok := lengthOf("abc"); !ok || length != 3 {
			t.Fatalf("string length failed: %d %v", length, ok)
		}
		if _, ok := lengthOf(10); ok {
			t.Fatal("number should not have length")
		}
		// A JSON null field decodes to a nil any; have-len: 0 must match it
		// the same way it matches an empty string/slice/map.
		if length, ok := lengthOf(nil); !ok || length != 0 {
			t.Fatalf("nil length = %d, %v; want 0, true (so have-len: 0 matches a null field)", length, ok)
		}
	})

	t.Run("asJSONBytes", func(t *testing.T) {
		if got, err := asJSONBytes("abc"); err != nil || string(got) != "abc" {
			t.Fatalf("string json bytes failed: %q %v", got, err)
		}
		if got, err := asJSONBytes([]byte("xyz")); err != nil || string(got) != "xyz" {
			t.Fatalf("bytes json bytes failed: %q %v", got, err)
		}
		got, err := asJSONBytes(map[string]any{"a": 1})
		if err != nil || string(got) != `{"a":1}` {
			t.Fatalf("map json bytes failed: %q %v", got, err)
		}
	})

	t.Run("gjsonValue", func(t *testing.T) {
		if got := gjsonValue(gjson.Parse(`{"a":"b"}`).Get("a")); got != "b" {
			t.Fatalf("string gjson = %#v", got)
		}
		if got := gjsonValue(gjson.Parse(`{"a":3.5}`).Get("a")); got != 3.5 {
			t.Fatalf("number gjson = %#v", got)
		}
		if got := gjsonValue(gjson.Parse(`{"a":true}`).Get("a")); got != true {
			t.Fatalf("bool gjson = %#v", got)
		}
		if got := gjsonValue(gjson.Parse(`{"a":{"b":1}}`).Get("a")); describeValue(got) != `{"b":1}` {
			t.Fatalf("json gjson = %#v", got)
		}
		if got := gjsonValue(gjson.Result{}); got != nil {
			t.Fatalf("missing gjson = %#v", got)
		}
		if got := gjsonValue(gjson.Parse(`{"a":null}`).Get("a")); got != nil {
			t.Fatalf("null gjson = %#v", got)
		}
	})

	t.Run("checksumFile", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "sum.txt")
		if err := os.WriteFile(path, []byte("abc"), 0644); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if closeErr := file.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
		}()

		sum, err := checksumFile(file, md5.New())
		if err != nil {
			t.Fatal(err)
		}
		if sum != "900150983cd24fb0d6963f7d28e17f72" {
			t.Fatalf("checksumFile() = %q", sum)
		}
	})
}

func TestRunGitReturnsErrorForMissingRepo(t *testing.T) {
	if _, err := runGit("", "definitely-not-a-git-command"); err == nil {
		t.Fatal("expected runGit to fail")
	}
}

// TestRunGitIgnoresInheritedGitDirEnv guards against runGit inheriting
// GIT_DIR/GIT_WORK_TREE/GIT_INDEX_FILE from the host process environment: git
// honors those over cmd.Dir, so a parent process that itself runs git
// commands (setting these for its own purposes) would make every assertion
// silently operate on the wrong repository.
func TestRunGitIgnoresInheritedGitDirEnv(t *testing.T) {
	target := initGitRepoWithFile(t, "marker.txt", "target-repo-content")
	decoy := initGitRepoWithFile(t, "marker.txt", "decoy-repo-content")

	t.Setenv("GIT_DIR", filepath.Join(decoy, ".git"))
	t.Setenv("GIT_WORK_TREE", decoy)

	out, err := runGit(target, "show", "HEAD:marker.txt")
	if err != nil {
		t.Fatalf("runGit() error = %v", err)
	}
	if out != "target-repo-content" {
		t.Fatalf("runGit() = %q, want the target repo's content (inherited GIT_DIR/GIT_WORK_TREE must not redirect it to the decoy repo)", out)
	}
}

func initGitRepoWithFile(t *testing.T, name string, content string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	run("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", name)
	run("commit", "-m", "add "+name)
	return dir
}

func TestFileTypeHelpers(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "file.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Lstat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileTypeOf(fileInfo); got != "file" {
		t.Fatalf("fileTypeOf(file) = %q", got)
	}

	dirInfo, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileTypeOf(dirInfo); got != "directory" {
		t.Fatalf("fileTypeOf(dir) = %q", got)
	}

	linkPath := filepath.Join(root, "link.txt")
	if err := os.Symlink(filePath, linkPath); err == nil {
		linkInfo, statErr := os.Lstat(linkPath)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if got := fileTypeOf(linkInfo); got != "symlink" {
			t.Fatalf("fileTypeOf(symlink) = %q", got)
		}
	}

	if got := gitObjectTypeToFileType("blob"); got != "file" {
		t.Fatalf("blob type = %q", got)
	}
	if got := gitObjectTypeToFileType("tree"); got != "directory" {
		t.Fatalf("tree type = %q", got)
	}
	if got := gitObjectTypeToFileType("commit"); got != "commit" {
		t.Fatalf("other type = %q", got)
	}
}
