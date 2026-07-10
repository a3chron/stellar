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
			// Best-effort cleanup of a binary left behind by a previous Windows self-update
			cleanupOldExecutable()
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

func Execute() error {
	return rootCmd.Execute()
}
