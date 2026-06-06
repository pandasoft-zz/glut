package workspace

import (
	"regexp"
	"strings"
)

var nonAlphanumRE = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func slugify(ref string) string {
	slug := nonAlphanumRE.ReplaceAllString(ref, "-")
	return strings.ToLower(strings.Trim(slug, "-"))
}
