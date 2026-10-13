package cmd

import (
	"fmt"

	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/config"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	cleanAll        bool
	cleanIncludeOwn bool
	cleanYes        bool
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove themes stellar downloaded",
	Long: `Remove themes stellar downloaded from the hub, keeping the currently applied
theme, your own themes (hand-made, or created with "stellar new"/"stellar
fork"), and your config backups.

Use --include-own to also remove your own themes - everything except the
currently applied one. This still never removes your backups; the only way
to remove those is "stellar remove <username>/backup". Use --all to also
remove the currently applied theme.`,
	Example: `  stellar clean
  stellar clean --include-own
  stellar clean --all --include-own`,
	Args: argsWithUsage(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			cfg = &config.Config{}
		}

		opts := cache.CleanOptions{
			All:        cleanAll,
			Downloaded: cfg.DownloadedThemes,
			Authored:   cfg.AuthoredThemes,
			IncludeOwn: cleanIncludeOwn,
		}
		if !cleanAll {
			opts.KeepPath = cfg.CurrentPath
		}

		plan, err := cache.PlanClean(opts)
		if err != nil {
			return fmt.Errorf("failed to plan clean: %w", err)
		}

		// Downloads never need a confirmation - only removing the user's own
		// themes does, since those usually can't be re-fetched from anywhere.
		if cleanIncludeOwn && len(plan.OwnThemes) > 0 && !cleanYes {
			color.Yellow("This will also remove these of your own themes:")
			for _, ot := range plan.OwnThemes {
				fmt.Printf("  %s (%d version(s))\n", ot.ID, ot.Versions)
			}
			ok, cerr := promptYesNo(newPromptReader(cmd), "Remove these too?", false, false, "--yes")
			if cerr != nil {
				return cerr
			}
			if !ok {
				// A decline only backs out of removing the user's own themes -
				// the plain download cleanup below still goes ahead, matching
				// "downloads never need a confirmation".
				opts.IncludeOwn = false
				color.Yellow("Keeping your own themes.")
			}
		}

		result, err := cache.CleanCache(opts)
		if err != nil {
			return fmt.Errorf("failed to clean cache: %w", err)
		}

		if result.DownloadedRemoved == 0 && result.OwnRemoved == 0 {
			color.Yellow("Cache already clean")
			return nil
		}

		if result.DownloadedRemoved > 0 {
			color.Green("Removed %d downloaded theme(s)", result.DownloadedRemoved)
		}
		if result.OwnRemoved > 0 {
			color.Green("Removed %d of your own theme(s)", result.OwnRemoved)
		}

		if !cleanAll && cfg.CurrentTheme != "" {
			fmt.Printf("Kept current theme: %s\n", cfg.CurrentTheme)
		}
		if !opts.IncludeOwn && len(plan.OwnThemes) > 0 {
			fmt.Printf("Kept %d of your own themes (use --include-own to remove those too)\n", len(plan.OwnThemes))
		}
		if plan.BackupVersions > 0 {
			fmt.Printf("Kept %d backup version(s)\n", plan.BackupVersions)
		}

		return nil
	},
}

func init() {
	cleanCmd.Flags().BoolVar(&cleanAll, "all", false, "Also remove the currently applied theme")
	cleanCmd.Flags().BoolVar(&cleanIncludeOwn, "include-own", false,
		"Also remove your own themes (hand-made, or from new/fork) - never your config backups, which only `stellar remove <username>/backup` removes")
	cleanCmd.Flags().BoolVarP(&cleanYes, "yes", "y", false, "Skip the confirmation before removing your own themes")
}
