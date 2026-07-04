package docker

import (
	"reflect"
	"testing"
)

func TestVolumeMounts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		useDocker bool
		workDir   string
		volName   string
		strategy  string
		want      []string
	}{
		{
			name:      "shell mode needs no mounts",
			useDocker: false,
			workDir:   "/work",
			volName:   "glut-abc",
			strategy:  VolumeStrategyVolume,
			want:      nil,
		},
		{
			name:      "bind strategy mounts the host path at the same path",
			useDocker: true,
			workDir:   "/work",
			strategy:  VolumeStrategyBind,
			want:      []string{"/work:/work"},
		},
		{
			name:      "volume strategy mounts the named volume at the work dir",
			useDocker: true,
			workDir:   "/work",
			volName:   "glut-abc",
			strategy:  VolumeStrategyVolume,
			want:      []string{"glut-abc:/work"},
		},
		{
			name:      "volume strategy without a volume name mounts nothing",
			useDocker: true,
			workDir:   "/work",
			strategy:  VolumeStrategyVolume,
			want:      nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VolumeMounts(tt.useDocker, tt.workDir, tt.volName, tt.strategy)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("VolumeMounts() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtraHosts(t *testing.T) {
	t.Parallel()
	if got := ExtraHosts(false, "10.0.0.5"); got != nil {
		t.Fatalf("shell mode should need no extra hosts, got %v", got)
	}
	want := []string{"host.docker.internal:10.0.0.5", "glut-mock:10.0.0.5"}
	if got := ExtraHosts(true, "10.0.0.5"); !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtraHosts() = %v, want %v", got, want)
	}
}

func TestOutboundIPReturnsSomething(t *testing.T) {
	t.Parallel()
	// The result is environment-dependent (a local IP or the
	// host.docker.internal fallback); assert only that it is never empty.
	if got := OutboundIP(); got == "" {
		t.Fatal("OutboundIP() returned an empty string")
	}
}
