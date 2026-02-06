package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/cache"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var previewCmd = &cobra.Command{
	Use:   "preview [author/theme[@version]]",
	Short: "Preview a theme in a new shell",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		identifier := args[0]

		// Parse and download if needed
		t, err := theme.ParseIdentifier(identifier)
		if err != nil {
			return err
		}

		if !cache.ThemeExists(t) {
			color.Yellow("Downloading %s...", t)

			client := api.NewClient()
			content, err := client.FetchThemeConfig(t.Author, t.Name, t.Version)
			if err != nil {
				return err
			}

			if err := theme.ValidateConfigContent(content); err != nil {
				return err
			}

			if err := cache.SaveTheme(t, content); err != nil {
				return err
			}
		}

		themePath, err := t.CachePath()
		if err != nil {
			return err
		}

		// Create temporary config for preview
		tempConfig := filepath.Join(os.TempDir(), "stellar-preview.toml")
		content, _ := os.ReadFile(themePath)
		os.WriteFile(tempConfig, content, 0644)

		// Spawn new shell with custom STARSHIP_CONFIG
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/bash"
		}

		color.Cyan("Preview mode - type 'exit' to return\n")

		shellCmd := exec.Command(shell)
		shellCmd.Env = append(os.Environ(), fmt.Sprintf("STARSHIP_CONFIG=%s", tempConfig))
		shellCmd.Stdin = os.Stdin
		shellCmd.Stdout = os.Stdout
		shellCmd.Stderr = os.Stderr

		return shellCmd.Run()
	},
}
