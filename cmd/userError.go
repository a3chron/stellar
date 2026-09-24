// cmd/userError.go holds stellar's structured CLI error type and the
// central place every command's error is finally printed from
// (printCLIError, called by ExecuteCmd/Execute in cmd/root.go).
//
// Two things used to live only in plain fmt.Errorf/errors.New text: the
// message itself, and any "Did you mean: ..." / "Available versions: ..."
// hints tacked onto it with embedded "\n"s (see cmd/hub_errors.go,
// cmd/suggest.go). That made the hints impossible to colour or indent
// independently from the message - printing an error meant printing one
// opaque string. hintedError keeps the message and its hints as separate
// structured fields so the CLI can render them (bold red "Error:", dim/
// yellow hint labels, bold cyan identifiers/commands, indented two spaces)
// while err.Error() itself still returns exactly the plain, unindented text
// stellar has always produced - so every existing errors.Is/errors.As check
// and every test asserting on err.Error() keeps working unchanged.
package cmd

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// Colours used for the structured parts of an error. fatih/color disables
// all of these automatically for non-TTY output or when NO_COLOR is set, so
// plain-text assertions (including every test in this package, none of
// which run against a real terminal) see exactly the uncoloured text.
var (
	errPrefixColor = color.New(color.FgRed, color.Bold)
	// Labels stay quiet so the eye goes to the error and to what can be
	// acted on: a suggestion is bold cyan, a list of options plain cyan.
	hintLabelColor = color.New(color.FgHiBlack)
	hintValueColor = color.New(color.FgCyan, color.Bold)
	hintItemColor  = color.New(color.FgCyan)
	hintDimColor   = color.New(color.FgHiBlack)
)

// hint is one suggestion/info line printed beneath an "Error: ..." line,
// e.g. "Did you mean: a3chron/ctp-blue" or
// "Available versions: 1.0, 1.1 (latest: 1.1)".
//
// A single value prints on the same line as the label ("Did you mean:
// a3chron/ctp-blue"). Several values print one per line beneath the label
// instead ("Did you mean:" then one indented identifier per line) - used for
// multiple "did you mean" candidates. suffix, when set, is appended after
// the value(s) in a dim colour (the "(latest: 1.1)" part).
type hint struct {
	label  string
	values []string
	// items is an inline, comma-separated list ("1.0, 1.1") - options to
	// choose from rather than one suggestion, so each is highlighted more
	// softly than a value. Used instead of values.
	items  []string
	suffix string
}

// legacyText renders h exactly as stellar's error messages have always
// embedded it inline (no leading indent, no colour) - this is what
// hintedError.Error() uses, so today's plain-text error strings never
// change.
func (h hint) legacyText() string {
	if len(h.items) > 0 {
		line := h.label + ": " + strings.Join(h.items, ", ")
		if h.suffix != "" {
			line += " " + h.suffix
		}
		return line
	}
	switch len(h.values) {
	case 0:
		return h.label + ":"
	case 1:
		line := h.label + ": " + h.values[0]
		if h.suffix != "" {
			line += " " + h.suffix
		}
		return line
	default:
		var b strings.Builder
		b.WriteString(h.label + ":")
		for _, v := range h.values {
			b.WriteString("\n  " + v)
		}
		return b.String()
	}
}

// render renders h the way it's actually shown to the user: indented two
// spaces under the error line, the label dimmed, a list of options in
// plain cyan, a single suggestion in bold cyan, and any suffix dimmed.
func (h hint) render() string {
	label := hintLabelColor.Sprintf("%s:", h.label)
	if len(h.items) > 0 {
		coloured := make([]string, len(h.items))
		for i, item := range h.items {
			coloured[i] = hintItemColor.Sprint(item)
		}
		line := "  " + label + " " + strings.Join(coloured, ", ")
		if h.suffix != "" {
			line += " " + hintDimColor.Sprint(h.suffix)
		}
		return line
	}
	switch len(h.values) {
	case 0:
		return "  " + label
	case 1:
		line := "  " + label + " " + hintValueColor.Sprint(h.values[0])
		if h.suffix != "" {
			line += " " + hintDimColor.Sprint(h.suffix)
		}
		return line
	default:
		var b strings.Builder
		b.WriteString("  " + label)
		for _, v := range h.values {
			b.WriteString("\n    " + hintValueColor.Sprint(v))
		}
		return b.String()
	}
}

