package docker

import "testing"

func TestGCLJobName(t *testing.T) {
	tests := []struct {
		vol    string
		want   string
		wantOK bool
	}{
		{"gcl-run-tests-12345-build", "run-tests", true},
		{"gcl-deploy%2Fweb-77-build", "deploy/web", true}, // URL-decoded
		{"gcl-build%20app-1-build", "build app", true},
		{"gcl-run-tests-12345-tmp", "", false}, // not a build volume
		{"gcl-build", "", false},               // no id segment
		{"gcl--99-build", "", false},           // empty job name
		{"gcl-x-build", "", false},             // no numeric id
		{"other-prefix-12-build", "", false},   // wrong prefix
	}
	for _, tt := range tests {
		t.Run(tt.vol, func(t *testing.T) {
			got, ok := GCLJobName(tt.vol)
			if ok != tt.wantOK || (ok && got != tt.want) {
				t.Fatalf("GCLJobName(%q) = (%q, %v), want (%q, %v)", tt.vol, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
