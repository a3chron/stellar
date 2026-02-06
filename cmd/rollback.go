package cmd

import (
	"fmt"

	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/symlink"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Restore the previous theme",
	Long:  `Switch back to the theme that was active before the current one.`,
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
		fmt.Println("\nRestart your shell or run: exec $SHELL")

		return nil
	},
}
