package cmd

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/symlink"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var forceApply bool
var updateTheme bool

// printBackupNotice tells the user their original starship.toml was preserved
// and how to restore it. Shared by apply and rollback so both surface the
// same notice whenever backupOriginalConfig actually creates (or recognizes
// an existing) backup.
//
// A foreign symlink (one pointing outside stellar's home, e.g. a
// dotfiles-manager-managed ~/.config/starship.toml) gets a distinct message:
// "backed up" alone would be misleading there, since what was actually lost
// is the symlink itself (the user's dotfiles wiring), not a plain file.
//
// info.AlreadyBackedUp means the content matched an existing backup
// byte-for-byte, so no new backup slot was created - the notice says so
// instead of claiming a fresh backup was just made (see
// backupOriginalConfig).
func printBackupNotice(info *symlink.BackupInfo) {
	if info.ForeignSymlinkTarget != "" {
		starshipPath, _ := symlink.StarshipConfigPath()
		color.Yellow("%s was a symlink to %s.", starshipPath, info.ForeignSymlinkTarget)
		if info.AlreadyBackedUp {
			color.Yellow("Its content is already backed up as %s - stellar now manages this path.", info.Identifier)
			color.Cyan("\nRestore the original symlink's content anytime with: stellar apply %s \n", info.Identifier)
			return
		}
		color.Yellow("Its content was backed up as %s - stellar now manages this path.", info.Identifier)
		color.Cyan("\nRestore the original symlink's content anytime with: stellar apply %s \n", info.Identifier)
		return
	}
	if info.AlreadyBackedUp {
		color.Yellow("Your original starship.toml is already backed up (unchanged) as:")
		color.Yellow("  %s", info.Path)
		color.Cyan("\nYou can apply it later with: stellar apply %s \n", info.Identifier)
		return
	}
	color.Yellow("Your original starship.toml has been backed up to:")
	color.Yellow("  %s", info.Path)
	color.Cyan("\nYou can apply it later with: stellar apply %s \n", info.Identifier)
}

// warnPostApplyEnvironment prints non-fatal warnings when the environment
// means the just-applied theme won't actually be visible: starship itself
// isn't installed, or $STARSHIP_CONFIG points somewhere other than the file
// stellar manages (in which case starship never reads what apply just wrote).
// These are warnings, not errors - the apply itself succeeded.
//
// The $STARSHIP_CONFIG check compares against the managed starship.toml path
// itself (symlink.StarshipConfigPath()) - NOT the cached theme file apply
// just wrote content into. Those are different files even in symlink mode
// (one is a symlink pointing at the other), so comparing $STARSHIP_CONFIG
// against the cache file used to warn even when it was correctly set to the
// managed starship.toml. Both sides are run through EvalSymlinks
// (best-effort - a failure just falls back to the cleaned, unresolved path)
// so a $STARSHIP_CONFIG that reaches the same file via another route (e.g.
// through the stellar symlink itself) is still recognized as correct.
func warnPostApplyEnvironment() {
	if _, err := exec.LookPath("starship"); err != nil {
		color.Yellow("\nNote: 'starship' was not found on your PATH.")
		color.Yellow("This theme won't show up until starship is installed: https://starship.rs/guide/#%s", "%F0%9F%9A%80-installation")
	}

	envConfig := os.Getenv("STARSHIP_CONFIG")
	if envConfig == "" {
		return
	}

	starshipPath, err := symlink.StarshipConfigPath()
	if err != nil {
		return
	}

	if samePath(envConfig, starshipPath) {
		return
	}

	color.Yellow("\nNote: $STARSHIP_CONFIG is set to %s, not %s.", envConfig, starshipPath)
	color.Yellow("Starship reads that file instead of the one stellar just applied - unset $STARSHIP_CONFIG or point it at %s.", starshipPath)
}

// samePath reports whether a and b refer to the same file, comparing cleaned
// absolute paths and, best-effort, their resolved (symlink-free) forms.
// EvalSymlinks errors are ignored and simply fall back to the unresolved
// path on that side, rather than failing the comparison outright - a
// dangling or not-yet-created path must not make this always say "not
// equal".
func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(absA); err == nil {
		absA = resolved
	}
	if resolved, err := filepath.EvalSymlinks(absB); err == nil {
		absB = resolved
	}
	return filepath.Clean(absA) == filepath.Clean(absB)
}

