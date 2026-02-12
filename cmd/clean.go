package cmd

import (
	"fmt"

	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/config"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	cleanAll bool
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove cached themes",
	Long:  `Remove all cached themes except the currently applied one. Use --all to remove everything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get current theme to preserve it
		cfg, err := config.Load()
		if err != nil {
			cfg = &config.Config{}
		}

		excludeCurrentPath := ""
		if !cleanAll {
			excludeCurrentPath = cfg.CurrentPath
		}

		// Count themes before cleaning
		themesBefore, _ := cache.ListCachedThemes()
		beforeCount := len(themesBefore)

		// Clean cache (always remove empty directories since we track downloads in config)
		err = cache.CleanCache(excludeCurrentPath)
		if err != nil {
			return fmt.Errorf("failed to clean cache: %w", err)
		}

		// Count after
		themesAfter, _ := cache.ListCachedThemes()
		afterCount := len(themesAfter)
		removed := beforeCount - afterCount

		if removed == 0 {
			color.Yellow("Cache already clean")
			return nil
		}

		color.Green("Cleaned cache: removed %d theme(s)", removed)

		if !cleanAll && cfg.CurrentTheme != "" {
			fmt.Printf("   Kept current theme: %s\n", cfg.CurrentTheme)
		}

		return nil
	},
}

func init() {
	cleanCmd.Flags().BoolVar(&cleanAll, "all", false, "Remove all cached themes including the current one")
}
