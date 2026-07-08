package docker

import (
	"net/url"
	"regexp"
)

// gclBuildVolumeRE matches gitlab-ci-local's build-volume naming pattern,
// gcl-<encodedJobName>-<jobId>-build, where jobId is a random number. This is
// the single source of truth for parsing those names; both the workspace's
// artifact fetch and the executor's log-capture path use GCLJobName below.
var gclBuildVolumeRE = regexp.MustCompile(`^gcl-(.+)-\d+-build$`)

// GCLJobName extracts the (URL-decoded) job name from a gcl-*-build volume name.
// gitlab-ci-local URL-encodes characters outside [\w-] into the segment, so we
// decode it to recover the original job name. Returns ok=false when the name
// does not match the build-volume shape.
func GCLJobName(vol string) (string, bool) {
	m := gclBuildVolumeRE.FindStringSubmatch(vol)
	if len(m) != 2 {
		return "", false
	}
	if decoded, err := url.PathUnescape(m[1]); err == nil {
		return decoded, true
	}
	return m[1], true
}
