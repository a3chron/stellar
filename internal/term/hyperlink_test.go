package term

import (
	"strings"
	"testing"

	"github.com/fatih/color"
)

func TestHyperlink(t *testing.T) {
	const url = "https://stellar.a3chron.dev/a3chron/ctp-blue?review=1.0"

	t.Run("wraps the text in an OSC 8 sequence when colour is enabled", func(t *testing.T) {
		restore := color.NoColor
		color.NoColor = false
		defer func() { color.NoColor = restore }()

		got := Hyperlink(url, url)

		want := "\x1b]8;;" + url + "\x1b\\" +
			"\x1b[4m" + url + "\x1b[24m" +
			"\x1b]8;;\x1b\\"
		if got != want {
			t.Errorf("Hyperlink() = %q, want %q", got, want)
		}
		// The URL has to survive verbatim, or a terminal opens the wrong page.
		if !strings.Contains(got, url) {
			t.Errorf("Hyperlink() lost the url: %q", got)
		}
	})

	t.Run("falls back to plain text when colour is disabled", func(t *testing.T) {
		restore := color.NoColor
		color.NoColor = true
		defer func() { color.NoColor = restore }()

		got := Hyperlink(url, url)

		if got != url {
			t.Errorf("Hyperlink() = %q, want plain %q", got, url)
		}
		if strings.Contains(got, "\x1b") {
			t.Errorf("Hyperlink() leaked an escape sequence into plain output: %q", got)
		}
	})

	t.Run("supports link text that differs from the url", func(t *testing.T) {
		restore := color.NoColor
		color.NoColor = false
		defer func() { color.NoColor = restore }()

		got := Hyperlink(url, "review this config")

		if !strings.Contains(got, "review this config") {
			t.Errorf("Hyperlink() dropped the link text: %q", got)
		}
		if !strings.Contains(got, url) {
			t.Errorf("Hyperlink() dropped the url: %q", got)
		}
	})
}

// TestHyperlink_RefusesUnsafeURLs guards against a hub-provided URL (a draft
// or theme link is never typed by stellar itself) smuggling control
// characters into an OSC 8 escape sequence, or using a scheme a terminal
// might act on in some unexpected way (file://, javascript:, a bare
// "evil\x1b]8;;..." payload, ...). Any of these must fall back to plain
// text, exactly like the NO_COLOR case already does.
func TestHyperlink_RefusesUnsafeURLs(t *testing.T) {
	restore := color.NoColor
	color.NoColor = false
	defer func() { color.NoColor = restore }()

	cases := []struct {
		name string
		url  string
	}{
		{"javascript scheme", "javascript:alert(1)"},
		{"file scheme", "file:///etc/passwd"},
		{"no scheme at all", "stellar.a3chron.dev/a3chron/ctp-blue"},
		{"embedded ESC", "https://stellar.a3chron.dev/\x1b]8;;http://evil.test\x1b\\x\x1b]8;;\x1b\\"},
		{"embedded newline", "https://stellar.a3chron.dev/\nfake line"},
		{"embedded DEL", "https://stellar.a3chron.dev/\x7f"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Hyperlink(tc.url, "click here")
			if got != "click here" {
				t.Errorf("Hyperlink(%q) = %q, want plain text only", tc.url, got)
			}
			if strings.ContainsAny(got, "\x1b\x7f") {
				t.Errorf("Hyperlink(%q) leaked a control byte: %q", tc.url, got)
			}
		})
	}

	t.Run("still links a genuinely safe https URL", func(t *testing.T) {
		const safeURL = "https://stellar.a3chron.dev/a3chron/ctp-blue?review=1.0"
		got := Hyperlink(safeURL, safeURL)
		if !strings.Contains(got, "\x1b]8;;"+safeURL) {
			t.Errorf("Hyperlink() unexpectedly refused a safe url: %q", got)
		}
	})

	t.Run("still links a genuinely safe http URL", func(t *testing.T) {
		const httpURL = "http://127.0.0.1:3000/a3chron/ctp-blue"
		got := Hyperlink(httpURL, httpURL)
		if !strings.Contains(got, "\x1b]8;;"+httpURL) {
			t.Errorf("Hyperlink() unexpectedly refused a safe http url: %q", got)
		}
	})
}
