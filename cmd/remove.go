package cmd

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	forceRemove bool
)

var removeCmd = &cobra.Command{
	Use:   "remove [author/theme[@version]]...",
	Short: "Remove one or more cached themes",
	Long: `Delete themes from local cache.

Without a version: removes all versions of the theme
With a version: removes only that specific version

Use --force to remove the currently active theme.`,
	Example: `  stellar remove a3chron/ctp-green
  stellar remove a3chron/ctp-green@1.0
  stellar remove a3chron/ctp-green a3chron/ctp-red`,
	Args: argsWithUsage(cobra.MinimumNArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load config once for all removals
		cfg, err := config.Load()
		if err != nil {
			cfg = &config.Config{}
		}

		var errs []error
		for _, identifier := range args {
			t, err := theme.ParseIdentifier(identifier)
			if err != nil {
				errs = append(errs, err)
				continue
			}

			// "@latest" is the newest cached version, not a version named
			// "latest" - nothing is ever stored under that name. Resolving it
			// here keeps remove agreeing with apply: without this, "remove
			// x/y@latest" looks for a latest.toml that apply no longer writes
			// and reports "theme not found in cache" while the theme is sitting
			// there, applied.
			//
			// No version at all still means every version, as documented.
			if t.VersionExplicit && t.Version == theme.LatestVersion {
				themeDir, dirErr := t.CacheDir()
				if dirErr != nil {
					err = dirErr
				} else if localVer, verErr := theme.FindLatestLocalVersion(themeDir); verErr != nil {
					err = fmt.Errorf("theme not found in cache: %s/%s", t.Author, t.Name)
				} else {
					t.Version = localVer
					err = removeSpecificVersion(t, cfg)
				}
			} else if !t.VersionExplicit {
				err = removeAllVersions(t, cfg)
			} else {
				err = removeSpecificVersion(t, cfg)
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", identifier, err))
			}
		}

		// A refusal (active theme, not cached) or a parse error must exit
		// non-zero like any other failure - errors.Join keeps every reason
		// in the returned error's text (cobra prints it once via
		// "Error: ..."), instead of collapsing them into just a count.
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		return nil
	},
}

func removeAllVersions(t *theme.Theme, cfg *config.Config) error {
	themeDir, err := t.CacheDir()
	if err != nil {
		return err
	}

	// Check if theme directory exists
	if _, err := os.Stat(themeDir); os.IsNotExist(err) {
		return fmt.Errorf("theme not found in cache: %s/%s", t.Author, t.Name)
	}

	// Check if current theme is in this directory
	currentThemeInDir := false
	if cfg.CurrentPath != "" {
		currentDir := filepath.Dir(cfg.CurrentPath)
		if currentDir == themeDir {
			currentThemeInDir = true
		}
	}

	if currentThemeInDir && !forceRemove {
		return fmt.Errorf(
			"cannot remove theme containing currently active version: %s/%s "+
				"(apply a different theme first, or use --force)",
			t.Author, t.Name,
		)
	}

	// Remove the entire theme directory
	if err := os.RemoveAll(themeDir); err != nil {
		return fmt.Errorf("failed to remove theme directory: %w", err)
	}

	// Try to remove author directory if empty
	authorDir := filepath.Dir(themeDir)
	if isEmpty, _ := isDirEmpty(authorDir); isEmpty {
		if err := os.Remove(authorDir); err != nil {
			log.Printf("warning: failed to remove directory %s: %v", authorDir, err)
		}
	}

	color.Green("Removed all versions: %s/%s", t.Author, t.Name)

	// If current theme was in this directory, update config
	if currentThemeInDir {
		cfg.CurrentTheme = ""
		cfg.CurrentPath = ""
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
		color.Yellow("\nYou removed the active theme. Apply a new one with: stellar apply <author>/<theme>")
	}

	return nil
}

func removeSpecificVersion(t *theme.Theme, cfg *config.Config) error {
	// Get theme path
	themePath, err := t.CachePath()
	if err != nil {
		return err
	}

	// Check if trying to remove current theme
	themeID := t.String()
	if themeID == cfg.CurrentTheme && !forceRemove {
		return fmt.Errorf(
			"cannot remove currently active theme: %s (apply a different theme first, or use --force)",
			themeID,
		)
	}

	// Check if theme exists
	if _, err := os.Stat(themePath); os.IsNotExist(err) {
		return fmt.Errorf("theme not found in cache: %s", themeID)
	}

	// Remove theme file
	if err := os.Remove(themePath); err != nil {
		return fmt.Errorf("failed to remove theme: %w", err)
	}

	// Clean up empty directories
	themeDir := filepath.Dir(themePath)
	if isEmpty, _ := isDirEmpty(themeDir); isEmpty {
		if err := os.Remove(themeDir); err != nil {
			log.Printf("warning: failed to remove directory %s: %v", themeDir, err)
		}

		// Also try to remove author directory if empty
		authorDir := filepath.Dir(themeDir)
		if isEmpty, _ := isDirEmpty(authorDir); isEmpty {
			if err := os.Remove(authorDir); err != nil {
				log.Printf("warning: failed to remove directory %s: %v", authorDir, err)
			}
		}
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
}

func isDirEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

func init() {
	removeCmd.Flags().BoolVarP(&forceRemove, "force", "f", false, "Force removal even if theme is currently active")
}