var applyCmd = &cobra.Command{
	Use:   "apply [author/theme[@version]]",
	Short: "Apply a Starship theme",
	Example: `  stellar apply a3chron/ctp-blue
  stellar apply a3chron/ctp-blue@1.2
  stellar apply a3chron/ctp-blue --update`,
	Args: argsWithUsage(cobra.ExactArgs(1)),
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

		client := api.NewClient()
		isLocalOnly := false

		// 3. Resolve the version unless a concrete one was given.
		if t.NeedsVersionResolution() {
			themeDir, _ := t.CacheDir()
			localVer, localErr := theme.FindLatestLocalVersion(themeDir)
			hasLocalCache := localErr == nil

			// A cache written by an older build can itself contain a literal
			// "latest.toml" - exactly the users this resolution exists for - so
			// that is not an answer either. Fall through to the hub for a
			// concrete version.
			if hasLocalCache && !updateTheme && localVer != theme.LatestVersion {
				t.Version = localVer
			} else {
				// Check online for latest version (first download or --update)
				info, infoErr := client.GetThemeInfo(t.Author, t.Name)
				if infoErr == nil && len(info.Versions) > 0 {
					// Online theme found - use latest version from API
					latestVersion := info.Versions[0].Version

					// If updating, report whether a newer version is actually
					// available - staying silent either way used to leave
					// `apply --update` on an already-current theme printing
					// nothing at all.
					if hasLocalCache && updateTheme {
						if latestVersion != localVer {
							color.Yellow("Update available: %s -> %s", localVer, latestVersion)
						} else {
							color.Green("Already on the latest version (%s)", latestVersion)
						}
					}

					t.Version = latestVersion
				} else {
					// Fallback to local cache
					isLocalOnly = true
					if !hasLocalCache {
						return hubUnreachableError(t, infoErr)
					}
					t.Version = localVer
					color.HiBlack("%s", hubUnavailableNotice(t, infoErr))
				}
			}
		}

		// 5. Check if cached, download if not
		if !cache.ThemeExists(t) {
			if isLocalOnly {
				return fmt.Errorf("theme not found in local cache: %s", t)
			}

			var content string

			if cache.TmpThemeExists(t) {
				// Reuse theme downloaded during a recent preview
				color.Cyan("Using previewed theme from temporary cache...")
				raw, err := os.ReadFile(cache.TmpCachePath(t))
				if err != nil {
					return fmt.Errorf("failed to read tmp cache: %w", err)
				}
				content = string(raw)
			} else {
				color.Yellow("Downloading %s...", t)
				content, err = client.FetchThemeConfig(t.Author, t.Name, t.Version)
				if err != nil {
					return downloadError(client, t, err)
				}
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
			if err := confirmCustomCommands(t, validationResult, forceApply, "", "apply", "applied"); err != nil {
				return err
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

		// Validate the config before applying, regardless of source: a
		// freshly downloaded theme was already validated above (and would
		// have refused to cache if invalid), but a theme that was already
		// cached - including a local/hand-written one under
		// ~/.config/stellar/<author>/<theme>/ that never went through the
		// download path at all - has never been checked. Invalid TOML here
		// would otherwise apply fine and break every starship prompt render
		// afterward. --force downgrades this to a printed warning instead of
		// a refusal (see validateOnDiskTheme).
		if _, verr := validateOnDiskTheme(t, themePath, forceApply); verr != nil {
			return verr
		}

		alreadyCurrent := t.String() == cfg.CurrentTheme

		// 5. Apply the theme FIRST (before saving config)
		// This ensures that if applying fails, config remains unchanged
		backupInfo, err := symlink.ApplyTheme(themePath, cfg)
		if err != nil {
			return err
		}

		// 6. Update config only AFTER applying succeeds
		// (ApplyTheme has already recorded cfg.AppliedHash for the applied file)
		if !alreadyCurrent {
			// Re-applying the theme that's already current is a repair/
			// refresh, not a real theme change: leave Previous untouched so
			// rollback doesn't just bounce back to the same theme (apply A,
			// apply B, apply B, rollback should land on A, not B).
			cfg.PreviousTheme = cfg.CurrentTheme
			cfg.PreviousPath = cfg.CurrentPath
		}
		cfg.CurrentTheme = t.String()
		cfg.CurrentPath = themePath

		if err := cfg.Save(); err != nil {
			// Symlink succeeded but config save failed
			// This is less severe - theme is applied, but rollback info may be lost
			return fmt.Errorf("theme applied but failed to save config: %w", err)
		}

		// Notify user if their original config was backed up
		if backupInfo != nil {
			printBackupNotice(backupInfo)
		}

		if alreadyCurrent {
			color.Green("Already applied (refreshed): %s", t)
		} else {
			color.Green("Applied %s", t)
		}

		warnPostApplyEnvironment()

		return nil
	},
}

func init() {
	applyCmd.Flags().BoolVarP(&forceApply, "force", "f", false,
		"Skip the custom-command confirmation, and skip TOML validation (with a warning) for a theme already on disk - a freshly downloaded theme is still always validated")
	applyCmd.Flags().BoolVarP(&updateTheme, "update", "u", false, "Check for and download newer version if available")
}
