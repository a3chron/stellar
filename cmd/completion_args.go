package cmd

import (
	"os"
	"strings"

	"github.com/a3chron/stellar/internal/completion"
	"github.com/spf13/cobra"
)

// init wires up shell tab-completion for the commands that take theme
// identifiers ("author/slug@version"). It lives in its own file rather than
// touching apply.go/preview.go/info.go/remove.go directly so those files'
// existing structure stays untouched.
func init() {
	applyCmd.ValidArgsFunction = themeIdentifierArgs
	previewCmd.ValidArgsFunction = themeIdentifierArgs
	infoCmd.ValidArgsFunction = themeIdentifierArgs
	removeCmd.ValidArgsFunction = removeIdentifierArgs

	// Commands that take no arguments still need this: with no
	// ValidArgsFunction cobra returns ShellCompDirectiveDefault and the shell
	// falls back to offering filenames, so `stellar list <TAB>` would list the
	// user's working directory.
	for _, c := range []*cobra.Command{
		listCmd, cleanCmd, currentCmd, rollbackCmd, updateCmd, versionCmd,
	} {
		c.ValidArgsFunction = cobra.NoFileCompletions
	}
}

// themeCompletionMode returns the candidate sources for apply/preview/info
// completion. Local-only by default: even a 2s hub round trip makes TAB feel
// broken, and most users complete themes they already have cached. Setting
// STELLAR_COMPLETION_ONLINE=1 opts in to hub suggestions.
//
// Read per invocation (not in init) because every shell completion request
// is its own process and tests toggle the variable at runtime.
func themeCompletionMode() completion.Mode {
	if os.Getenv(completion.EnvOnline) == "1" || os.Getenv(completion.EnvOnline) == "true" {
		return completion.LocalAndRemote
	}
	return completion.LocalOnly
}

// themeIdentifierArgs is the ValidArgsFunction for commands that accept
// exactly one identifier (apply, preview, info): once that argument is
// already typed, there's nothing left to complete.
func themeIdentifierArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completion.ThemeIdentifier(toComplete, themeCompletionMode())
}

// removeIdentifierArgs completes "stellar remove" arguments from the local
// cache only - matching remove's own semantics, it never touches the
// network - and, since remove accepts several identifiers, filters out any
// candidate that's already been typed on the command line so repeated
// completion doesn't keep re-suggesting the same theme.
func removeIdentifierArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	candidates, directive := completion.ThemeIdentifier(toComplete, completion.LocalOnly)
	if len(candidates) == 0 || len(args) == 0 {
		return candidates, directive
	}

	already := make(map[string]bool, len(args))
	for _, a := range args {
		already[a] = true
	}

	filtered := make([]string, 0, len(candidates))
	for _, c := range candidates {
		value := c
		if idx := strings.IndexByte(c, '\t'); idx != -1 {
			value = c[:idx]
		}
		if already[value] {
			continue
		}
		filtered = append(filtered, c)
	}

	return filtered, directive
}
