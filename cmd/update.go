package cmd

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

const (
	LatestReleaseURL = "https://github.com/a3chron/stellar/releases/latest/download"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update stellar CLI to the latest version",
	RunE: func(cmd *cobra.Command, args []string) error {
		color.Yellow("Checking for updates...")

		// Check if update is available
		updateAvailable, latestVersion, err := IsUpdateAvailable()
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}

		if !updateAvailable {
			color.Green("You're already on the latest version (%s)", latestVersion)
			return nil
		}

		color.Yellow("Updating to version %s...", latestVersion)

		// Construct download URL based on OS/arch
		binary := fmt.Sprintf("stellar-%s-%s", runtime.GOOS, runtime.GOARCH)
		if runtime.GOOS == "windows" {
			return fmt.Errorf("why would you use windows? Anyways, stellar does not yet support windows, but support is planned")
		}

		downloadURL := fmt.Sprintf("%s/%s", LatestReleaseURL, binary)

		// Download new binary
		resp, err := http.Get(downloadURL)
		if err != nil {
			return fmt.Errorf("failed to download: %w", err)
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("no update available")
		}

		// Write to temporary file
		tmpFile, err := os.CreateTemp("", "stellar-update-*")
		if err != nil {
			return err
		}
		defer func() {
			if err := os.Remove(tmpFile.Name()); err != nil {
				log.Printf("failed to remove temp file: %v", err)
			}
		}()

		if _, err := io.Copy(tmpFile, resp.Body); err != nil {
			return err
		}
		if err := tmpFile.Close(); err != nil {
			log.Printf("warning: failed to close temp file: %v", err)
		}

		// Get current executable path
		execPath, err := os.Executable()
		if err != nil {
			return err
		}

		// Replace current binary
		if err := os.Chmod(tmpFile.Name(), 0755); err != nil {
			return err
		}

		if err := os.Rename(tmpFile.Name(), execPath); err != nil {
			return err
		}

		color.Green("Successfully updated to version %s!", latestVersion)
		return nil
	},
}
