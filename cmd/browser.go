// cmd/browser.go opens the user's default browser for `stellar auth`'s
// device-login link. Opening a browser is best-effort and silent on
// failure: the code and link are always printed too (see cmd/auth.go), so a
// user on a headless box, an unsupported platform, or one where this simply
// fails can still complete the flow by hand.
package cmd

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

// openBrowser is a package var - like preview.go's startTerminal - purely so
// `go test` can never actually pop open a browser window regardless of the
// host environment, and so a test can override it to record what URL would
// have been opened. testing.Testing() is a property of the process, not of
// any environment variable a test might forget to unset.
var openBrowser = func(url string) {
	if testing.Testing() {
		return
	}
	_ = launchBrowser(url)
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
