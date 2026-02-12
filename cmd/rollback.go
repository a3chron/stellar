package cmd

import (
	"fmt"
	"os"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/symlink"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Restore the previous theme",
	Long:  `Switch back to the theme that was active before the current one. Return to the current one by running rollback again`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load config
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if cfg.PreviousTheme == "" {
			color.Yellow("No previous theme to rollback to")
			return nil
		}

		if cfg.PreviousPath == "" {
			return fmt.Errorf("previous theme path not found in config")
		}

		// Check if previous theme file exists, re-download if missing
		if _, err := os.Stat(cfg.PreviousPath); os.IsNotExist(err) {
			color.Yellow("Previous theme not in cache, downloading...")

			// Parse the theme identifier
			t, err := theme.ParseIdentifier(cfg.PreviousTheme)
			if err != nil {
				return fmt.Errorf("failed to parse previous theme: %w", err)
			}

			// Download the theme
			client := api.NewClient()
			content, err := client.FetchThemeConfig(t.Author, t.Name, t.Version)
			if err != nil {
				return fmt.Errorf("failed to download previous theme: %w", err)
			}

			// Validate and save
			if err := theme.ValidateConfigContent(content); err != nil {
				return fmt.Errorf("invalid config: %w", err)
			}

			if err := cache.SaveTheme(t, content); err != nil {
				return fmt.Errorf("failed to save theme: %w", err)
			}
		}

		// Swap current and previous
		previousTheme := cfg.PreviousTheme
		previousPath := cfg.PreviousPath

		cfg.PreviousTheme = cfg.CurrentTheme
		cfg.PreviousPath = cfg.CurrentPath
		cfg.CurrentTheme = previousTheme
		cfg.CurrentPath = previousPath

		// Save config
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		// Update symlink
		if err := symlink.CreateSymlink(cfg.CurrentPath); err != nil {
			return fmt.Errorf("failed to update symlink: %w", err)
		}

		color.Green("Rolled back to: %s", cfg.CurrentTheme)

		return nil
	},
}
