package cmd

import (
	"bufio"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPromptYesNo_Enter guards the fix for review item 20: pressing Enter at
// a "[y/N]"/"[Y/n]" prompt on a terminal must return the shown default, not
// error - only a non-interactive stdin that hit EOF with nothing typed at
// all has nobody there to even see the default.
func TestPromptYesNo_Enter(t *testing.T) {
	t.Run("blank line on a terminal returns the default (No)", func(t *testing.T) {
		r := bufio.NewReader(strings.NewReader("\n"))
		ok, err := promptYesNo(r, true, "Proceed?", false, "--yes")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("blank line on a terminal returns the default (Yes)", func(t *testing.T) {
		r := bufio.NewReader(strings.NewReader("\n"))
		ok, err := promptYesNo(r, true, "Proceed?", true, "--yes")
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("EOF with nothing typed on a terminal still returns the default", func(t *testing.T) {
		r := bufio.NewReader(strings.NewReader(""))
		ok, err := promptYesNo(r, true, "Proceed?", false, "--yes")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("EOF with nothing typed on a non-terminal errors naming the flag", func(t *testing.T) {
		r := bufio.NewReader(strings.NewReader(""))
		_, err := promptYesNo(r, false, "Proceed?", false, "--yes")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--yes")
	})

	t.Run("a blank PIPED line (not EOF) on a non-terminal still returns the default", func(t *testing.T) {
		// A real newline was read (ok=true) even though stdin isn't a
		// terminal - e.g. `printf '\n' | stellar ...` - so there WAS an
		// answer, just a blank one; this must not be confused with the
		// "nobody is there" EOF case.
		r := bufio.NewReader(strings.NewReader("\n"))
		ok, err := promptYesNo(r, false, "Proceed?", false, "--yes")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("a real y answer wins regardless of tty", func(t *testing.T) {
		r := bufio.NewReader(strings.NewReader("y\n"))
		ok, err := promptYesNo(r, false, "Proceed?", false, "--yes")
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("a real n answer wins over a Yes default", func(t *testing.T) {
		r := bufio.NewReader(strings.NewReader("n\n"))
		ok, err := promptYesNo(r, true, "Proceed?", true, "--yes")
		require.NoError(t, err)
		assert.False(t, ok)
	})
}

// TestPromptText_RequiredFieldWording guards review item 9: a required
// (no-default) prompt that DID get a line back - a terminal where the user
// just pressed Enter, or a piped explicit blank line - must not say "no
// answer on stdin", since stdin plainly answered; that wording is reserved
// for stdin hitting EOF with nothing read at all.
func TestPromptText_RequiredFieldWording(t *testing.T) {
	t.Run("blank line (Enter pressed) on a required field asks for a value, not stdin", func(t *testing.T) {
		r := bufio.NewReader(strings.NewReader("\n"))
		_, err := promptText(r, "Version", "", "--version <version>")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "a value is required")
		assert.NotContains(t, err.Error(), "no answer on stdin")
		assert.Contains(t, err.Error(), "--version <version>")
	})

	t.Run("true EOF with nothing typed still says no answer on stdin", func(t *testing.T) {
		r := bufio.NewReader(strings.NewReader(""))
		_, err := promptText(r, "Version", "", "--version <version>")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no answer on stdin")
	})

	t.Run("blank line with a default still returns the default", func(t *testing.T) {
		r := bufio.NewReader(strings.NewReader("\n"))
		v, err := promptText(r, "Version", "1.3", "--version <version>")
		require.NoError(t, err)
		assert.Equal(t, "1.3", v)
	})
}

// TestPromptSelect_RequiredFieldWording mirrors
// TestPromptText_RequiredFieldWording for promptSelect's no-default case.
func TestPromptSelect_RequiredFieldWording(t *testing.T) {
	t.Run("blank line with no default asks for a value, not stdin", func(t *testing.T) {
		r := bufio.NewReader(strings.NewReader("\n"))
		_, err := promptSelect(r, "Pick one", []string{"a", "b"}, -1, "--slug <slug>")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "a value is required")
		assert.NotContains(t, err.Error(), "no answer on stdin")
	})

	t.Run("true EOF with no default says no answer on stdin", func(t *testing.T) {
		r := bufio.NewReader(strings.NewReader(""))
		_, err := promptSelect(r, "Pick one", []string{"a", "b"}, -1, "--slug <slug>")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no answer on stdin")
	})
}