// didYouMeanHints builds the (possibly empty) hint slice for a "Did you
// mean: ..." suggestion list, so every caller with zero-or-more candidates
// (theme suggestions, cache-only suggestions, ...) can append the result
// straight into a hintedError's hints without an extra "if empty" check at
// every call site.
func didYouMeanHints(candidates []string) []hint {
	if len(candidates) == 0 {
		return nil
	}
	return []hint{{label: "Did you mean", values: candidates}}
}

// hintedError is a runtime error with zero or more structured hints
// attached. Its Error() returns the plain text stellar has always produced
// (message, then each hint on its own line, exactly as the old inline
// fmt.Errorf/errors.New calls built it) - only the CLI's own top-level
// printing (printCLIError) renders the coloured, indented form.
type hintedError struct {
	msg   string
	hints []hint
}

// newHintedError builds a hintedError. Passing no hints is fine (and used
// by callers that only sometimes have a suggestion) - Error() then returns
// exactly msg, same as a plain errors.New(msg) would.
func newHintedError(msg string, hints ...hint) *hintedError {
	return &hintedError{msg: msg, hints: hints}
}

func (e *hintedError) Error() string {
	if len(e.hints) == 0 {
		return e.msg
	}
	var b strings.Builder
	b.WriteString(e.msg)
	for _, h := range e.hints {
		b.WriteString("\n")
		b.WriteString(h.legacyText())
	}
	return b.String()
}

// printSingleError writes one error's "Error: ..." line (bold red prefix,
// plain-weight message) followed by its hint lines, if any. Write errors to
// the CLI's own error stream are deliberately ignored, same as the rest of
// this package's output calls (color.*, fmt.Print*) - there is nowhere left
// to report a failure to write the error message itself.
func printSingleError(w io.Writer, err error) {
	prefix := errPrefixColor.Sprint("Error:")

	var he *hintedError
	if errors.As(err, &he) {
		_, _ = fmt.Fprintln(w, prefix+" "+he.msg)
		for _, h := range he.hints {
			_, _ = fmt.Fprintln(w, h.render())
		}
		return
	}

	_, _ = fmt.Fprintln(w, prefix+" "+err.Error())
}

// printCLIError is stellar's single place for printing a command's returned
// error to the user - called from ExecuteCmd (cmd/root.go), which every real
// invocation and every test that wants to see the actual rendered output
// goes through instead of relying on cobra's own (now-silenced, via
// SilenceErrors on root) error printing.
//
// `remove <id1> <id2> ...` aggregates one failure per identifier with
// errors.Join, whose result implements Unwrap() []error - printCLIError
// prints each of those as its own "Error: ..." line (plus its own hints)
// rather than dumping the whole join as one opaque, uncoloured block, so a
// partial failure across several identifiers still reads as N clean
// one-line failures instead of one run-on error.
//
// executedCmd is whichever (sub)command cobra actually resolved and ran (the
// *cobra.Command ExecuteC returns) - its usage block, not root's, is what
// gets shown for a usageError (see cmd/args.go and root's SetFlagErrorFunc).
func printCLIError(executedCmd *cobra.Command, err error) {
	w := executedCmd.ErrOrStderr()

	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, sub := range joined.Unwrap() {
			printSingleError(w, sub)
		}
	} else {
		printSingleError(w, err)
	}

	var ue *usageError
	if errors.As(err, &ue) {
		_, _ = fmt.Fprintln(w, executedCmd.UsageString())
	}
}
