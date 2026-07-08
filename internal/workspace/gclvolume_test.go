package workspace

import (
	"reflect"
	"testing"
)

func TestSelectGCLArtifactVolumes(t *testing.T) {
	tests := []struct {
		name     string
		preRun   []string
		current  []string
		jobNames []string
		want     []string
	}{
		{
			name:     "new build volume for our job is selected",
			preRun:   []string{},
			current:  []string{"gcl-run-tests-12345-build", "gcl-run-tests-12345-tmp"},
			jobNames: []string{"run-tests"},
			want:     []string{"gcl-run-tests-12345-build"},
		},
		{
			name:     "pre-existing volume is ignored",
			preRun:   []string{"gcl-run-tests-111-build"},
			current:  []string{"gcl-run-tests-111-build", "gcl-run-tests-222-build"},
			jobNames: []string{"run-tests"},
			want:     []string{"gcl-run-tests-222-build"},
		},
		{
			name:     "concurrent run's volume for a different job is NOT clobbered",
			preRun:   []string{},
			current:  []string{"gcl-build-100-build", "gcl-deploy-200-build"},
			jobNames: []string{"build"},
			want:     []string{"gcl-build-100-build"},
		},
		{
			name:     "result is sorted deterministically",
			preRun:   []string{},
			current:  []string{"gcl-build-300-build", "gcl-build-100-build", "gcl-build-200-build"},
			jobNames: []string{"build"},
			want:     []string{"gcl-build-100-build", "gcl-build-200-build", "gcl-build-300-build"},
		},
		{
			name:     "job name containing dashes is matched correctly",
			preRun:   []string{},
			current:  []string{"gcl-test-2-999-build"},
			jobNames: []string{"test-2"},
			want:     []string{"gcl-test-2-999-build"},
		},
		{
			// gitlab-ci-local URL-encodes characters outside [\w-] into the
			// volume segment; selection must decode it to match the raw job name.
			name:     "url-encoded job name is decoded and matched",
			preRun:   []string{},
			current:  []string{"gcl-deploy%2Fweb-77-build", "gcl-other%20job-88-build"},
			jobNames: []string{"deploy/web"},
			want:     []string{"gcl-deploy%2Fweb-77-build"},
		},
		{
			name:     "no job names falls back to all new build volumes",
			preRun:   []string{},
			current:  []string{"gcl-anything-1-build", "gcl-other-2-tmp"},
			jobNames: nil,
			want:     []string{"gcl-anything-1-build"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectGCLArtifactVolumes(tt.preRun, tt.current, tt.jobNames)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("selectGCLArtifactVolumes() = %v, want %v", got, tt.want)
			}
		})
	}
}

