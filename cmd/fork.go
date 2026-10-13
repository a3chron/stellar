// cmd/fork.go implements `stellar fork`: copy an existing theme (hub-first,
// cache as offline fallback) into a new local theme the user owns and can
// edit freely.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/paths"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var forkForce bool

var forkCmd = &cobra.Command{
	Use:   "fork <author/slug[@version]> [[author/]name]",
	Short: "Copy a theme into one you own, to edit freely",
	Long: `Copy an existing theme into a new local theme under your own author, so you
can edit it freely without touching the original.

Fetches from stellar-hub first (resolving "latest" the same way "stellar
apply" does), falling back to your local cache if the hub can't be reached.
Without a destination name, the theme keeps its slug under your logged-in
(or OS) username. Either way, the destination's theme name is normalized to
the hub's slug form (lowercase, dashes).`,
	Example: `  stellar fork a3chron/ctp-blue my-blue
  stellar fork a3chron/ctp-blue@1.2
  stellar fork a3chron/ctp-blue a3chron/my-blue`,
	Args: argsWithUsage(cobra.RangeArgs(1, 2)),
	RunE: func(cmd *cobra.Command, args []string) error {
		r := newPromptReader(cmd)
		tty := isTerminal(cmd.InOrStdin())

		t, err := theme.ParseIdentifier(args[0])
		if err != nil {
			return err
		}

		author := cachedAuthor()
		var name string
		normalized := false
		if len(args) == 2 {
			author, name, normalized, err = splitAuthoredName(args[1])
			if err != nil {
				return err
			}
		} else {
			// No destination given: the fork keeps the source's own slug,
			// still run through the same normalization - a source that only
			// ever existed locally (never published) isn't guaranteed to
			// already be in slug form.
			var nerr error
			name, nerr = normalizeDestinationName(t.Name)
			if nerr != nil {
				return nerr
			}
			normalized = name != t.Name
		}

		destPath, err := paths.ThemeCachePath(author, name, "1.0")
		if err != nil {
			return err
		}
		if err := ensureFreshThemeDir(author, name, filepath.Dir(destPath)); err != nil {
			return err
		}

		// Unlike the usual "config.Load() failed -> use an empty Config"
		// fallback elsewhere, a failure here must not proceed to Save()
		// later: an empty *Config saved over a config.json that merely
		// failed to *read* would silently wipe the user's real
		// current/downloaded-theme state.
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("couldn't load config.json: %w", err)
		}

		client := api.NewClient()

		// Resolve the version hub-first, exactly as documented (README:
		// "Fetches from stellar-hub first ... falling back to your local
		// cache if the hub can't be reached") - preferring any cached copy
		// first, as an earlier version of this did, could silently fork a
		// stale version even when the hub has a newer one and is perfectly
		// reachable.
		if t.NeedsVersionResolution() {
			info, infoErr := client.GetThemeInfo(t.Author, t.Name)
			latestVersion := ""
			if infoErr == nil && len(info.Versions) > 0 {
				// The numeric maximum, per the contract's definition of
				// "latest" - not whichever entry the hub happens to list
				// first.
				latestVersion = maxThemeVersion(info.Versions)
			}
			// Never trust a version string from the hub unchecked: it ends
			// up in a cache file path (CachePath) and in the "Forked from"
			// header below - "../" or an embedded newline must not reach
			// either.
			if latestVersion != "" && !theme.IsValidVersion(latestVersion) {
				infoErr = fmt.Errorf("stellar-hub returned an invalid version %q for %s/%s", latestVersion, t.Author, t.Name)
				latestVersion = ""
			}

			if latestVersion != "" {
				t.Version = latestVersion
			} else {
				themeDir, _ := t.CacheDir()
				localVer, localErr := theme.FindLatestLocalVersion(themeDir)
				if localErr != nil {
					return hubUnreachableError(t, infoErr)
				}
				t.Version = localVer
				color.HiBlack("%s", hubUnavailableNotice(t, infoErr))
			}
		}

		themeID := fmt.Sprintf("%s/%s", t.Author, t.Name)
		var content string
		fetched := false

		if cache.ThemeExists(t) {
			srcPath, perr := t.CachePath()
			if perr != nil {
				return perr
			}
			raw, rerr := os.ReadFile(srcPath)
			if rerr != nil {
				return rerr
			}
			content = string(raw)
		} else {
			fetchedContent, ferr := client.FetchThemeConfig(t.Author, t.Name, t.Version)
			if ferr != nil {
				return downloadError("fork", client, t, ferr)
			}
			content = fetchedContent
			fetched = true
		}

		validationResult, verr := theme.ValidateConfigContent(content)
		if verr != nil {
			return fmt.Errorf("validation error: %w", verr)
		}
		if !validationResult.Valid {
			return validationResult.Error
		}

		// fork writes the content straight into a theme the user now owns,
		// and "stellar apply" never re-prompts for a theme that's already on
		// disk - so this is the only chance to warn about [custom] commands
		// before they become silently trusted local content. Declining
		// aborts and writes nothing.
		if validationResult.HasCustomCommands && !forkForce {
			printCustomCommandWarning(t, "")
			ok, cerr := promptYesNo(r, tty, "Do you trust this theme and want to fork it?", false, "--force")
			if cerr != nil {
				return fmt.Errorf("fork aborted: %w", cerr)
			}
			if !ok {
				return fmt.Errorf("aborted: theme was not forked")
			}
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		sourceURL := fmt.Sprintf("%s/%s/%s", paths.APIURL(api.BaseURL), t.Author, t.Name)
		header := fmt.Sprintf("# Forked from %s (%s)\n\n", t.String(), sourceURL)
		if err := writeNewThemeFile(destPath, []byte(header+content)); err != nil {
			return err
		}

		// Only count a download for content that was actually fetched from
		// the hub just now - reusing an already-cached copy must not inflate
		// anything. The hub's public counter is skipped for dev builds
		// (IsDev), same as apply; the local "you have this" bookkeeping
		// still happens regardless, so `stellar clean` treats it as a plain,
		// re-fetchable download rather than an untracked/"own" theme.
		if fetched {
			if !IsDev() && !cfg.HasDownloaded(themeID) {
				if ierr := client.IncrementDownloadCount(t.Author, t.Name); ierr != nil {
					color.HiBlack("download count failed: %v", ierr)
				}
			}
			cfg.MarkDownloaded(themeID)
		}

		destID := fmt.Sprintf("%s/%s", author, name)
		cfg.MarkAuthored(destID)
		if err := cfg.Save(); err != nil {
			color.Yellow("Warning: couldn't record %s as authored (stellar clean may remove it): %v", destID, err)
		}

		color.Green("✓ Forked %s -> %s", t, destID)
		if normalized {
			color.HiBlack(normalizedNameNotice(destID))
		}
		fmt.Printf("  %s\n", destPath)
		fmt.Println()
		color.Cyan("Next steps:")
		fmt.Printf("  stellar apply %s      # try it out\n", destID)
		fmt.Printf("  stellar publish %s    # publish it to stellar-hub\n", destID)
		return nil
	},
}

func init() {
	forkCmd.Flags().BoolVarP(&forkForce, "force", "f", false, "Skip the [custom]-command confirmation when forking a theme that runs shell commands")
	forkCmd.ValidArgsFunction = themeIdentifierArgs
}
