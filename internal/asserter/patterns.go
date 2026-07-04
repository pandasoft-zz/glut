package asserter

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"
)

// maxScanLineSize bounds the per-line buffer bufio.Scanner grows into. The
// default 64 KiB limit is easily exceeded by a single long CI log line
// (e.g. a minified JSON blob echoed to stdout), which otherwise makes the
// scanner silently stop after that line — dropping every subsequent line
// from pattern matching without any indication that it happened.
const maxScanLineSize = 10 * 1024 * 1024

func matchTextPatterns(expected any, actual string) matchState {
	patterns, ok := toSlice(expected)
	if !ok {
		return matchValue(expected, actual)
	}

	lines, err := scanLines(actual)
	if err != nil {
		// A scan failure (a line larger than maxScanLineSize) is a broken
		// input, not a genuine mismatch: mark it IsError so not/and/or cannot
		// invert it into a pass.
		return matchState{Expected: expected, Actual: fmt.Sprintf("<failed to scan output: %v>", err), IsError: true}
	}
	for _, item := range patterns {
		pattern, ok := item.(string)
		if !ok {
			return matchState{Expected: expected, Actual: actual}
		}
		matched, patErr := matchSinglePattern(pattern, lines)
		if patErr != nil {
			return matchState{Expected: fmt.Sprintf("valid pattern %q", pattern), Actual: patErr.Error(), IsError: true}
		}
		if !matched {
			return matchState{Expected: pattern, Actual: actual}
		}
	}
	return matchState{Passed: true}
}

// matchSinglePattern reports whether pattern matches any line. It returns a
// non-nil error only when the pattern is itself broken (an uncompilable regexp
// in /re/ or !/re/ form), so callers can surface that as a configuration error
// rather than silently treating it as "no match" — which not: would otherwise
// invert into a false pass.
func matchSinglePattern(pattern string, lines []string) (bool, error) {
	switch {
	case strings.HasPrefix(pattern, "\\!"):
		return anyLine(lines, func(line string) bool { return strings.Contains(line, strings.TrimPrefix(pattern, "\\")) }), nil
	case strings.HasPrefix(pattern, "!/") && strings.HasSuffix(pattern, "/"):
		expression, err := regexp.Compile(strings.TrimSuffix(strings.TrimPrefix(pattern, "!/"), "/"))
		if err != nil {
			return false, fmt.Errorf("invalid regexp %q: %w", pattern, err)
		}
		return !anyLine(lines, expression.MatchString), nil
	case strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/"):
		expression, err := regexp.Compile(strings.TrimSuffix(strings.TrimPrefix(pattern, "/"), "/"))
		if err != nil {
			return false, fmt.Errorf("invalid regexp %q: %w", pattern, err)
		}
		return anyLine(lines, expression.MatchString), nil
	case strings.HasPrefix(pattern, "!"):
		needle := strings.TrimPrefix(pattern, "!")
		return !anyLine(lines, func(line string) bool { return strings.Contains(line, needle) }), nil
	default:
		return anyLine(lines, func(line string) bool { return strings.Contains(line, pattern) }), nil
	}
}

func scanLines(text string) ([]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64*1024), maxScanLineSize)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return lines, err
	}
	if len(lines) == 0 {
		return []string{""}, nil
	}
	return lines, nil
}

func anyLine(lines []string, match func(string) bool) bool {
	for _, line := range lines {
		if match(line) {
			return true
		}
	}
	return false
}
