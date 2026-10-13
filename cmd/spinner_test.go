package cmd

import (
	"testing"

	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
)

// TestStartSpinner_NonTerminalPrintsLabelOnce guards review item 5: on a
// non-terminal stdout (every test's captured stdout is a pipe, exactly like
// a piped/redirected real run), startSpinner must fall back to printing the
// label once and returning a no-op stop - never spin up a goroutine that
// writes '\r'/animation frames a script or test might have to parse around.
func TestStartSpinner_NonTerminalPrintsLabelOnce(t *testing.T) {
	out := testutil.CaptureOutput(t, func() {
		stop := startSpinner("Waiting for you to approve in the browser...")
		stop()
		stop() // idempotent - must not panic or double-print
	})
	assert.Equal(t, "Waiting for you to approve in the browser...\n", out)
}
