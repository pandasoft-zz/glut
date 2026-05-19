package asserter

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pandasoft-zz/glut/internal/config"
)

func runGitAsserts(asserts *config.GitAssert, ctx AssertContext) []AssertResult {
	if asserts == nil {
		return nil
	}

	var results []AssertResult
	if asserts.Origin != nil {
		originPath, err := ctx.OriginRepo.Path()
		if err != nil {
			return append(results, AssertResult{
				Path:     "assert.git.origin",
				Passed:   false,
				Expected: "accessible origin repo",
				Actual:   err.Error(),
			})
		}
		results = append(results, runGitRepoAssert("assert.git.origin", originPath, true, asserts.Origin)...)
	}
	if asserts.Workspace != nil {
		results = append(results, runGitRepoAssert("assert.git.workspace", ctx.WorkspacePath, false, asserts.Workspace)...)
	}
	return results
}

func runGitRepoAssert(basePath string, repoPath string, bare bool, assert *config.GitRepoAssert) []AssertResult {
	var results []AssertResult

	if assert.Commits != nil {
		count, err := gitCommitCount(repoPath, bare)
		if err != nil {
			results = append(results, failResult(basePath+".commits", assert.Commits, err))
		} else {
			results = append(results, resultFromState(basePath+".commits", matchValue(assert.Commits, count)))
		}
	}

	if assert.LastCommit != nil {
		results = append(results, runGitLastCommitAsserts(basePath+".last-commit", repoPath, bare, assert.LastCommit)...)
	}

	for _, relativePath := range keysSorted(assert.File) {
		path := quotedKeyPath(basePath+".file", relativePath)
		results = append(results, runGitFileAssert(path, repoPath, bare, relativePath, assert.File[relativePath])...)
	}

	if !bare && assert.Branch != nil {
		branch, err := runGit(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			results = append(results, failResult(basePath+".branch", assert.Branch, err))
		} else {
			results = append(results, resultFromState(basePath+".branch", matchValue(assert.Branch, branch)))
		}
	}

	if !bare && assert.Clean != nil {
		status, err := runGit(repoPath, "status", "--porcelain")
		if err != nil {
			results = append(results, failResult(basePath+".clean", *assert.Clean, err))
		} else {
			clean := strings.TrimSpace(status) == ""
			results = append(results, resultFromBool(basePath+".clean", *assert.Clean == clean, *assert.Clean, clean))
		}
	}

	return results
}

func gitCommitCount(repoPath string, bare bool) (int, error) {
	if bare {
		output, err := runGit("", "--git-dir", repoPath, "rev-list", "--count", "HEAD")
		if err != nil {
			return 0, err
		}
		var count int
		_, scanErr := fmt.Sscanf(output, "%d", &count)
		if scanErr != nil {
			return 0, fmt.Errorf("parse commit count %q: %w", output, scanErr)
		}
		return count, nil
	}

	output, err := runGit(repoPath, "rev-list", "--count", "HEAD")
	if err != nil {
		return 0, err
	}
	var count int
	_, scanErr := fmt.Sscanf(output, "%d", &count)
	if scanErr != nil {
		return 0, fmt.Errorf("parse commit count %q: %w", output, scanErr)
	}
	return count, nil
}

func runGitLastCommitAsserts(basePath string, repoPath string, bare bool, assert *config.GitLastCommitAssert) []AssertResult {
	var results []AssertResult
	fields := map[string]any{
		"author-name":  assert.AuthorName,
		"author-email": assert.AuthorEmail,
		"message":      assert.Message,
		"sha":          assert.SHA,
	}
	formats := map[string]string{
		"author-name":  "%an",
		"author-email": "%ae",
		"message":      "%B",
		"sha":          "%H",
	}

	for _, key := range []string{"author-name", "author-email", "message", "sha"} {
		expected := fields[key]
		if expected == nil {
			continue
		}
		var actual string
		var err error
		if bare {
			actual, err = runGit("", "--git-dir", repoPath, "log", "-1", "--format="+formats[key])
		} else {
			actual, err = runGit(repoPath, "log", "-1", "--format="+formats[key])
		}
		if err != nil {
			results = append(results, failResult(basePath+"."+key, expected, err))
			continue
		}
		results = append(results, resultFromState(basePath+"."+key, matchValue(expected, actual)))
	}

	return results
}

func runGitFileAssert(basePath string, repoPath string, bare bool, relativePath string, assert config.ArtifactAssert) []AssertResult {
	if bare {
		return runBareGitFileAssert(basePath, repoPath, relativePath, assert)
	}
	fullPath, err := joinWorkspacePath(repoPath, relativePath)
	if err != nil {
		return []AssertResult{failResult(basePath, "path inside workspace", err)}
	}
	return runArtifactAssert(basePath, fullPath, assert)
}

func runBareGitFileAssert(basePath string, repoPath string, relativePath string, assert config.ArtifactAssert) []AssertResult {
	var results []AssertResult
	cleanPath := filepath.ToSlash(filepath.Clean(relativePath))
	if cleanPath == ".." || strings.HasPrefix(cleanPath, "../") || strings.HasPrefix(cleanPath, "/") {
		return []AssertResult{failResult(basePath, "path inside repository", relativePath)}
	}

	_, existsErr := runGit("", "--git-dir", repoPath, "cat-file", "-e", "HEAD:"+cleanPath)
	exists := existsErr == nil
	if assert.Exists != nil {
		results = append(results, resultFromBool(basePath+".exists", *assert.Exists == exists, *assert.Exists, exists))
	}
	if !exists {
		if assert.Exists == nil || *assert.Exists {
			results = append(results, failResult(basePath, "git file to exist", existsErr))
		}
		return results
	}

	if assert.Contents != nil {
		content, err := runGit("", "--git-dir", repoPath, "show", "HEAD:"+cleanPath)
		if err != nil {
			results = append(results, failResult(basePath+".contents", assert.Contents, err))
		} else {
			results = append(results, resultFromState(basePath+".contents", matchTextPatterns(assert.Contents, content)))
		}
	}
	if assert.Mode != "" || assert.Filetype != "" {
		entry, err := runGit("", "--git-dir", repoPath, "ls-tree", "HEAD", cleanPath)
		if err != nil {
			results = append(results, failResult(basePath, "git tree entry", err))
			return results
		}
		fields := strings.Fields(entry)
		if len(fields) >= 2 {
			if assert.Mode != "" {
				actualMode := gitTreeModeToPermissionMode(fields[0])
				results = append(results, resultFromBool(basePath+".mode", assert.Mode == actualMode, assert.Mode, actualMode))
			}
			if assert.Filetype != "" {
				actualType := gitObjectTypeToFileType(fields[1])
				results = append(results, resultFromBool(basePath+".filetype", assert.Filetype == actualType, assert.Filetype, actualType))
			}
		}
	}
	return results
}

func gitTreeModeToPermissionMode(mode string) string {
	if len(mode) > 4 {
		return mode[len(mode)-4:]
	}
	return mode
}

func gitObjectTypeToFileType(gitType string) string {
	switch gitType {
	case "blob":
		return "file"
	case "tree":
		return "directory"
	default:
		return gitType
	}
}
