package asserter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

func passResult(path string) AssertResult {
	return AssertResult{Path: path, Passed: true}
}

func failResult(path string, expected any, actual any) AssertResult {
	return AssertResult{
		Path:     path,
		Passed:   false,
		Expected: describeValue(expected),
		Actual:   describeValue(actual),
	}
}

func keysSorted[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func quotedKeyPath(base string, key string) string {
	return base + ".\"" + strings.ReplaceAll(key, "\"", "\\\"") + "\""
}

func describeValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		return typed
	case error:
		return typed.Error()
	}

	data, err := json.Marshal(value)
	if err == nil {
		return string(data)
	}
	return fmt.Sprintf("%v", value)
}

func toFloat64(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	}
	return 0, false
}

func toSlice(value any) ([]any, bool) {
	if value == nil {
		return nil, false
	}
	if values, ok := value.([]any); ok {
		return values, true
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Array {
		return nil, false
	}

	out := make([]any, reflected.Len())
	for i := 0; i < reflected.Len(); i++ {
		out[i] = reflected.Index(i).Interface()
	}
	return out, true
}

func toStringMap(value any) (map[string]any, bool) {
	if value == nil {
		return nil, false
	}
	if typed, ok := value.(map[string]any); ok {
		return typed, true
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Map || reflected.Type().Key().Kind() != reflect.String {
		return nil, false
	}

	out := make(map[string]any, reflected.Len())
	for _, key := range reflected.MapKeys() {
		out[key.String()] = reflected.MapIndex(key).Interface()
	}
	return out, true
}

func deepEqualValue(expected any, actual any) bool {
	if expected == nil || actual == nil {
		return expected == actual
	}
	if expectedNumber, ok := toFloat64(expected); ok {
		actualNumber, actualOK := toFloat64(actual)
		return actualOK && expectedNumber == actualNumber
	}

	expectedSlice, expectedSliceOK := toSlice(expected)
	actualSlice, actualSliceOK := toSlice(actual)
	if expectedSliceOK || actualSliceOK {
		if !expectedSliceOK || !actualSliceOK || len(expectedSlice) != len(actualSlice) {
			return false
		}
		for i := range expectedSlice {
			if !deepEqualValue(expectedSlice[i], actualSlice[i]) {
				return false
			}
		}
		return true
	}

	expectedMap, expectedMapOK := toStringMap(expected)
	actualMap, actualMapOK := toStringMap(actual)
	if expectedMapOK || actualMapOK {
		if !expectedMapOK || !actualMapOK || len(expectedMap) != len(actualMap) {
			return false
		}
		for key, expectedValue := range expectedMap {
			actualValue, ok := actualMap[key]
			if !ok || !deepEqualValue(expectedValue, actualValue) {
				return false
			}
		}
		return true
	}

	return reflect.DeepEqual(expected, actual)
}

func joinWorkspacePath(root string, relativePath string) (string, error) {
	if filepath.IsAbs(relativePath) {
		return "", fmt.Errorf("absolute path is not allowed: %s", relativePath)
	}

	cleanPath := filepath.Clean(relativePath)
	if cleanPath == "." {
		return root, nil
	}
	if strings.HasPrefix(cleanPath, ".."+string(os.PathSeparator)) || cleanPath == ".." {
		return "", fmt.Errorf("path escapes workspace: %s", relativePath)
	}

	fullPath := filepath.Join(root, cleanPath)
	relative, err := filepath.Rel(root, fullPath)
	if err != nil {
		return "", fmt.Errorf("resolve path %s: %w", relativePath, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace: %s", relativePath)
	}

	// The checks above are purely lexical; a symlink created by the pipeline
	// (or one of its ancestor directories) can still point outside root, which
	// would let contents/md5/sha256 read a file the "path escapes workspace"
	// error implies is impossible. Resolve symlinks and re-check containment.
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		// root should always exist; if it doesn't, let the caller's own
		// stat/open report a clearer error than one raised here.
		return fullPath, nil //nolint:nilerr
	}
	resolvedPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		// fullPath (or a component of it) does not exist — nothing to
		// resolve; let the caller's own stat/open report the real error.
		return fullPath, nil //nolint:nilerr
	}
	resolvedRel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil {
		return "", fmt.Errorf("resolve symlinked path %s: %w", relativePath, err)
	}
	if resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace: %s", relativePath)
	}

	return fullPath, nil
}

func runGit(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	cmd.Env = sanitizedGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run git %s in %s: %w; output: %s", strings.Join(args, " "), repoPath, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

// runGitBytes returns raw, untrimmed stdout — unlike runGit, which merges
// stderr into the result and trims trailing newlines (safe for text output
// such as commit metadata, but corrupt for binary blob content or exact
// size/checksum measurement).
func runGitBytes(repoPath string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	cmd.Env = sanitizedGitEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("run git %s in %s: %w; output: %s", strings.Join(args, " "), repoPath, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// sanitizedGitEnv returns the process environment with GIT_DIR/GIT_WORK_TREE/
// GIT_INDEX_FILE removed. If any of these are set in the host environment
// (e.g. inherited from a parent process that itself runs git commands), git
// ignores cmd.Dir entirely and operates on whatever repo they point at
// instead of the one being asserted on.
func sanitizedGitEnv() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE":
			continue
		}
		filtered = append(filtered, kv)
	}
	return filtered
}

func pathForIndex(base string, index int, field string) string {
	if field == "" {
		return base + "[" + strconv.Itoa(index) + "]"
	}
	return base + "[" + strconv.Itoa(index) + "]." + field
}
