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

// LatestReleaseURL is the base URL for downloading the latest release's
// assets (checksums.txt and the per-platform binaries). It is a var rather
// than a const so tests can point it at an httptest server; production
// behavior is unchanged since the default is assigned here.
var LatestReleaseURL = "https://github.com/a3chron/stellar/releases/latest/download"

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

// platformBinaryName returns the release asset name for the running platform,
// matching the per-platform artifact names goreleaser produces
// ("stellar-<os>-<arch>", with a ".exe" suffix on Windows). It is the single
// definition of that name, shared by updateCmd and the update E2E test.
func platformBinaryName() string {
	name := fmt.Sprintf("stellar-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
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

		// Release asset name for this platform (matches goreleaser artifact names)
		binary := platformBinaryName()

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

		// replaceExecutable owns the fate of tmpPath from here on: on success it
		// has been renamed into place, and on failure it has either removed it
		// (nothing of value lost) or deliberately preserved it for manual
		// recovery (with instructions in the returned error). The caller must
		// not call cleanup() itself, or it could delete the only verified copy
		// of the new binary out from under a recovery path.
		if err := replaceExecutable(tmpPath, execPath); err != nil {
			return err
		}

		color.Green("Successfully updated to version %s!", latestVersion)
		return nil
	},
}

// cleanupUpdateLeftovers is the PersistentPreRunE gate: it best-effort removes
// artifacts a previous "stellar update" run may have left behind — a ".old"
// binary (Windows renames the running .exe aside rather than overwriting it,
// since a running .exe can't be deleted) and any ".stellar-update-*" temp file
// that survived an interrupted update (e.g. Ctrl-C or SIGKILL before the
// update command's own cleanup ran). Temp files are created next to the
// binary on every OS, so this cleanup runs on every OS, not just Windows.
func cleanupUpdateLeftovers() {
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
// Directory entries are listed with os.ReadDir and matched with
// strings.HasPrefix rather than filepath.Glob: Glob interprets "[", "]", "*"
// etc. in the directory path itself as pattern metacharacters, so an install
// dir containing one of those characters (e.g. "tools[1]") could silently
// corrupt the pattern or return ErrBadPattern, both of which left leftovers
// on disk forever.
//
// This runs from PersistentPreRunE, before the update command creates its own
// temp file, so it never deletes an in-progress download of the current
// process. A concurrent "stellar update" in another process could theoretically
// have its temp file removed here; that race is accepted as unlikely.
func removeUpdateLeftovers(execPath string) {
	_ = os.Remove(execPath + ".old")

	dir := filepath.Dir(execPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".stellar-update-") {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

// replaceExecutable swaps the running binary at execPath for the checksum-
// verified file at newPath. It owns newPath's fate entirely: on success
// newPath has been renamed into place and no longer exists; on failure it has
// either removed newPath (nothing of value was lost) or deliberately
// preserved it with recovery instructions in the returned error. Callers must
// not remove newPath themselves after calling this.
//
// On Unix a plain rename over the target works, and a failure means execPath
// was never touched, so newPath is simply discarded.
//
// On Windows a running .exe cannot be overwritten, but it can be renamed:
// move the current binary aside, then put the new one in its place. If
// putting the new binary in place fails, we try to restore the original
// binary automatically. If even that restore fails (execPath now missing
// entirely), newPath is deliberately NOT removed — it is the only verified
// copy of the new binary — and the error tells the user exactly what to
// rename to recover.
func replaceExecutable(newPath, execPath string) error {
	if runtime.GOOS != "windows" {
		if err := os.Rename(newPath, execPath); err != nil {
			_ = os.Remove(newPath)
			return err
		}
		return nil
	}

	oldPath := execPath + ".old"
	_ = os.Remove(oldPath) // clear any leftover from a previous update

	if err := os.Rename(execPath, oldPath); err != nil {
		// execPath was never touched, so the verified download isn't needed.
		_ = os.Remove(newPath)
		return err
	}

	if err := os.Rename(newPath, execPath); err != nil {
		if restoreErr := os.Rename(oldPath, execPath); restoreErr == nil {
			// Original binary restored; the verified download isn't needed.
			_ = os.Remove(newPath)
			return err
		}

		// Double failure: execPath is now missing entirely. Preserve newPath
		// (the verified new binary) and oldPath (the previous binary) and
		// tell the user exactly how to recover instead of silently deleting
		// the one file that's known-good.
		return fmt.Errorf(
			"failed to install update and could not restore the previous binary: %w\n\n"+
				"Your files were not deleted, but manual recovery is needed:\n"+
				"  - verified new binary: %s\n"+
				"  - previous binary:     %s\n\n"+
				"To finish the update yourself, run:\n"+
				"  move %q %q",
			err, newPath, oldPath, newPath, execPath,
		)
	}

	// New binary is now in place; the old one stays locked until this process
	// exits, so it is left for cleanupUpdateLeftovers on the next stellar run.
	return nil
}
