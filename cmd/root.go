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
			created, err := stellarinit.EnsureStellarDir()
			if err != nil {
				return err
			}
			// Kick off the anonymous install report (see cmd/telemetry.go). It
			// runs in the background and is joined in PersistentPostRunE, so
			// the user's command never waits on the hub.
			startTelemetry(created)
			return nil
		},
		// Cobra runs only the nearest PersistentPostRun*, and no subcommand
		// defines one, so this fires after every command that ran to
		// completion. It does NOT run when RunE returned an error - the report
		// is then simply retried on the next run, since nothing was recorded.
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			finishTelemetry()
			return nil
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
	cmd.AddCommand(uninstallCmd)

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
