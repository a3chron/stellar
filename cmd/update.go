package cmd

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/a3chron/stellar/internal/symlink"
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

		// Construct binary name based on OS/arch (matches goreleaser artifact names)
		binary := fmt.Sprintf("stellar-%s-%s", runtime.GOOS, runtime.GOARCH)
		if runtime.GOOS == "windows" {
			binary += ".exe"
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

		// Resolve the running binary's path up front so the downloaded file can be
		// written next to it. Keeping them on the same volume means the final
		// rename works on all OSes (system temp may be on a different volume).
		execPath, err := os.Executable()
		if err != nil {
			return err
		}

		// Write to a temporary file in the same directory as the current binary
		tmpFile, err := os.CreateTemp(filepath.Dir(execPath), ".stellar-update-*")
		if err != nil {
			return err
		}
		tmpPath := tmpFile.Name()

		// Cleanup helper - only call this in error paths before rename
		cleanup := func() {
			_ = os.Remove(tmpPath)
		}

		if _, err := io.Copy(tmpFile, resp.Body); err != nil {
			// Close first so the temp file isn't left with an open handle
			// (which would block removal on Windows).
			_ = tmpFile.Close()
			cleanup()
			return err
		}
		if err := tmpFile.Close(); err != nil {
			cleanup()
			return fmt.Errorf("failed to close temp file: %w", err)
		}

		// Step 4: Compute hash of downloaded file
		color.Yellow("Verifying checksum...")
		actualHash, err := symlink.HashFile(tmpPath)
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
		if err := os.Chmod(tmpPath, 0755); err != nil {
			cleanup()
			return err
		}

		if err := replaceExecutable(tmpPath, execPath); err != nil {
			cleanup()
			return err
		}

		// No cleanup needed - temp file was successfully moved
		color.Green("Successfully updated to version %s!", latestVersion)
		return nil
	},
}

// cleanupUpdateLeftovers is the PersistentPreRunE gate: it only does anything
// on Windows, since that's the only platform replaceExecutable leaves a ".old"
// binary behind on (a running .exe can't be deleted, only renamed out of the
// way) and the only platform a ".stellar-update-*" temp file can survive a
// crashed update (Unix error paths already remove their own temp file). Gating
// here — rather than inside removeUpdateLeftovers — keeps the glob/remove logic
// itself trivially testable on any OS.
func cleanupUpdateLeftovers() {
	if runtime.GOOS != "windows" {
		return
	}

	execPath, err := os.Executable()
	if err != nil {
		return
	}
	removeUpdateLeftovers(execPath)
}

// removeUpdateLeftovers deletes exactly this binary's ".old" leftover and any
// ".stellar-update-*" temp files next to it. Only stellar's own artifacts are
// touched — the install dir may be a shared bin directory (STELLAR_INSTALL_DIR),
// so a broad "*.old" glob is off-limits. Every removal is best-effort and safe
// to call when nothing is left over.
//
// This runs from PersistentPreRunE, before the update command creates its own
// temp file, so it never deletes an in-progress download of the current
// process. A concurrent "stellar update" in another process could theoretically
// have its temp file removed here; that race is accepted, and is Windows-only
// per the gate in cleanupUpdateLeftovers.
func removeUpdateLeftovers(execPath string) {
	_ = os.Remove(execPath + ".old")

	matches, err := filepath.Glob(filepath.Join(filepath.Dir(execPath), ".stellar-update-*"))
	if err != nil {
		return
	}
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// replaceExecutable swaps the running binary at execPath for the file at newPath.
//
// On Unix a plain rename over the target works. On Windows a running .exe cannot
// be overwritten, but it can be renamed: move the current binary aside, put the
// new one in its place. The old binary stays locked until this process exits,
// so it is deliberately left in place rather than removed here — cleanup
// happens on the next stellar run via cleanupUpdateLeftovers.
func replaceExecutable(newPath, execPath string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(newPath, execPath)
	}

	oldPath := execPath + ".old"
	_ = os.Remove(oldPath) // clear any leftover from a previous update
	if err := os.Rename(execPath, oldPath); err != nil {
		return err
	}
	if err := os.Rename(newPath, execPath); err != nil {
		// Restore the original binary so the user isn't left without one
		_ = os.Rename(oldPath, execPath)
		return err
	}
	return nil
}
