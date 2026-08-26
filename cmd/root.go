package cmd

import (
	stellarinit "github.com/a3chron/stellar/internal/init"
	"github.com/spf13/cobra"
)

var rootCmd *cobra.Command

func init() {
	rootCmd = NewRootCmd()
}

// NewRootCmd creates and returns the root command.
// This is exported for testing purposes.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stellar",
		Short: "Starship theme manager",
		Long:  `Stellar - Discover, preview, and apply Starship themes from the community`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Shell completion runs this on every keystroke, so it must not
			// touch the filesystem - and must not fail: an error here prints
			// no directive line at all, which every shell reads as "offer
			// filenames", silently breaking completion (e.g. on a read-only
			// HOME) rather than degrading to no candidates.
			if isCompletionRequest(cmd) {
				return nil
			}
			// Best-effort cleanup of files left behind by a previous self-update
			cleanupUpdateLeftovers()
			// Initialize stellar directory structure before any command runs
			return stellarinit.EnsureStellarDir()
		},
		// Custom version template (will be set by SetVersionInfo)
		Version: "dev",
	}

	// Add subcommands
	cmd.AddCommand(applyCmd)
	cmd.AddCommand(previewCmd)
	cmd.AddCommand(listCmd)
	cmd.AddCommand(cleanCmd)
	cmd.AddCommand(infoCmd)
	cmd.AddCommand(currentCmd)
	cmd.AddCommand(rollbackCmd)
	cmd.AddCommand(removeCmd)
	cmd.AddCommand(updateCmd)

	return cmd
}

// isCompletionRequest reports whether cmd is (or sits under) one of cobra's
// hidden completion-request commands, i.e. whether this process was spawned by
// the user pressing TAB rather than running stellar themselves.
func isCompletionRequest(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == cobra.ShellCompRequestCmd ||
			c.Name() == cobra.ShellCompNoDescRequestCmd {
			return true
		}
	}
	return false
}

func Execute() error {
	return rootCmd.Execute()
}
