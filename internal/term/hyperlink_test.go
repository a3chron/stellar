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
