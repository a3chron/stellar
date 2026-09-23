// cmd/confirm.go holds confirmation-prompt plumbing shared across commands:
// TTY detection, the generic yes/no prompt, and the [custom]-command security
// warning that gates apply, preview and rollback alike.
package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/paths"
	"github.com/a3chron/stellar/internal/term"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/fatih/color"
	xterm "golang.org/x/term"
)

// isTerminal reports whether r is an interactive terminal, as opposed to a
// pipe, a redirected file, or closed stdin. A character-device check is not
// enough: /dev/null is one too, and `</dev/null` is exactly how scripts and
// CI detach stdin.
//
// Confirmation prompts use this only to decide how to explain an EOF with no
// input at all (see promptConfirmation) - NOT to refuse reading stdin
// outright. An earlier version of this failed fast whenever stdin wasn't a
// terminal, which broke perfectly good input: `echo y | stellar apply ...`
// pipes a real answer through a non-terminal stdin, and some terminals
// (mintty/Git Bash on Windows) report xterm.IsTerminal false for a stdin
// that's genuinely interactive. Both cases have an actual line to read, so
// both must be allowed to answer.
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	return xterm.IsTerminal(int(f.Fd()))
}

// promptConfirmation asks for user confirmation on os.Stdin, defaulting to
// No.
//
// It always prints the prompt and attempts to read a line, regardless of
// whether stdin looks like a terminal: `echo y | stellar apply ...` and
// mintty/Git Bash both deliver a real answer through a stdin that isTerminal
// reports as non-interactive, so refusing to read it outright (as an earlier
// version of this did) broke exactly the non-interactive usage --force
// exists for. A "y"/"yes" answer is accepted however it arrived.
//
// The only case treated as an error is stdin hitting EOF with nothing at all
// read AND stdin genuinely not being a terminal (e.g. `</dev/null`, or
// closed stdin in CI): there is nobody there to answer, so the caller is
// told to pass --force instead of silently treating that as a decline. Any
// other non-yes answer (an explicit "n", an empty line typed at a real
// terminal, garbage input) is a plain decline, not an error.
func promptConfirmation(prompt string) (bool, error) {
	fmt.Printf("%s [y/N]: ", prompt)

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}

	trimmed := strings.ToLower(strings.TrimSpace(response))
	if trimmed == "y" || trimmed == "yes" {
		return true, nil
	}

	if err == io.EOF && trimmed == "" && !isTerminal(os.Stdin) {
		return false, fmt.Errorf("no answer on stdin; re-run with --force to skip this confirmation")
	}

	return false, nil
}

// confirmCustomCommands warns the user and asks for confirmation when a
// theme's [custom] commands can execute arbitrary shell code, before the
// theme is ever applied, previewed or rolled back to. It is the single
// implementation of that gate, shared by apply, preview and rollback so none
// of them can reach the same dangerous content without showing it - apply had
// this warning, but preview and rollback's re-download path used to skip it
// entirely, even though both also end up running starship against the
// theme's config.
//
// localPath is the file that will actually be used, when there is one on
// disk (preview, rollback to a cached theme); it is shown first, since a local
// theme may not exist on the hub at all. Pass "" for content that was just
// fetched from the hub, where the hub link is the natural place to review it.
//
// verb and verbPast name the action this confirmation guards ("apply"/
// "applied", "preview"/"previewed", "restore"/"restored"), used only for the
// prompt wording. It returns nil immediately when there are no custom
// commands or force is true. Declining, or promptConfirmation failing fast on
// a non-interactive stdin, is returned as an error so the caller aborts with
// a non-zero exit instead of silently continuing (see promptConfirmation).
func confirmCustomCommands(t *theme.Theme, validationResult theme.ValidationResult, force bool, localPath, verb, verbPast string) error {
	if !validationResult.HasCustomCommands || force {
		return nil
	}

	color.Red("\nSECURITY WARNING ")
	color.Yellow("This theme contains [custom] commands that can execute arbitrary shell code.")
	color.Yellow("Custom commands run on your system every time Starship renders your prompt.")
	fmt.Println()
	color.Cyan("Before proceeding, you should review the config at:")
	if localPath != "" {
		fmt.Printf("  %s\n", localPath)
		color.Cyan("or, if it's published, on stellar-hub:")
	}
	// Built from the same base the API client uses (paths.APIURL returns the
	// site root - the "/api" segment is appended per request), so the link
	// cannot drift away from the deployment the theme was actually fetched
	// from, and follows STELLAR_API_URL in tests.
	//
	// ?review= opens the hub straight into the config viewer for this exact
	// version with the [custom] sections highlighted, rather than dropping the
	// user on the theme page to find them.
	reviewURL := fmt.Sprintf(
		"%s/%s/%s?review=%s",
		paths.APIURL(api.BaseURL), t.Author, t.Name, t.Version,
	)
	fmt.Printf(
		"  %s%s\n",
		term.Hyperlink(reviewURL, reviewURL),
		color.HiBlackString(term.ClickHint()),
	)
	fmt.Println()

	ok, err := promptConfirmation(fmt.Sprintf("Do you trust this theme and want to %s it?", verb))
	if err != nil {
		return fmt.Errorf("%s aborted: %w", verb, err)
	}
	if !ok {
		return fmt.Errorf("aborted: theme was not %s", verbPast)
	}
	return nil
}

// validateOnDiskTheme validates the theme config already sitting on disk at
// path - a cached, local/hand-written, or backup theme file, as opposed to
// content that was just downloaded from the hub. Freshly downloaded content
// is always validated in full regardless of force (it never reaches this
// helper); a theme already on disk, however, honors --force: invalid TOML or
// an oversized file is downgraded to a printed warning and the theme is used
// anyway, instead of refusing outright.
//
// t is only used for %s formatting in messages (fmt.Stringer is enough, so
// callers don't need a *theme.Theme in scope, e.g. rollback's redownload
// check).
//
// The returned ValidationResult still reports HasCustomCommands accurately
// when the content could be parsed, so callers can still gate the
// [custom]-command confirmation on it; when force suppressed a validation
// failure, HasCustomCommands reflects whatever the validator managed to
// determine before failing (false if it never got that far, e.g. a TOML
// syntax error).
func validateOnDiskTheme(t fmt.Stringer, path string, force bool) (theme.ValidationResult, error) {
	validationResult, err := theme.ValidateConfig(path)
	if err != nil {
		return theme.ValidationResult{}, fmt.Errorf("failed to read theme for validation: %w", err)
	}
	if !validationResult.Valid {
		if force {
			color.Yellow("Warning: skipping validation for %s (--force): %v", t, validationResult.Error)
			validationResult.Valid = true
			return validationResult, nil
		}
		return validationResult, fmt.Errorf("invalid theme config %s: %w", t, validationResult.Error)
	}
	return validationResult, nil
}
