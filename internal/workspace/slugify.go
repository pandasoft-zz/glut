package workspace

import (
	"regexp"
	"strings"
)

var nonAlphanumRE = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// maxSlugLength matches real GitLab's limit on CI_COMMIT_REF_SLUG and
// CI_PROJECT_PATH_SLUG; a longer branch/tag/path name diverges from what a
// real pipeline would see.
const maxSlugLength = 63

func slugify(ref string) string {
	slug := strings.ToLower(nonAlphanumRE.ReplaceAllString(ref, "-"))
	if len(slug) > maxSlugLength {
		slug = slug[:maxSlugLength]
	}
	return strings.Trim(slug, "-")
}
