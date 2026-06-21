package asserter

import (
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"os"

	"github.com/pandasoft-zz/glut/internal/config"
)

func runArtifactAsserts(asserts map[string]config.ArtifactAssert, ctx AssertContext) []AssertResult {
	var results []AssertResult
	for _, relativePath := range keysSorted(asserts) {
		basePath := quotedKeyPath("assert.artifacts", relativePath)
		fullPath, err := joinWorkspacePath(ctx.WorkspacePath, relativePath)
		if err != nil {
			results = append(results, failResult(basePath, "path inside workspace", err))
			continue
		}
		results = append(results, runArtifactAssert(basePath, fullPath, asserts[relativePath])...)
	}
	return results
}

func runArtifactAssert(basePath string, fullPath string, assert config.ArtifactAssert) (results []AssertResult) {

	info, err := os.Lstat(fullPath)
	exists := err == nil
	if assert.Exists != nil {
		results = append(results, resultFromBool(basePath+".exists", *assert.Exists == exists, *assert.Exists, exists))
	}
	if !exists {
		if err != nil && !os.IsNotExist(err) {
			results = append(results, failResult(basePath, "read artifact", err))
		}
		// Every other assertion needs the file's contents or stat info, which a
		// missing file cannot provide. Surface each one that is set as a failure
		// instead of returning silently, otherwise an assertion on an artifact the
		// job never produced would pass vacuously. Skip this when the caller
		// explicitly expects the file to be absent (`exists: false`) — there the
		// absence is the asserted outcome and any further assertion is moot.
		expectedAbsent := assert.Exists != nil && !*assert.Exists
		if !expectedAbsent {
			missing := []struct {
				set    bool
				suffix string
			}{
				{assert.Report != nil, ".report"},
				{assert.Contents != nil, ".contents"},
				{assert.Mode != "", ".mode"},
				{assert.Size != nil, ".size"},
				{assert.Filetype != "", ".filetype"},
				{assert.MD5 != "", ".md5"},
				{assert.SHA256 != "", ".sha256"},
			}
			for _, check := range missing {
				if check.set {
					results = append(results, failResult(basePath+check.suffix, "artifact to exist", "file not found"))
				}
			}
		}
		return
	}

	if assert.Mode != "" {
		actualMode := fmt.Sprintf("%04o", info.Mode().Perm())
		results = append(results, resultFromBool(basePath+".mode", assert.Mode == actualMode, assert.Mode, actualMode))
	}
	if assert.Size != nil {
		results = append(results, resultFromState(basePath+".size", matchValue(assert.Size, info.Size())))
	}
	if assert.Filetype != "" {
		results = append(results, resultFromBool(basePath+".filetype", assert.Filetype == fileTypeOf(info), assert.Filetype, fileTypeOf(info)))
	}
	if assert.Contents != nil {
		content, readErr := os.ReadFile(fullPath)
		if readErr != nil {
			results = append(results, failResult(basePath+".contents", "read file", readErr))
		} else {
			results = append(results, resultFromState(basePath+".contents", matchTextPatterns(assert.Contents, string(content))))
		}
	}
	if assert.Report != nil {
		results = append(results, assertReport(basePath, fullPath, assert.Report)...)
	}
	if assert.MD5 != "" || assert.SHA256 != "" {
		file, openErr := os.Open(fullPath)
		if openErr != nil {
			results = append(results, failResult(basePath, "open file", openErr))
		} else {
			defer func() {
				if closeErr := file.Close(); closeErr != nil {
					results = append(results, failResult(basePath, "close file", closeErr))
				}
			}()
			if assert.MD5 != "" {
				sum, sumErr := checksumFile(file, md5.New())
				if sumErr != nil {
					results = append(results, failResult(basePath+".md5", assert.MD5, sumErr))
				} else {
					results = append(results, resultFromBool(basePath+".md5", assert.MD5 == sum, assert.MD5, sum))
				}
			}
			if _, seekErr := file.Seek(0, 0); seekErr != nil && assert.SHA256 != "" {
				results = append(results, failResult(basePath+".sha256", assert.SHA256,
					fmt.Errorf("seek file: %w", seekErr)))
			} else if seekErr == nil && assert.SHA256 != "" {
				sum, sumErr := checksumFile(file, sha256.New())
				if sumErr != nil {
					results = append(results, failResult(basePath+".sha256", assert.SHA256, sumErr))
				} else {
					results = append(results, resultFromBool(basePath+".sha256", assert.SHA256 == sum, assert.SHA256, sum))
				}
			}
		}
	}
	return
}

func checksumFile(file *os.File, digest hash.Hash) (string, error) {
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func fileTypeOf(info os.FileInfo) string {
	mode := info.Mode()
	switch {
	case mode.IsRegular():
		return "file"
	case mode.IsDir():
		return "directory"
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode&os.ModeSocket != 0:
		return "socket"
	default:
		return mode.String()
	}
}
