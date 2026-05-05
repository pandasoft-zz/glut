package asserter

import (
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
	return fullPath, nil
}

func runGit(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run git %s in %s: %w; output: %s", strings.Join(args, " "), repoPath, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func pathForIndex(base string, index int, field string) string {
	if field == "" {
		return base + "[" + strconv.Itoa(index) + "]"
	}
	return base + "[" + strconv.Itoa(index) + "]." + field
}
