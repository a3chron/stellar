package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all cached themes",
	Long:    `Display all themes that have been downloaded and cached locally, each with its cached versions, newest first.`,
	Example: `  stellar list`,
	Args:    argsWithUsage(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get current theme
		cfg, err := config.Load()
		if err != nil {
			cfg = &config.Config{} // Empty config if doesn't exist
		}

		// List all cached themes
		cached, err := cache.ListCachedThemes()
		if err != nil {
			return fmt.Errorf("failed to list themes: %w", err)
		}

		if len(cached) == 0 {
			color.Yellow("No themes cached yet")
			fmt.Println("\nDownload a theme with: stellar apply <author/theme>")
			return nil
		}

		themes := groupCachedVersions(cached)
		width := 0
		for _, t := range themes {
			width = max(width, len([]rune(t.name)))
		}

		color.Cyan("Cached Themes (%d):\n", len(themes))

		green := color.New(color.FgGreen)
		for _, t := range themes {
			current := ""
			for _, v := range t.versions {
				if t.name+"@"+v == cfg.CurrentTheme {
					current = v
				}
			}

			name := fmt.Sprintf("%-*s", width, t.name)
			if current != "" {
				_, _ = green.Printf("  ✳ %s", name)
			} else {
				fmt.Printf("    %s", name)
			}

			for i, v := range t.versions {
				sep := "  "
				if i > 0 {
					sep = ", "
				}
				if v == current {
					fmt.Print(sep)
					_, _ = green.Printf("%s (current)", v)
				} else {
					fmt.Print(sep + v)
				}
			}
			fmt.Println()
		}

		return nil
	},
}

// cachedThemeVersions is one theme in `stellar list`: "author/name" and its
// cached versions, newest first.
type cachedThemeVersions struct {
	name     string
	versions []string
}

// groupCachedVersions turns cache.ListCachedThemes' flat "author/name@version"
// entries into one entry per theme, sorted by name, with the versions sorted
// numerically, newest first (1.10 before 1.9).
func groupCachedVersions(entries []string) []cachedThemeVersions {
	byName := map[string][]string{}
	for _, entry := range entries {
		i := strings.LastIndex(entry, "@")
		if i < 0 {
			continue
		}
		name, version := entry[:i], entry[i+1:]
		byName[name] = append(byName[name], version)
	}

	themes := make([]cachedThemeVersions, 0, len(byName))
	for name, versions := range byName {
		sort.Slice(versions, func(a, b int) bool {
			return theme.CompareSemver(versions[a], versions[b]) > 0
		})
		themes = append(themes, cachedThemeVersions{name: name, versions: versions})
	}
	sort.Slice(themes, func(a, b int) bool { return themes[a].name < themes[b].name })
	return themes
}
