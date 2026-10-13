// cmd/spinner.go is a tiny terminal spinner used anywhere stellar has to
// wait on something slow with no incremental progress of its own to show
// (right now, just `stellar auth`'s device-login poll).
package cmd

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// spinnerFrames cycles through a braille-dot animation, the same style used
// by most other CLI tools' spinners.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinnerInterval is how often the frame advances - a package var purely so
// a test could override it if a future test ever needed to, matching the
// authSleep pattern elsewhere in this package.
var spinnerInterval = 100 * time.Millisecond

// startSpinner prints label and, only when stdout looks like an actual
// terminal, animates a spinner after it until the returned stop function is
// called. On a non-terminal stdout (piped, redirected, CI, and every test -
// see internal/testutil.CaptureOutput, which replaces os.Stdout with a pipe)
// it just prints label once and returns a no-op stop, so nothing ever writes
// a stray '\r' or control sequence into output a script or test might parse.
//
// The returned stop must be called exactly once; calling it more than once
// is safe (it's idempotent) but pointless.
func startSpinner(label string) (stop func()) {
	if !isTerminal(os.Stdout) {
		fmt.Println(label)
		return func() {}
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(spinnerInterval)
		defer ticker.Stop()
		i := 0
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				fmt.Printf("\r%s %s", spinnerFrames[i%len(spinnerFrames)], label)
				i++
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			wg.Wait()
			// \r returns to column 0, \033[K clears to end of line - leaves
			// nothing behind for whatever prints next.
			fmt.Print("\r\033[K")
		})
	}
}
