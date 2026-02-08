package cmd

import (
	"fmt"

	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/config"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all cached themes",
	Long:  `Display all themes that have been downloaded and cached locally.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get current theme
		cfg, err := config.Load()
		if err != nil {
			cfg = &config.Config{} // Empty config if doesn't exist
		}

		// List all cached themes
		themes, err := cache.ListCachedThemes()
		if err != nil {
			return fmt.Errorf("failed to list themes: %w", err)
		}

		if len(themes) == 0 {
			color.Yellow("No themes cached yet")
			fmt.Println("\nDownload a theme with: stellar apply <author/theme>")
			return nil
		}

		color.Cyan("Cached Themes (%d):\n", len(themes))

		for _, theme := range themes {
			// Check if this is the current theme
			isCurrent := theme == cfg.CurrentTheme

			if isCurrent {
				color.Green("  ✳ %s (current)", theme)
			} else {
				fmt.Printf("    %s\n", theme)
			}
		}

		return nil
	},
}
