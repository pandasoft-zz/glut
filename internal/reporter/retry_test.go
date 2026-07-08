package reporter

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// TestSinkTestRetryOutput pins each progress sink's retry notification: the
// console sinks announce the retry (unless quiet), while the JSON console and
// the file reports stay silent — a retry is not a result.
func TestSinkTestRetryOutput(t *testing.T) {
	t.Parallel()
	retryErr := errors.New("volume create: daemon busy")

	consoleCases := []struct {
		name       string
		format     string
		quiet      bool
		wantOutput bool
	}{
		{name: "pretty announces retry", format: "pretty", wantOutput: true},
		{name: "pretty quiet is silent", format: "pretty", quiet: true, wantOutput: false},
		{name: "dots announces retry", format: "dots", wantOutput: true},
		{name: "dots quiet is silent", format: "dots", quiet: true, wantOutput: false},
		{name: "json is silent", format: "json", wantOutput: false},
	}
	for _, tc := range consoleCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			sink, err := NewConsole(ConsoleOptions{Format: tc.format, Quiet: tc.quiet, Writer: &out})
			if err != nil {
				t.Fatalf("NewConsole(%s) error = %v", tc.format, err)
			}
			sink.TestRetry("release test", retryErr)
			got := out.String()
			if tc.wantOutput {
				if !strings.Contains(got, "retrying") || !strings.Contains(got, "release test") {
					t.Fatalf("retry output = %q, want the test name and a retry notice", got)
				}
			} else if got != "" {
				t.Fatalf("retry output = %q, want none", got)
			}
		})
	}

	t.Run("file reports ignore retries", func(t *testing.T) {
		t.Parallel()
		junit := NewJUnit()
		tap := NewTAP()
		junit.TestRetry("release test", retryErr)
		tap.TestRetry("release test", retryErr)
	})
}
