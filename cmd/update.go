package cmd

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

const (
	LatestReleaseURL = "https://github.com/a3chron/stellar/releases/latest/download"
)

// fetchChecksums downloads checksums.txt from GitHub releases
func fetchChecksums() (string, error) {
	checksumsURL := fmt.Sprintf("%s/checksums.txt", LatestReleaseURL)

	resp, err := http.Get(checksumsURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch checksums: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksums not available (status: %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read checksums: %w", err)
	}

	return string(body), nil
}

// parseChecksum extracts the SHA256 hash for a specific binary from checksums.txt
// Format: "hash  filename" (two spaces between hash and filename, goreleaser standard)
func parseChecksum(checksums, binaryName string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(checksums))

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		// goreleaser format: "abc123def456...  stellar-linux-amd64"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		hash := fields[0]
		filename := fields[len(fields)-1]

		if filename == binaryName {
			// Validate hash looks like SHA256 (64 hex characters)
			if len(hash) != 64 {
				return "", fmt.Errorf("invalid hash length for %s: expected 64, got %d", binaryName, len(hash))
			}
			return hash, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading checksums: %w", err)
	}

	return "", fmt.Errorf("checksum not found for binary: %s", binaryName)
}

// computeFileHash computes the SHA256 hash of a file and returns it as a hex string
func computeFileHash(filePath string) (hash string, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file for hashing: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("failed to hash file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// verifyChecksum compares expected and actual checksums
func verifyChecksum(expected, actual, binaryName string) error {
	expected = strings.ToLower(strings.TrimSpace(expected))
	actual = strings.ToLower(strings.TrimSpace(actual))

	if expected != actual {
		return fmt.Errorf(
			"checksum verification failed for %s\n  expected: %s\n  got:      %s\n\nThe downloaded file may be corrupted or tampered with. Please try again",
			binaryName, expected, actual,
		)
	}

	return nil
}

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

		// Construct binary name based on OS/arch
		binary := fmt.Sprintf("stellar-%s-%s", runtime.GOOS, runtime.GOARCH)
		if runtime.GOOS == "windows" {
			return fmt.Errorf("why would you use windows? Anyways, stellar does not yet support windows, but support is planned")
		}

		// Step 1: Fetch checksums.txt for verification
		color.Yellow("Fetching checksums...")
		checksums, err := fetchChecksums()
		if err != nil {
			return fmt.Errorf("failed to fetch checksums: %w", err)
		}

		// Step 2: Parse checksum for our binary
		expectedHash, err := parseChecksum(checksums, binary)
		if err != nil {
			return fmt.Errorf("failed to parse checksums: %w", err)
		}

		// Step 3: Download the binary
		color.Yellow("Downloading %s...", binary)
		downloadURL := fmt.Sprintf("%s/%s", LatestReleaseURL, binary)

		resp, err := http.Get(downloadURL)
		if err != nil {
			return fmt.Errorf("failed to download: %w", err)
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("download failed (status: %d)", resp.StatusCode)
		}

		// Write to temporary file
		tmpFile, err := os.CreateTemp("", "stellar-update-*")
		if err != nil {
			return err
		}
		tmpPath := tmpFile.Name()

		// Cleanup helper - only call this in error paths before rename
		cleanup := func() {
			_ = os.Remove(tmpPath)
		}

		if _, err := io.Copy(tmpFile, resp.Body); err != nil {
			cleanup()
			return err
		}
		if err := tmpFile.Close(); err != nil {
			cleanup()
			return fmt.Errorf("failed to close temp file: %w", err)
		}

		// Step 4: Compute hash of downloaded file
		color.Yellow("Verifying checksum...")
		actualHash, err := computeFileHash(tmpPath)
		if err != nil {
			cleanup()
			return fmt.Errorf("failed to compute checksum: %w", err)
		}

		// Step 5: Verify checksum matches
		if err := verifyChecksum(expectedHash, actualHash, binary); err != nil {
			cleanup()
			return err
		}
		color.Green("Checksum verified successfully")

		// Step 6: Replace current binary (only after checksum verified)
		execPath, err := os.Executable()
		if err != nil {
			cleanup()
			return err
		}

		if err := os.Chmod(tmpPath, 0755); err != nil {
			cleanup()
			return err
		}

		if err := os.Rename(tmpPath, execPath); err != nil {
			cleanup()
			return err
		}

		// No cleanup needed - temp file was successfully moved
		color.Green("Successfully updated to version %s!", latestVersion)
		return nil
	},
}
