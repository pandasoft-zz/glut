package mockserver

import "testing"

func TestIsUnsetIdentifier(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		v    any
		want bool
	}{
		{"int zero is unset", int(0), true},
		{"int64 zero is unset", int64(0), true},
		{"float64 zero is unset (JSON numbers)", float64(0), true},
		{"non-zero int is set", 7, false},
		{"non-zero int64 is set", int64(7), false},
		{"non-zero float64 is set", float64(7), false},
		{"string is never unset", "0", false},
		{"nil is never unset", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isUnsetIdentifier(tt.v); got != tt.want {
				t.Fatalf("isUnsetIdentifier(%#v) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}
