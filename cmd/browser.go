// cmd/browser.go opens the user's default browser for `stellar auth`'s
// device-login link. Opening a browser is best-effort and silent on
// failure: the code and link are always printed too (see cmd/auth.go), so a
// user on a headless box, an unsupported platform, or one where this simply
// fails can still complete the flow by hand.
package cmd

import (
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/paths"
)

// openBrowser is a package var - like preview.go's startTerminal - purely so
// `go test` can never actually pop open a browser window regardless of the
// host environment, and so a test can override it to record what URL would
// have been opened. testing.Testing() is a property of the process, not of
// any environment variable a test might forget to unset.
var openBrowser = func(rawURL string) {
	if testing.Testing() || !isSafeBrowserURL(rawURL) {
		return
	}
	_ = launchBrowser(rawURL)
}

// isSafeBrowserURL reports whether rawURL is safe to hand to the OS's "open
// this URL" mechanism: every URL stellar ever opens should point at the
// configured hub, but this is still the one place stellar passes a string
// *the hub itself returned* (verificationUrlComplete) to a shell-adjacent
// command (open/xdg-open/rundll32), so it's validated the same way a
// browser's own URL bar would refuse to silently "upgrade" scheme or host:
// https to any host, or plain http only to a loopback address, and in both
// cases only to the exact host stellar is actually configured to talk to
// (paths.APIURL(api.BaseURL)) - never some other host the response might
// have been tampered with (or simply misconfigured) to point at. Anything
// else opens nothing; the link is always printed too, so the user can still
// open or copy it themselves.
func isSafeBrowserURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	expected, err := url.Parse(paths.APIURL(api.BaseURL))
	if err != nil {
		return false
	}
	if !strings.EqualFold(u.Host, expected.Host) {
		return false
	}

	switch strings.ToLower(u.Scheme) {
	case "https":
		return true
	case "http":
		return isLoopbackHostname(u.Hostname())
	default:
		return false
	}
}

// isLoopbackHostname reports whether host (already stripped of any port, as
// url.URL.Hostname() does) refers to the local machine.
func isLoopbackHostname(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// launchBrowser best-effort opens url in the user's default browser.
// Failures are deliberately swallowed - the caller always prints the link
// too, so there's nothing more useful to do here than let the user open it
// themselves.
func launchBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		// xdg-open needs an actual display session to do anything useful -
		// without one it just as often hangs or fails, so skip it entirely on
		// a headless box rather than leave a doomed process behind.
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return nil
		}
		return exec.Command("xdg-open", url).Start()
	}
}
