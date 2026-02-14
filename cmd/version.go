package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorReset  = "\033[0m"
)

var (
	versionInfo = struct {
		version string
		commit  string
		date    string
	}{
		version: "dev",
		commit:  "none",
		date:    "unknown",
	}
)

// SetVersionInfo is called from main to set version information
func SetVersionInfo(version, commit, date string) {
	versionInfo.version = version
	versionInfo.commit = commit
	versionInfo.date = date
	// Also set the version for the root command to enable --version flag
	rootCmd.Version = version
	// Set custom version template to show ASCII art and check for updates
	rootCmd.SetVersionTemplate(getFullVersionOutput())
}

// IsDev returns true if running a development build
func IsDev() bool {
	return versionInfo.version == "dev"
}

type GitHubRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Display the current version of stellar and check for updates`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(getFullVersionOutput())
	},
}

func getFullVersionOutput() string {
	var buf bytes.Buffer

	// Print ASCII art with version info
	buf.WriteString(getVersionAsciiArt())

	// Check for updates if not dev version
	if versionInfo.version != "dev" {
		buf.WriteString("\nChecking for updates...\n")
		buf.WriteString(checkForUpdates())
	}

	return buf.String()
}

func getVersionAsciiArt() string {
	asciiArt := []string{
		"                                ",
		"               ##               ",
		"               ##               ",
		"       ###     ##     ###       ",
		"         ###   ##   ###         ",
		"           ### ## ###           ",
		"             ######             ",
		"    ########################    ",
		"             ######             ",
		"           ### ## ###           ",
		"         ###   ##   ###         ",
		"       ###     ##     ###       ",
		"               ##               ",
		"               ##               ",
		"                                ",
	}

	// Truncate commit hash to 8 characters for cleaner display
	commit := versionInfo.commit
	if len(commit) > 8 {
		commit = commit[:8]
	}

	versionLines := []string{
		"",
		"",
		"",
		"  stellar",
		"",
		fmt.Sprintf("  version: %s", versionInfo.version),
		fmt.Sprintf("  commit:  %s", commit),
		fmt.Sprintf("  built:   %s", versionInfo.date),
	}

	var buf bytes.Buffer
	for i, artLine := range asciiArt {
		buf.WriteString(artLine)
		if i < len(versionLines) && versionLines[i] != "" {
			buf.WriteString(versionLines[i])
		}
		buf.WriteString("\n")
	}

	return buf.String()
}

// GetLatestRelease fetches the latest GitHub release information
func GetLatestRelease() (*GitHubRelease, error) {
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get("https://api.github.com/repos/a3chron/stellar/releases/latest")
	if err != nil {
		return nil, fmt.Errorf("failed to check for updates: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch latest release (status: %d)", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release info: %w", err)
	}

	return &release, nil
}

// IsUpdateAvailable checks if a newer version is available
func IsUpdateAvailable() (bool, string, error) {
	if IsDev() {
		return false, "dev", nil
	}

	release, err := GetLatestRelease()
	if err != nil {
		return false, "", err
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion := strings.TrimPrefix(versionInfo.version, "v")

	return latestVersion != currentVersion, release.TagName, nil
}

func checkForUpdates() string {
	var buf bytes.Buffer

	release, err := GetLatestRelease()
	if err != nil {
		fmt.Fprintf(&buf, "Failed to check for updates: %v\n", err)
		return buf.String()
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion := strings.TrimPrefix(versionInfo.version, "v")

	if latestVersion == currentVersion {
		fmt.Fprintf(&buf, "%s  You have the latest version (%s)%s\n", colorGreen, release.TagName, colorReset)
	} else {
		fmt.Fprintf(&buf, "%s  New version available: %s (current: %s)%s\n", colorYellow, release.TagName, versionInfo.version, colorReset)
		fmt.Fprintf(&buf, "  Released: %s\n", release.PublishedAt.Format("2006-01-02"))
		fmt.Fprintf(&buf, "  View release: %s\n", release.HTMLURL)
		buf.WriteString("\nTo update, run:\n")
		buf.WriteString("  stellar update\n")
	}

	return buf.String()
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
