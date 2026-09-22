// Package term holds small helpers for talking to the terminal.
package term

import (
	"fmt"

	"github.com/fatih/color"
)

// Hyperlink renders text as a clickable link using the OSC 8 escape sequence,
// which terminals such as ghostty, kitty, WezTerm, iTerm2 and recent VTE
// builds turn into something you can click or ctrl+click.
//
// The sequence is ESC ] 8 ; ; URI ST, then the visible text, then an empty
// OSC 8 to close the link. ST is written as ESC \ (the spec's string
// terminator) rather than BEL, which some terminals render as a stray glyph.
//
// Terminals that do not understand OSC 8 swallow the sequence and show only
// the text - which is why callers should pass the URL itself as the text
// wherever the URL is the point. Anywhere output is not a terminal at all
// (a pipe, a file, CI) or the user asked for plain output via NO_COLOR or
// TERM=dumb, the escapes are skipped entirely: a URL wrapped in invisible
// control characters is worse than a plain one when it is about to be grepped.
//
// The text is also underlined. Several terminals (ghostty among them) already
// linkify bare URLs on their own, so an OSC 8 link with no styling looks
// exactly like ordinary text until you happen to hover it - the underline is
// what tells the reader there is something to click at all.
func Hyperlink(url, text string) string {
	if color.NoColor {
		return text
	}
	return fmt.Sprintf(
		"\x1b]8;;%s\x1b\\%s%s%s\x1b]8;;\x1b\\",
		url, underlineOn, text, underlineOff,
	)
}

const (
	underlineOn  = "\x1b[4m"
	underlineOff = "\x1b[24m"
)

// ClickHint returns a short note telling the reader the link is clickable, or
// an empty string when no link was rendered (piped output, NO_COLOR,
// TERM=dumb) - where the advice would be meaningless.
//
// It deliberately does not name one keybinding. There is no universal one:
// VTE-based terminals, Konsole, Windows Terminal and VS Code use ctrl+click,
// macOS Terminal and iTerm2 use cmd+click, kitty defaults to ctrl+shift+click,
// and some terminals (ghostty included) open a hyperlink on a plain click.
// Printing "ctrl+click" would simply be wrong for every macOS user, so the
// hint names the two common modifiers and leaves the rest to the reader.
func ClickHint() string {
	if color.NoColor {
		return ""
	}
	return " (ctrl/cmd-click to open)"
}
