package cmd

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/user"
	"strings"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/symlink"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var forceApply bool

func getCurrentUsername() string {
	currentUser, err := user.Current()
	if err != nil {
		return "local"
	}
	return currentUser.Username
}

// promptConfirmation asks for user confirmation, defaults to No
func promptConfirmation(prompt string) bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s [y/N]: ", prompt)

	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

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
			validationResult, err := theme.ValidateConfigContent(content)
			if err != nil {
				return fmt.Errorf("validation error: %w", err)
			}
			if !validationResult.Valid {
				return fmt.Errorf("invalid config: %w", validationResult.Error)
			}

			// Check for custom commands and warn user
			if validationResult.HasCustomCommands && !forceApply {
				color.Red("\nSECURITY WARNING ")
				color.Yellow("This theme contains [custom] commands that can execute arbitrary shell code.")
				color.Yellow("Custom commands run on your system every time Starship renders your prompt.")
				fmt.Println()
				color.Cyan("Before proceeding, you should review the config at:")
				fmt.Printf("  https://stellar-hub.vercel.app/%s/%s\n", t.Author, t.Name)
				fmt.Println()

				if !promptConfirmation("Do you trust this theme and want to apply it?") {
					color.Yellow("Aborted. Theme was not applied.")
					return nil
				}
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

		// 5. Create symlink FIRST (before saving config)
		// This ensures that if symlink fails, config remains unchanged
		backupPath, err := symlink.CreateSymlink(themePath)
		if err != nil {
			return err
		}

		// 6. Update config only AFTER symlink succeeds
		cfg.PreviousTheme = cfg.CurrentTheme
		cfg.PreviousPath = cfg.CurrentPath
		cfg.CurrentTheme = t.String()
		cfg.CurrentPath = themePath

		if err := cfg.Save(); err != nil {
			// Symlink succeeded but config save failed
			// This is less severe - theme is applied, but rollback info may be lost
			return fmt.Errorf("theme applied but failed to save config: %w", err)
		}

		// Notify user if their original config was backed up
		if backupPath != "" {
			color.Yellow("Your original starship.toml has been backed up to:")
			color.Yellow("  %s", backupPath)
			color.Cyan("\nYou can apply it later with: stellar apply %s/backup \n", getCurrentUsername())
		}

		color.Green("Applied %s", t)
		return nil
	},
}

func init() {
	applyCmd.Flags().BoolVarP(&forceApply, "force", "f", false, "Skip custom command warning and apply without confirmation")
}
