package cmd

import (
	"fmt"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info [author/theme]",
	Short: "Show detailed information about a theme",
	Long:  `Display detailed information about a theme including versions, dependencies, and download count.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		identifier := args[0]

		// Parse identifier
		t, err := theme.ParseIdentifier(identifier)
		if err != nil {
			return err
		}

		// Fetch theme info from API
		client := api.NewClient()
		info, err := client.GetThemeInfo(t.Author, t.Name)
		if err != nil {
			return fmt.Errorf("failed to fetch theme info: %w", err)
		}

		// Display theme information
		color.Cyan("═══════════════════════════════════════")
		color.Green("  %s", info.Name)
		color.Cyan("═══════════════════════════════════════")
		fmt.Println()

		// Basic info
		fmt.Printf("Author:       %s\n", info.Author.Name)
		fmt.Printf("Slug:         %s\n", info.Slug)
		if info.Description != "" {
			fmt.Printf("Description:  %s\n", info.Description)
		}
		fmt.Printf("Downloads:    %d\n", info.Downloads)
		fmt.Println()

		// Versions
		color.Yellow("Versions (%d):", len(info.Versions))
		for _, v := range info.Versions {
			fmt.Printf("  • %s", v.Version)
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
