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
// completion.
//
// The default is completion.LocalThenRemote: the hub is queried only when the
// local cache produced nothing for what the user typed. That keeps the common
// case - completing a theme you already have - entirely offline and instant,
// while the case that previously completed to nothing at all (a theme you've
// never downloaded) now reaches the hub, under a sub-second budget and
// degrading silently to no candidates.
//
// STELLAR_COMPLETION_ONLINE overrides it in both directions: "1"/"true"
// queries the hub on every completion (hub themes are merged in even when the
// cache already matched), "0"/"false" never touches the network at all.
//
// Read per invocation (not in init) because every shell completion request
// is its own process and tests toggle the variable at runtime.
func themeCompletionMode() completion.Mode {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(completion.EnvOnline)))

	switch raw {
	case "":
		// Unset: the default. Local first, hub only when local finds nothing.
		return completion.LocalThenRemote
	case "1", "true", "yes", "on":
		return completion.LocalAndRemote
	default:
		// Set to anything else - "0", "false", "off", "no", or a typo - means
		// local only. Failing closed matters here: someone who writes
		// STELLAR_COMPLETION_ONLINE=off is asking for no network, and falling
		// through to the default would silently do the opposite of what they
		// asked.
		return completion.LocalOnly
	}
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
