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

var rollbackForce bool

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Restore the previous theme",
	Long:  `Switch back to the theme that was active before the current one. Return to the current one by running rollback again`,
	Example: `  stellar rollback
  stellar rollback --force`,
	Args: argsWithUsage(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load config
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if cfg.PreviousTheme == "" {
			return fmt.Errorf("no previous theme to roll back to")
		}

		if cfg.PreviousPath == "" {
			return fmt.Errorf("previous theme path not found in config")
		}

		// A re-apply of the current theme leaves Previous untouched (see
		// apply.go), so this can only mean rollback already ran, or Previous
		// was fabricated equal to Current by hand. Either way there's nothing
		// to swap to. This also happens after re-applying the same theme with
		// an older stellar version that didn't have that fix - applying a
		// different theme (even temporarily) fixes it going forward.
		if cfg.PreviousTheme == cfg.CurrentTheme {
			return fmt.Errorf(
				"nothing to roll back to: previous and current theme are both %s "+
					"(this can happen after re-applying the same theme with an older "+
					"stellar version; applying a different theme fixes it)",
				cfg.CurrentTheme,
			)
		}

		redownloaded := false

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
			validationResult, err := theme.ValidateConfigContent(content)
			if err != nil {
				return fmt.Errorf("validation error: %w", err)
			}
			if !validationResult.Valid {
				return fmt.Errorf("invalid config: %w", validationResult.Error)
			}

			// A re-download is fresh content from the hub the user has never
			// seen locally before, so the security warning always applies
			// here, same as a first-time apply.
			if err := confirmCustomCommands(t, validationResult, rollbackForce, "", "restore", "restored"); err != nil {
				return err
			}

			if err := cache.SaveTheme(t, content); err != nil {
				return fmt.Errorf("failed to save theme: %w", err)
			}
			redownloaded = true
		}

		// Even when the file was already cached, validate it - invalid TOML
		// must never be silently re-applied, unless --force explicitly
		// accepts that (see validateOnDiskTheme). Unlike the redownload
		// branch above, a cached previous theme is never prompted for
		// [custom] commands here: it's content the user already had
		// locally (they saw the prompt, if any, when it was first applied
		// or downloaded), matching how apply treats an already-cached
		// theme. Only genuinely fresh content from the hub gets that
		// prompt.
		if !redownloaded {
			t, perr := theme.ParseIdentifier(cfg.PreviousTheme)
			if perr != nil {
				return fmt.Errorf("failed to parse previous theme: %w", perr)
			}

			if _, verr := validateOnDiskTheme(t, cfg.PreviousPath, rollbackForce); verr != nil {
				return verr
			}
		}

		// Capture values for swap
		previousTheme := cfg.PreviousTheme
		previousPath := cfg.PreviousPath

		// Apply the previous theme FIRST (before modifying config)
		// This ensures that if applying fails, config remains unchanged
		backupInfo, err := symlink.ApplyTheme(previousPath, cfg)
		if err != nil {
			return fmt.Errorf("failed to apply previous theme: %w", err)
		}

		// Only swap config after applying succeeds
		// (ApplyTheme has already recorded cfg.AppliedHash for the applied file)
		cfg.PreviousTheme = cfg.CurrentTheme
		cfg.PreviousPath = cfg.CurrentPath
		cfg.CurrentTheme = previousTheme
		cfg.CurrentPath = previousPath

		// Save config
		if err := cfg.Save(); err != nil {
			// Symlink succeeded but config save failed
			// This is less severe - rollback applied, but state tracking may be lost
			return fmt.Errorf("rollback applied but failed to save config: %w", err)
		}

		// Notify user if their original config was backed up (previously silent)
		if backupInfo != nil {
			printBackupNotice(backupInfo)
		}

		color.Green("Rolled back to: %s", cfg.CurrentTheme)

		warnPostApplyEnvironment()

		return nil
	},
}

func init() {
	rollbackCmd.Flags().BoolVarP(&rollbackForce, "force", "f", false,
		"Skip the custom-command confirmation on a re-download, and skip TOML validation (with a warning) for an already-cached previous theme")
}
