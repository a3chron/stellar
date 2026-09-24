package cmd

import "testing"

func TestIsNewerRelease(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"1.6.0", "1.5.0", true},
		{"1.5.0", "1.5.0", false},
		// A build ahead of GitHub's latest release (tag pushed, goreleaser
		// still running) must not be told to "update" back down.
		{"1.5.0", "1.6.0", false},
		{"1.10.0", "1.9.0", true}, // numeric, not lexical
		{"v1.6.1", "1.6.0", true},
		{"2.0", "1.9.9", true}, // missing patch counts as 0
		{"1.6.0", "1.6.0-rc1", false},
		// Unparsable versions fall back to plain inequality.
		{"nightly", "1.6.0", true},
		{"nightly", "nightly", false},
	}
	for _, tt := range tests {
		if got := isNewerRelease(tt.latest, tt.current); got != tt.want {
			t.Errorf("isNewerRelease(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}
