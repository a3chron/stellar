// cmd/prompt.go holds the shared interactive-prompt reader used by the
// publish-flow commands (auth, new, fork, publish).
//
// promptConfirmation and confirmCustomCommands (cmd/confirm.go) each open
// their own bufio.Reader directly on os.Stdin - fine for a command that
// shows exactly one yes/no gate, but wrong for a command that may ask
// several questions in a row: two independent bufio.Readers layered over the
// same stdin pipe each buffer ahead past what they return, so the second
// reader silently "steals" input the first one already buffered but never
// consumed. Every command in this file's family builds exactly one
// *bufio.Reader (via newPromptReader) up front and threads it through every
// prompt it shows, and must never also call promptConfirmation/
// confirmCustomCommands.
package cmd

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// newPromptReader returns the single buffered reader a command should thread
// through every prompt it shows, built from cmd.InOrStdin() (not os.Stdin
// directly) so tests can supply input via cmd.SetIn.
func newPromptReader(cmd *cobra.Command) *bufio.Reader {
	return bufio.NewReader(cmd.InOrStdin())
}

// readLine reads one line from r, stripping the trailing newline/carriage
// return. ok is false only when stdin hit EOF with absolutely nothing read -
// the "nobody is there to answer" case every prompt helper below turns into
// a "no answer on stdin" error rather than silently treating as a blank
// answer.
func readLine(r *bufio.Reader) (line string, ok bool, err error) {
	raw, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", false, err
	}
	trimmed := strings.TrimRight(raw, "\r\n")
	if err == io.EOF && trimmed == "" {
		return "", false, nil
	}
	return trimmed, true, nil
}

// noAnswerError is the error every prompt helper returns when it can't get
// an answer at all: stdin hit EOF with nothing read, or stdin isn't a
// terminal and there is no default to fall back to. flagHint names the exact
// flag (with its value placeholder, e.g. "--name <name>") the caller should
// pass instead.
func noAnswerError(flagHint string) error {
	return fmt.Errorf("no answer on stdin; re-run with %s", flagHint)
}

// promptText asks prompt (shown with "[def]" when def is non-empty) and
// returns the typed line, or def if the user presses Enter with no input. An
// empty line with no default, or stdin hitting EOF with nothing typed at
// all, is reported as noAnswerError(flagHint).
func promptText(r *bufio.Reader, prompt, def, flagHint string) (string, error) {
	if def != "" {
		fmt.Printf("%s [%s]: ", prompt, def)
	} else {
		fmt.Printf("%s: ", prompt)
	}

	line, ok, err := readLine(r)
	if err != nil {
		return "", err
	}
	if !ok || strings.TrimSpace(line) == "" {
		if def != "" {
			return def, nil
		}
		return "", noAnswerError(flagHint)
	}
	return line, nil
}

// promptSelect numbers options 1..N, asks prompt, and returns the chosen
// (0-based) index. defaultIndex is shown as the bracketed default and is
// returned when the user presses Enter with no input; pass -1 when there is
// no safe default, which turns a blank/missing answer into
// noAnswerError(flagHint) instead.
func promptSelect(r *bufio.Reader, prompt string, options []string, defaultIndex int, flagHint string) (int, error) {
	for i, opt := range options {
		fmt.Printf("  %d) %s\n", i+1, opt)
	}

	defLabel := ""
	hasDefault := defaultIndex >= 0 && defaultIndex < len(options)
	if hasDefault {
		defLabel = fmt.Sprintf(" [%d]", defaultIndex+1)
	}
	fmt.Printf("%s%s: ", prompt, defLabel)

	line, ok, err := readLine(r)
	if err != nil {
		return -1, err
	}
	trimmed := strings.TrimSpace(line)
	if !ok || trimmed == "" {
		if hasDefault {
			return defaultIndex, nil
		}
		return -1, noAnswerError(flagHint)
	}

	n, convErr := strconv.Atoi(trimmed)
	if convErr != nil || n < 1 || n > len(options) {
		return -1, fmt.Errorf("invalid selection %q (expected a number from 1 to %d)", line, len(options))
	}
	return n - 1, nil
}

// promptYesNo asks a yes/no question, showing def as the bracketed default
// ("[Y/n]" or "[y/N]") exactly like a real terminal prompt would - which
// means a blank line (the user just pressing Enter) always returns def,
// never an error, as long as someone could plausibly have answered at all.
//
// The only case treated as "no answer" is stdin hitting EOF with absolutely
// nothing read AND tty being false (a genuinely non-interactive stdin -
// `</dev/null`, a detached CI run, a script that forgot to pipe an answer
// in): there is nobody there to even see the bracketed default, so the
// caller is told to pass flagHint instead of silently assuming it. tty
// should be the same isTerminal(cmd.InOrStdin()) value the caller already
// computed once per command - see cmd/publish.go's "interactive" convention.
//
// Any other non-"y"/"yes" answer is a plain "no", matching
// promptConfirmation's convention elsewhere in this package.
func promptYesNo(r *bufio.Reader, tty bool, prompt string, def bool, flagHint string) (bool, error) {
	defLabel := "[y/N]"
	if def {
		defLabel = "[Y/n]"
	}
	fmt.Printf("%s %s: ", prompt, defLabel)

	line, ok, err := readLine(r)
	if err != nil {
		return false, err
	}
	trimmed := strings.ToLower(strings.TrimSpace(line))
	if trimmed == "" {
		if !ok && !tty {
			return false, noAnswerError(flagHint)
		}
		return def, nil
	}

	return trimmed == "y" || trimmed == "yes", nil
}
