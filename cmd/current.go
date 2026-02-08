package cmd

import (
	"fmt"
	"os"

	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/symlink"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var currentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the currently applied theme",
	Long:  `Display information about the theme that is currently active.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load config
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if cfg.CurrentTheme == "" {
			color.Yellow("No theme currently applied")
			fmt.Println("\nApply a theme with: stellar apply <author/theme>")
			return nil
		}

		// Verify symlink is still valid
		target, err := symlink.GetCurrentTarget()
		if err != nil {
			color.Red("Symlink broken or missing")
			fmt.Printf("Config says: %s\n", cfg.CurrentTheme)
			fmt.Println("\nRe-apply with: stellar apply " + cfg.CurrentTheme)
			return nil
		}

		// Check if target exists
		if _, err := os.Stat(target); os.IsNotExist(err) {
			color.Red("Theme file missing")
			fmt.Printf("Theme: %s\n", cfg.CurrentTheme)
			fmt.Printf("Expected at: %s\n", cfg.CurrentPath)
			fmt.Println("\nRe-download with: stellar apply " + cfg.CurrentTheme)
			return nil
		}

		// All good - display current theme
		color.Green("Current Theme")
		fmt.Println()
		fmt.Printf("  Theme:  %s\n", cfg.CurrentTheme)
		fmt.Printf("  Path:   %s\n", cfg.CurrentPath)
		fmt.Println()

		// Show symlink info
		starshipConfig, _ := symlink.StarshipConfigPath()
		fmt.Printf("  Starship config: %s\n", starshipConfig)

		return nil
	},
}
