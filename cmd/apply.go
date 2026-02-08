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

		// 2. Check if cached, download if not
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

			// Increment download count (fire and forget)
			go func() {
				if err := client.IncrementDownloadCount(t.Author, t.Name); err != nil {
					log.Printf("failed to increment download count: %v", err)
				}
			}()
		}

		// 3. Get cached path
		themePath, err := t.CachePath()
		if err != nil {
			return err
		}

		// 4. Update config (save previous for rollback)
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		cfg.PreviousTheme = cfg.CurrentTheme
		cfg.PreviousPath = cfg.CurrentPath
		cfg.CurrentTheme = t.String()
		cfg.CurrentPath = themePath

		if err := cfg.Save(); err != nil {
			return err
		}

		// 5. Create symlink
		if err := symlink.CreateSymlink(themePath); err != nil {
			return err
		}

		color.Green("Applied %s", t)
		return nil
	},
}
