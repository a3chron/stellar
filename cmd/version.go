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

	// LatestReleaseAPIURL is the GitHub API endpoint used to fetch the latest
	// release's metadata (tag, publish date, HTML URL). It is a var rather
	// than a const so tests can point it at an httptest server; production
	// behavior is unchanged since the default is assigned here.
	LatestReleaseAPIURL = "https://api.github.com/repos/a3chron/stellar/releases/latest"
)

// SetVersionInfo is called from main to set version information. It runs on
// every single invocation of the stellar binary, for every command - not
// just `stellar version` / `stellar --version` - so it must stay cheap and
// must never block on the network.
//
// SetVersionTemplate only stores the template text and a closure that will
// parse/execute it later (see cobra's tmpl() helper); it does not render
// anything itself. The template text here is just "{{stellarVersionOutput}}",
// a reference to the template func registered in init() below - so the
// actual work (printing the version block, then checking GitHub for a newer
// release) happens only when cobra calls that func, which it does only
// inside Command.execute() when the --version flag was actually set. That is
// what makes the update check lazy: previously this line passed
// getFullVersionOutput() directly, and since Go evaluates function arguments
// before the call, that eagerly ran the update check (a blocking HTTP GET,
// up to several seconds when offline) on every stellar invocation, for every
// command, before SetVersionTemplate even received its argument.
func SetVersionInfo(version, commit, date string) {
	versionInfo.version = version
	versionInfo.commit = commit
	versionInfo.date = date
	// Also set the version for the root command to enable --version flag
	rootCmd.Version = version
	rootCmd.SetVersionTemplate("{{stellarVersionOutput}}")
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
		printVersionOutput()
	},
}

// printVersionOutput writes the ASCII art and version/commit/built block to
// stdout FIRST, then performs the (network-bound) update check and writes its
// result. Two separate writes, in that order, are what make the current
// version show up on the terminal immediately instead of only after the
// GitHub round-trip finishes - the previous behavior buffered both pieces
// together and printed nothing until the network call had returned (or
// timed out), which is exactly what item 1 of A3C-213 requires fixing even
// though item (a)'s eager-evaluation bug was the reason it happened on every
// command instead of only here.
//
// This is the single implementation shared by `stellar version` (the Run
// func above) and `stellar --version` (wired up as a cobra version-template
// func - see the "stellarVersionOutput" registration in init() and the
// comment on SetVersionInfo), so the two behave identically.
func printVersionOutput() {
	fmt.Print(getVersionAsciiArt())

	// Dev builds have no meaningful "latest version" to compare against, so
	// skip the update check entirely - same as before.
	if IsDev() {
		return
	}

	fmt.Print("\nChecking for updates...\n")
	fmt.Print(checkForUpdates())
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

// updateCheckTimeout bounds how long GetLatestRelease's HTTP GET may block.
// Before A3C-213, this same 5s timeout sat on every single command's startup
// path (see SetVersionInfo), so it barely mattered how long it was so long as
// it terminated eventually. Now it only blocks `stellar version` /
// `stellar --version`, i.e. a user actively waiting on the terminal for this
// output - so it's shortened to 3s: GitHub's API responds in well under a
// second when reachable at all, and an offline user waiting on a warning
// message shouldn't have to wait 5s to see it.
const updateCheckTimeout = 3 * time.Second

// GetLatestRelease fetches the latest GitHub release information
func GetLatestRelease() (*GitHubRelease, error) {
	client := &http.Client{Timeout: updateCheckTimeout}

	resp, err := client.Get(LatestReleaseAPIURL)
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
		// Deliberately not the raw error (e.g. "dial tcp: lookup
		// api.github.com: no such host") - that's noise to a user who's just
		// trying to find out if they're on the latest version. A short
		// warning that names the likely cause is more useful.
		fmt.Fprintf(&buf, "%s  couldn't fetch latest version, are you online?%s\n", colorYellow, colorReset)
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

	// Registers the template func SetVersionInfo's "{{stellarVersionOutput}}"
	// template refers to. cobra parses/executes that template - and therefore
	// calls this func - only when Command.execute() has determined the
	// --version flag is actually set, which happens at flag-parse time on
	// each run, long after this init() and long after SetVersionInfo. That
	// laziness is exactly what keeps the update check off every other
	// command's startup path. The func returns "" because printVersionOutput
	// writes directly to stdout itself (matching how `stellar version`'s Run
	// func and the rest of this codebase, e.g. cmd/update.go, write output);
	// the template has nothing left to render.
	cobra.AddTemplateFunc("stellarVersionOutput", func() string {
		printVersionOutput()
		return ""
	})
}
