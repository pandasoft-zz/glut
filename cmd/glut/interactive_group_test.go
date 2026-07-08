package main

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pandasoft-zz/glut/internal/runner"
)

func TestGroupByDirSortsAndDeduplicates(t *testing.T) {
	t.Parallel()
	tests := []runner.ListedTest{
		{FilePath: filepath.Join("tests", "release", "a.yml")},
		{FilePath: filepath.Join("tests", "release", "b.yml")},
		{FilePath: filepath.Join("tests", "api", "c.yml")},
		{FilePath: "root.yml"},
	}
	got := groupByDir(tests)
	want := []string{".", filepath.Join("tests", "api"), filepath.Join("tests", "release")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("groupByDir() = %v, want %v", got, want)
	}

	if got := groupByDir(nil); len(got) != 0 {
		t.Fatalf("groupByDir(nil) = %v, want empty", got)
	}
}
