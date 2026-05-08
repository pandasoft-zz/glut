package asserter

import (
	"bufio"
	"regexp"
	"strings"
)

func matchTextPatterns(expected any, actual string) matchState {
	patterns, ok := toSlice(expected)
	if !ok {
		return matchValue(expected, actual)
	}

	lines := scanLines(actual)
	for _, item := range patterns {
		pattern, ok := item.(string)
		if !ok {
			return matchState{Expected: expected, Actual: actual}
		}
		if !matchSinglePattern(pattern, lines) {
			return matchState{Expected: pattern, Actual: actual}
		}
	}
	return matchState{Passed: true}
}

func matchSinglePattern(pattern string, lines []string) bool {
	switch {
	case strings.HasPrefix(pattern, "\\!"):
		return anyLine(lines, func(line string) bool { return strings.Contains(line, strings.TrimPrefix(pattern, "\\")) })
	case strings.HasPrefix(pattern, "!/") && strings.HasSuffix(pattern, "/"):
		expression, err := regexp.Compile(strings.TrimSuffix(strings.TrimPrefix(pattern, "!/"), "/"))
		if err != nil {
			return false
		}
		return !anyLine(lines, expression.MatchString)
	case strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/"):
		expression, err := regexp.Compile(strings.TrimSuffix(strings.TrimPrefix(pattern, "/"), "/"))
		if err != nil {
			return false
		}
		return anyLine(lines, expression.MatchString)
	case strings.HasPrefix(pattern, "!"):
		needle := strings.TrimPrefix(pattern, "!")
		return !anyLine(lines, func(line string) bool { return strings.Contains(line, needle) })
	default:
		return anyLine(lines, func(line string) bool { return strings.Contains(line, pattern) })
	}
}

func scanLines(text string) []string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func anyLine(lines []string, match func(string) bool) bool {
	for _, line := range lines {
		if match(line) {
			return true
		}
	}
	return false
}
