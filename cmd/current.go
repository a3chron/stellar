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

		starshipConfig, err := symlink.StarshipConfigPath()
		if err != nil {
			return fmt.Errorf("failed to resolve starship config path: %w", err)
		}

		// Branch on what's actually on disk (via a single Lstat), not on
		// STELLAR_APPLY_MODE/GOOS: a copy-mode config inspected with
		// STELLAR_APPLY_MODE=symlink (or vice versa) must still get the right
		// diagnostics, since the env var only affects how a *future* apply
		// behaves, not what's currently on disk.
		info, statErr := os.Lstat(starshipConfig)
		switch {
		case os.IsNotExist(statErr):
			color.Red("Starship config missing")
			fmt.Printf("Config says: %s\n", cfg.CurrentTheme)
			fmt.Println("\nRe-apply with: stellar apply " + cfg.CurrentTheme)
			return nil

		case statErr == nil && info.Mode()&os.ModeSymlink != 0:
			// Symlink mode. Verify the link points at an existing theme file.
			target, terr := symlink.GetCurrentTarget()
			if terr != nil {
				color.Red("Symlink broken or missing")
				fmt.Printf("Config says: %s\n", cfg.CurrentTheme)
				fmt.Println("\nRe-apply with: stellar apply " + cfg.CurrentTheme)
				return nil
			}

			if _, err := os.Stat(target); os.IsNotExist(err) {
				color.Red("Theme file missing")
				fmt.Printf("Theme: %s\n", cfg.CurrentTheme)
				fmt.Printf("Expected at: %s\n", cfg.CurrentPath)
				fmt.Println("\nRe-download with: stellar apply " + cfg.CurrentTheme)
				return nil
			}

		case statErr == nil && info.IsDir():
			// Something (not stellar) replaced starship.toml with a
			// directory. Neither symlink nor copy mode ever produces this,
			// so there's nothing further to check.
			color.Red("Starship config path is a directory, not a file")
			fmt.Printf("Config says: %s\n", cfg.CurrentTheme)
			fmt.Println("\nRe-apply with: stellar apply " + cfg.CurrentTheme)
			return nil

		case statErr == nil:
			// Regular file (copy mode, or a symlink-mode config someone
			// hand-edited into a plain file). There's no link to read, but
			// unlike a plain existence check, AppliedHash (recorded by
			// ApplyTheme/rollback whenever they write starship.toml) lets us
			// verify the file's *content* still matches what was applied,
			// not just that some file is present at cfg.CurrentPath.
			if cfg.CurrentPath == "" {
				color.Red("Current theme path is unknown")
				fmt.Printf("Config says: %s\n", cfg.CurrentTheme)
				fmt.Println("\nRe-apply with: stellar apply " + cfg.CurrentTheme)
				return nil
			}

			if _, err := os.Stat(cfg.CurrentPath); os.IsNotExist(err) {
				color.Red("Theme file missing")
				fmt.Printf("Theme: %s\n", cfg.CurrentTheme)
				fmt.Printf("Cached theme file missing, expected at: %s\n", cfg.CurrentPath)
				fmt.Println("\nRe-download with: stellar apply " + cfg.CurrentTheme)
				return nil
			}

			// Content check: does starship.toml still hold what was actually
			// applied? Prefer AppliedHash, which is mode/OS independent and
			// recorded at apply time; fall back to comparing directly
			// against the cached theme file for legacy configs saved before
			// AppliedHash existed. A hashing failure is treated as "can't
			// tell" rather than "modified", so it fails open instead of
			// reporting a false positive.
			modified := false
			if cfg.AppliedHash != "" {
				if actualHash, herr := symlink.HashFile(starshipConfig); herr == nil {
					modified = actualHash != cfg.AppliedHash
				}
			} else if currentHash, cerr := symlink.HashFile(starshipConfig); cerr == nil {
				if cachedHash, cerr2 := symlink.HashFile(cfg.CurrentPath); cerr2 == nil {
					modified = currentHash != cachedHash
				}
			}

			if modified {
				color.Red("Starship config was modified or replaced")
				fmt.Printf("Theme: %s\n", cfg.CurrentTheme)
				fmt.Println("starship.toml no longer matches the theme that was applied.")
				fmt.Println("The next `stellar apply` will automatically back up your current starship.toml before applying.")
				fmt.Println("\nRe-apply with: stellar apply " + cfg.CurrentTheme)
				return nil
			}

		default:
			// Some other Lstat error (e.g. permission denied). Surface it
			// rather than silently reporting a healthy state.
			return fmt.Errorf("failed to inspect starship config: %w", statErr)
		}

		// All good - display current theme
		color.Green("Current Theme")
		fmt.Println()
		fmt.Printf("  Theme:  %s\n", cfg.CurrentTheme)
		fmt.Printf("  Path:   %s\n", cfg.CurrentPath)
		fmt.Println()

		// Show starship config path
		fmt.Printf("  Starship config: %s\n", starshipConfig)

		return nil
	},
}
