package cmd

import (
	"fmt"
	"log"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/symlink"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var applyCmd = &cobra.Command{
	Use:   "apply [author/theme[@version]]",
	Short: "Apply a Starship theme",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		identifier := args[0]

		// 1. Parse identifier
		t, err := theme.ParseIdentifier(identifier)
		if err != nil {
			return err
		}

		// 2. Load config early to check download history
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		// Theme identifier without version for tracking (author/name)
		themeID := fmt.Sprintf("%s/%s", t.Author, t.Name)

		// 3. Check if cached, download if not
		if !cache.ThemeExists(t) {
			color.Yellow("Downloading %s...", t)

			client := api.NewClient()
			content, err := client.FetchThemeConfig(t.Author, t.Name, t.Version)
			if err != nil {
				return fmt.Errorf("failed to download: %w", err)
			}

			// Validate before saving
			if err := theme.ValidateConfigContent(content); err != nil {
				return fmt.Errorf("invalid config: %w", err)
			}

			if err := cache.SaveTheme(t, content); err != nil {
				return err
			}

			// Only increment download count if:
			// 1. Not running dev build
			// 2. Theme hasn't been downloaded before
			shouldCount := !IsDev() && !cfg.HasDownloaded(themeID)
			if shouldCount {
				if err := client.IncrementDownloadCount(t.Author, t.Name); err != nil {
					log.Printf("download count failed: %v", err)
				}
			}

			// Mark theme as downloaded
			cfg.MarkDownloaded(themeID)
		}

		// 4. Get cached path
		themePath, err := t.CachePath()
		if err != nil {
			return err
		}

		// 5. Update config (save previous for rollback)
		cfg.PreviousTheme = cfg.CurrentTheme
		cfg.PreviousPath = cfg.CurrentPath
		cfg.CurrentTheme = t.String()
		cfg.CurrentPath = themePath

		if err := cfg.Save(); err != nil {
			return err
		}

		// 6. Create symlink
		if err := symlink.CreateSymlink(themePath); err != nil {
			return err
		}

		color.Green("Applied %s", t)
		return nil
	},
}
