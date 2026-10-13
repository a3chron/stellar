package cmd

import (
	"testing"

	"github.com/a3chron/stellar/internal/paths"
)

// TestIsSafeBrowserURL exercises the pure allow/deny logic openBrowser gates
// on, without ever needing to actually launch anything.
func TestIsSafeBrowserURL(t *testing.T) {
	t.Setenv(paths.EnvAPIURL, "https://stellar.a3chron.dev")

	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"https to the configured host is allowed", "https://stellar.a3chron.dev/cli/authorize?code=ABCD-EFGH", true},
		{"http to the configured host is refused (not loopback)", "http://stellar.a3chron.dev/cli/authorize", false},
		{"https to a different host is refused", "https://evil.test/cli/authorize", false},
		{"a scheme other than http/https is refused", "javascript:alert(1)", false},
		{"a malformed url is refused", "://not a url", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSafeBrowserURL(tc.url); got != tc.want {
				t.Errorf("isSafeBrowserURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

// TestIsSafeBrowserURL_LoopbackDev covers the local-dev shape
// (STELLAR_API_URL=http://127.0.0.1:3000), where plain http is expected to
// work since there's no TLS cert for localhost.
func TestIsSafeBrowserURL_LoopbackDev(t *testing.T) {
	t.Setenv(paths.EnvAPIURL, "http://127.0.0.1:3000")

	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"http to the same loopback host:port is allowed", "http://127.0.0.1:3000/cli/authorize", true},
		{"http to a different port is refused", "http://127.0.0.1:4000/cli/authorize", false},
		// https is unconditionally fine once the host matches - per the rule
		// "https, OR http-with-loopback, AND the same host" - even though
		// the configured default for this host happens to be http.
		{"https to the same host is still allowed", "https://127.0.0.1:3000/cli/authorize", true},
		{"http to a non-loopback host is refused even if configured", "http://example.com:3000/cli/authorize", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSafeBrowserURL(tc.url); got != tc.want {
				t.Errorf("isSafeBrowserURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

func TestIsLoopbackHostname(t *testing.T) {
	cases := map[string]bool{
		"localhost":   true,
		"LOCALHOST":   true,
		"127.0.0.1":   true,
		"::1":         true,
		"example.com": false,
		"0.0.0.0":     false,
	}
	for host, want := range cases {
		if got := isLoopbackHostname(host); got != want {
			t.Errorf("isLoopbackHostname(%q) = %v, want %v", host, got, want)
		}
	}
}
