package workspace

import (
	"regexp"
	"strings"
)

func slugify(ref string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	slug := re.ReplaceAllString(ref, "-")
	return strings.ToLower(strings.Trim(slug, "-"))
}
