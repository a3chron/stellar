package paths

import "testing"

// TestAPIURL_TrimsTrailingSlash guards against a trailing slash in
// STELLAR_API_URL (or the default) producing a double slash once a caller
// appends its own leading "/api/...": Next.js answers "//api/..." with a
// 308 redirect, which the CLI's no-redirect authed client then reports as a
// bare request failure instead of ever reaching the route.
func TestAPIURL_TrimsTrailingSlash(t *testing.T) {
	cases := []struct {
		name     string
		envVal   string
		fallback string
		want     string
	}{
		{"env var with one trailing slash", "http://127.0.0.1:3000/", "https://stellar.a3chron.dev", "http://127.0.0.1:3000"},
		{"env var with several trailing slashes", "http://127.0.0.1:3000///", "https://stellar.a3chron.dev", "http://127.0.0.1:3000"},
		{"env var with no trailing slash is untouched", "http://127.0.0.1:3000", "https://stellar.a3chron.dev", "http://127.0.0.1:3000"},
		{"falls back to the default, also trimmed", "", "https://stellar.a3chron.dev/", "https://stellar.a3chron.dev"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvAPIURL, tc.envVal)
			if got := APIURL(tc.fallback); got != tc.want {
				t.Errorf("APIURL() = %q, want %q", got, tc.want)
			}
		})
	}
}
