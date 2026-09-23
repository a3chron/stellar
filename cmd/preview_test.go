package cmd

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLookPath returns an exec.LookPath-shaped func that reports "found" only
// for the given names, so shell/terminal selection can be tested without
// touching the real PATH.
func fakeLookPath(available ...string) func(string) (string, error) {
	set := make(map[string]bool, len(available))
	for _, a := range available {
		set[a] = true
	}
	return func(name string) (string, error) {
		if set[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
}

func TestSelectPreviewShell(t *testing.T) {
	t.Run("explicit flag wins outright", func(t *testing.T) {
		got := selectPreviewShell("nu", "/bin/bash", fakeLookPath("bash", "fish"))
		assert.Equal(t, "nu", got)
	})

	t.Run("prefers $SHELL over fish/zsh/bash", func(t *testing.T) {
		got := selectPreviewShell("", "/usr/bin/fish", fakeLookPath("fish", "zsh", "/usr/bin/fish"))
		assert.Equal(t, "/usr/bin/fish", got)
	})

	t.Run("falls back to fish, then zsh, then bash", func(t *testing.T) {
		assert.Equal(t, "fish", selectPreviewShell("", "", fakeLookPath("fish", "zsh", "bash")))
		assert.Equal(t, "zsh", selectPreviewShell("", "", fakeLookPath("zsh", "bash")))
		assert.Equal(t, "bash", selectPreviewShell("", "", fakeLookPath("bash")))
	})

	t.Run("falls back to sh when nothing is found", func(t *testing.T) {
		assert.Equal(t, "sh", selectPreviewShell("", "", fakeLookPath()))
	})

	t.Run("ignores an unusable $SHELL", func(t *testing.T) {
		got := selectPreviewShell("", "/usr/bin/nonexistent-shell", fakeLookPath("fish"))
		assert.Equal(t, "fish", got)
	})
}

func TestShellArgsFor(t *testing.T) {
	assert.Equal(t, []string{"fish", "-l"}, shellArgsFor("fish"))
	assert.Equal(t, []string{"/usr/bin/zsh", "-l"}, shellArgsFor("/usr/bin/zsh"))
	assert.Equal(t, []string{"sh"}, shellArgsFor("sh"))
}

func TestPreviewTerminalCandidates(t *testing.T) {
	t.Run("explicit and $TERMINAL come first, deduplicated", func(t *testing.T) {
		got := previewTerminalCandidates("kitty", "kitty")
		assert.Equal(t, "kitty", got[0])
		count := 0
		for _, c := range got {
			if c == "kitty" {
				count++
			}
		}
		assert.Equal(t, 1, count, "kitty must not be listed twice")
	})

	t.Run("known terminals follow, in order", func(t *testing.T) {
		got := previewTerminalCandidates("", "")
		assert.Equal(t, "wezterm", got[0])
		assert.Contains(t, got, "xterm")
	})
}

// TestStartTerminalRefusesUnderTest guards against the exact incident this
// is meant to prevent: preview honoring $TERMINAL/--terminal and matching
// real binaries on $PATH means an E2E test that reaches spawnLinuxTerminal
// or spawnMacTerminal could otherwise exec a real terminal emulator and pop
// open a window on whatever machine runs `go test` - a real risk on a
// developer's own desktop session (DISPLAY/WAYLAND_DISPLAY set, a terminal
// actually on PATH), not just in headless CI.
//
// startTerminal's default implementation checks testing.Testing() and
// refuses to actually start anything while running inside a test binary -
// this asserts that contract directly, using the real "echo" binary (it
// would definitely start if startTerminal ever fell through to a real
// cmd.Start()) so a regression here fails loudly instead of only showing up
// as a window nobody expected.
func TestStartTerminalRefusesUnderTest(t *testing.T) {
	cmd := exec.Command("echo", "should never actually run")
	err := startTerminal(cmd)
	require.Error(t, err, "startTerminal must refuse to spawn anything while running under go test")
	assert.Contains(t, err.Error(), "test guard")
	assert.Nil(t, cmd.Process, "no process may ever actually be started")
}

func TestArgsForTerminal(t *testing.T) {
	t.Run("known terminal uses its documented convention", func(t *testing.T) {
		assert.Equal(t, []string{"start", "--", "fish", "-l"}, argsForTerminal("wezterm", []string{"fish", "-l"}))
		assert.Equal(t, []string{"fish", "-l"}, argsForTerminal("foot", []string{"fish", "-l"}))
	})

	t.Run("unknown terminal defaults to -e", func(t *testing.T) {
		assert.Equal(t, []string{"-e", "fish", "-l"}, argsForTerminal("some-custom-term", []string{"fish", "-l"}))
	})

	t.Run("matches a known terminal given as a full path", func(t *testing.T) {
		// TERMINAL=/usr/bin/wezterm must still get wezterm's own convention,
		// not the generic "-e" fallback.
		assert.Equal(t, []string{"start", "--", "fish", "-l"}, argsForTerminal("/usr/bin/wezterm", []string{"fish", "-l"}))
	})
}

func TestShellQuoteUnix(t *testing.T) {
	assert.Equal(t, "'/home/user/theme.toml'", shellQuoteUnix("/home/user/theme.toml"))
	assert.Equal(t, `'it'\''s.toml'`, shellQuoteUnix("it's.toml"))
	assert.Equal(t, "'/path with spaces/theme.toml'", shellQuoteUnix("/path with spaces/theme.toml"))
}

func TestPSSingleQuote(t *testing.T) {
	assert.Equal(t, "'C:\\Users\\me\\theme.toml'", psSingleQuote(`C:\Users\me\theme.toml`))
	assert.Equal(t, "'it''s.toml'", psSingleQuote("it's.toml"))
}

func TestUnixManualCommand(t *testing.T) {
	got := unixManualCommand("/home/user/it's.toml", "fish")
	assert.Equal(t, `STARSHIP_CONFIG='/home/user/it'\''s.toml' fish`, got)
}

func TestWindowsManualCommand(t *testing.T) {
	t.Run("prefers pwsh when it's on PATH", func(t *testing.T) {
		got := windowsManualCommand(`C:\Users\me\theme.toml`, fakeLookPath("pwsh", "powershell"))
		assert.Equal(t, `pwsh -NoExit -Command "$env:STARSHIP_CONFIG='C:\Users\me\theme.toml'"`, got)
	})

	t.Run("falls back to powershell when pwsh is missing", func(t *testing.T) {
		got := windowsManualCommand(`C:\Users\me\theme.toml`, fakeLookPath("powershell"))
		assert.Equal(t, `powershell -NoExit -Command "$env:STARSHIP_CONFIG='C:\Users\me\theme.toml'"`, got)
	})

	t.Run("doubles an embedded single quote", func(t *testing.T) {
		got := windowsManualCommand(`C:\it's\theme.toml`, fakeLookPath("powershell"))
		assert.Equal(t, `powershell -NoExit -Command "$env:STARSHIP_CONFIG='C:\it''s\theme.toml'"`, got)
	})
}
