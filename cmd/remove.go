package cmd

import (
	"fmt"
	"os"

	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	forceRemove bool
)

var removeCmd = &cobra.Command{
	Use:   "remove [author/theme[@version]]",
	Short: "Remove a cached theme",
	Long:  `Delete a theme from local cache. Use --force to remove the currently active theme.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		identifier := args[0]

		// Parse identifier
		t, err := theme.ParseIdentifier(identifier)
		if err != nil {
			return err
		}

		// Load config to check if it's current
		cfg, err := config.Load()
		if err != nil {
			cfg = &config.Config{}
		}

		// Check if trying to remove current theme
		themeID := t.String()
		if themeID == cfg.CurrentTheme && !forceRemove {
			color.Yellow("Cannot remove currently active theme: %s", themeID)
			fmt.Println("\nOptions:")
			fmt.Println("  1. Apply a different theme first")
			fmt.Println("  2. Use --force to remove anyway (why would one do that? for the sake of the force?)")
			return nil
		}

		// Get theme path
		themePath, err := t.CachePath()
		if err != nil {
			return err
		}

		// Check if theme exists
		if _, err := os.Stat(themePath); os.IsNotExist(err) {
			color.Yellow("Theme not found in cache: %s", themeID)
			return nil
		}

		// Remove theme file
		if err := os.Remove(themePath); err != nil {
			return fmt.Errorf("failed to remove theme: %w", err)
		}

		color.Green("Removed: %s", themeID)

		// If it was the current theme, update config
		if themeID == cfg.CurrentTheme {
			cfg.CurrentTheme = ""
			cfg.CurrentPath = ""
			if err := cfg.Save(); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
			color.Yellow("\nYou removed the active theme. Apply a new one with: stellar apply <author>/<theme>")
		}

		return nil
	},
}

func init() {
	removeCmd.Flags().BoolVarP(&forceRemove, "force", "f", false, "Force removal even if theme is currently active")
}
