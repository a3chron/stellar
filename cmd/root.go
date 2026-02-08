package cmd

import (
	stellarinit "github.com/a3chron/stellar/internal/init"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "stellar",
	Short: "Starship theme manager",
	Long:  `Stellar - Discover, preview, and apply Starship themes from the community`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Initialize stellar directory structure before any command runs
		return stellarinit.EnsureStellarDir()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(applyCmd)
	rootCmd.AddCommand(previewCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(cleanCmd)
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(currentCmd)
	rootCmd.AddCommand(rollbackCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(updateCmd)
}
