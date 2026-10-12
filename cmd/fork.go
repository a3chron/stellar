// cmd/fork.go implements `stellar fork`: copy an existing theme (hub-first,
// cache as offline fallback) into a new local theme the user owns and can
// edit freely.
package cmd

import (
	"fmt"
	"log"
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

var forkCmd = &cobra.Command{
	Use:   "fork <author/slug[@version]> [[author/]name]",
	Short: "Copy a theme into one you own, to edit freely",
	Long: `Copy an existing theme into a new local theme under your own author, so you
can edit it freely without touching the original.

Fetches from stellar-hub first (resolving "latest" the same way "stellar
apply" does), falling back to your local cache if the hub can't be reached.
Without a destination name, the theme keeps its slug under your logged-in
(or OS) username.`,
	Example: `  stellar fork a3chron/ctp-blue my-blue
  stellar fork a3chron/ctp-blue@1.2
  stellar fork a3chron/ctp-blue a3chron/my-blue`,
	Args: argsWithUsage(cobra.RangeArgs(1, 2)),
	RunE: func(cmd *cobra.Command, args []string) error {
		t, err := theme.ParseIdentifier(args[0])
		if err != nil {
			return err
		}

		author, name := cachedAuthor(), t.Name
		if len(args) == 2 {
			author, name, err = splitAuthoredName(args[1])
			if err != nil {
				return err
			}
		}

		destPath, err := paths.ThemeCachePath(author, name, "1.0")
		if err != nil {
			return err
		}
		if _, statErr := os.Stat(destPath); statErr == nil {
			return fmt.Errorf("%s/%s already exists at %s - pick another name", author, name, destPath)
		}

		cfg, err := config.Load()
		if err != nil {
			cfg = &config.Config{}
		}

		client := api.NewClient()

		// Resolve the version exactly like "stellar apply" does.
		if t.NeedsVersionResolution() {
			themeDir, _ := t.CacheDir()
			localVer, localErr := theme.FindLatestLocalVersion(themeDir)
			hasLocalCache := localErr == nil

			if hasLocalCache && localVer != theme.LatestVersion {
				t.Version = localVer
			} else {
				info, infoErr := client.GetThemeInfo(t.Author, t.Name)
				if infoErr == nil && len(info.Versions) > 0 {
					t.Version = info.Versions[0].Version
				} else {
					if !hasLocalCache {
						return hubUnreachableError(t, infoErr)
					}
					t.Version = localVer
					color.HiBlack("%s", hubUnavailableNotice(t, infoErr))
				}
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
			return fmt.Errorf("invalid config: %w", validationResult.Error)
		}
		if validationResult.HasCustomCommands {
			color.Yellow("[custom] notice: this theme runs shell commands - anyone who applies your fork will see a security warning.")
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		sourceURL := fmt.Sprintf("%s/%s/%s", paths.APIURL(api.BaseURL), t.Author, t.Name)
		header := fmt.Sprintf("# Forked from %s (%s)\n\n", t.String(), sourceURL)
		if err := os.WriteFile(destPath, []byte(header+content), 0644); err != nil {
			return err
		}

		// Only count a download for content that was actually fetched from
		// the hub just now, and only once per theme - reusing an
		// already-cached copy, or forking a theme forked/applied before,
		// must not inflate the count.
		if fetched && !cfg.HasDownloaded(themeID) {
			if ierr := client.IncrementDownloadCount(t.Author, t.Name); ierr != nil {
				log.Printf("download count failed: %v", ierr)
			}
			cfg.MarkDownloaded(themeID)
		}

		destID := fmt.Sprintf("%s/%s", author, name)
		cfg.MarkAuthored(destID)
		if err := cfg.Save(); err != nil {
			color.Yellow("Warning: couldn't record %s as authored (stellar clean may remove it): %v", destID, err)
		}

		color.Green("Forked %s -> %s", t, destID)
		fmt.Printf("  %s\n", destPath)
		fmt.Println()
		color.Cyan("Next steps:")
		fmt.Printf("  stellar apply %s      # try it out\n", destID)
		fmt.Printf("  stellar publish %s    # publish it to stellar-hub\n", destID)
		return nil
	},
}

func init() {
	forkCmd.ValidArgsFunction = themeIdentifierArgs
}
