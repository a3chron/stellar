package cmd

import (
	"errors"
	"fmt"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info [author/theme[@version]]",
	Short: "Show detailed information about a theme",
	Long:  `Display detailed information about a theme including versions, dependencies, and download count.`,
	Example: `  stellar info a3chron/ctp-green
  stellar info a3chron/ctp-green@1.2`,
	Args: argsWithUsage(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		identifier := args[0]

		// Parse identifier - accepts an optional @version, e.g.
		// "a3chron/ctp-green@1.2", which is validated against the fetched (or
		// cached, if offline) version list below.
		t, err := theme.ParseIdentifier(identifier)
		if err != nil {
			return err
		}

		// Fetch theme info from API, falling back to cached versions/metadata
		// when the hub can't be reached at all - a local/dev theme, or one the
		// user is simply offline for, would otherwise be unusable with
		// `stellar info` even though `stellar apply` happily uses the cache.
		//
		// A 404 gets the exact same cache fallback as being offline: a theme
		// that was deleted or renamed on the hub, or one that only ever
		// existed locally, still has a perfectly usable local copy - refusing
		// outright here (the old behavior) made `stellar info` less capable
		// than `stellar apply` for the same theme.
		client := api.NewClient()
		info, err := client.GetThemeInfo(t.Author, t.Name)
		offline := false
		localOnly := false
		if err != nil {
			switch {
			case errors.Is(err, api.ErrOffline), errors.Is(err, api.ErrNotFound):
				cachedVersions, cerr := cache.ListThemeVersions(t.Author, t.Name)
				if cerr != nil || len(cachedVersions) == 0 {
					return hubUnreachableError(t, err)
				}
				offline = errors.Is(err, api.ErrOffline)
				localOnly = errors.Is(err, api.ErrNotFound)
				info = &api.ThemeInfo{
					Name:   t.Name,
					Slug:   t.Name,
					Author: api.AuthorInfo{Name: t.Author},
				}
				for _, v := range cachedVersions {
					info.Versions = append(info.Versions, api.VersionInfo{Version: v})
				}
			default:
				return fmt.Errorf("failed to fetch theme info: %w", err)
			}
		}

		// An explicit @version must actually exist among the versions we know
		// about (hub, or cache when offline) - otherwise silently showing
		// every version's info while ignoring the one the user asked about is
		// more confusing than refusing outright.
		if t.VersionExplicit && t.Version != theme.LatestVersion {
			found := false
			for _, v := range info.Versions {
				if v.Version == t.Version {
					found = true
					break
				}
			}
			if !found {
				available := make([]string, len(info.Versions))
				for i, v := range info.Versions {
					available[i] = v.Version
				}
				return versionNotFoundError("info", t, available)
			}
		}

		// Load config to know which version (if any) is currently applied.
		cfg, err := config.Load()
		if err != nil {
			cfg = &config.Config{} // Empty config if doesn't exist
		}

		// Locally cached versions of this theme, newest first. Gracefully
		// treats a missing cache directory / zero cached versions as "none
		// cached" rather than failing the command (cache.ListThemeVersions
		// already returns (nil, nil) for a missing directory).
		cachedVersions, err := cache.ListThemeVersions(t.Author, t.Name)
		if err != nil {
			cachedVersions = nil
		}
		cachedSet := make(map[string]bool, len(cachedVersions))
		for _, cv := range cachedVersions {
			cachedSet[cv] = true
		}

		// Display theme information
		color.Cyan("═══════════════════════════════════════")
		color.Green("  %s", info.Name)
		color.Cyan("═══════════════════════════════════════")
		fmt.Println()
		switch {
		case offline:
			color.HiBlack("(offline - showing cached info only)")
			fmt.Println()
		case localOnly:
			color.HiBlack("(not on stellar-hub - showing local copy)")
			fmt.Println()
		}

		// Basic info
		fmt.Printf("Author:       %s\n", info.Author.Name)
		fmt.Printf("Slug:         %s\n", info.Slug)
		if info.Description != "" {
			fmt.Printf("Description:  %s\n", info.Description)
		}
		if !offline && !localOnly {
			fmt.Printf("Downloads:    %d\n", info.Downloads)
		}
		fmt.Println()

		// Versions
		color.Yellow("Versions (%d):", len(info.Versions))
		for _, v := range info.Versions {
			isCurrent := cachedSet[v.Version] &&
				cfg.CurrentTheme == fmt.Sprintf("%s/%s@%s", t.Author, t.Name, v.Version)

			switch {
			case isCurrent:
				fmt.Print(color.GreenString("  ✳ %s (installed, current)", v.Version))
			case cachedSet[v.Version]:
				fmt.Print(color.GreenString("  ✓ %s (installed)", v.Version))
			default:
				fmt.Printf("  • %s", v.Version)
			}
			if v.VersionNotes != "" {
				fmt.Printf(" - %s", v.VersionNotes)
			}
			fmt.Println()
		}
		fmt.Println()

		// Dependencies (from latest version) TODO: allow info for exact version as well
		if len(info.Versions) > 0 && len(info.Versions[0].Dependencies) > 0 {
			color.Yellow("Dependencies:")
			for _, dep := range info.Versions[0].Dependencies {
				fmt.Printf("  • %s\n", dep)
			}
			fmt.Println()
		}

		// Installation command
		color.Cyan("Install:")
		fmt.Printf("  stellar apply %s/%s\n", t.Author, t.Name)
		fmt.Println()

		return nil
	},
}
