package theme

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/a3chron/stellar/internal/paths"
)

type Theme struct {
	Author          string
	Name            string
	Version         string // Optional, defaults to "latest"
	VersionExplicit bool   // True if version was explicitly specified in the identifier
}

// BackupThemeName is the reserved theme name used for backups of the user's
// original starship config (they live at <author>/backup). It is not a real
// theme on the hub: internal/symlink writes backups under it, and
// internal/cache skips it at clean time so a backup is never swept away.
const BackupThemeName = "backup"

// IsValidIdentifierRune reports whether r is allowed inside an author or name
// segment of a theme identifier. It is the single definition of that character
// class ([a-zA-Z0-9_-]); ParseIdentifier's regex below and any caller that
// sanitizes input (e.g. internal/symlink.sanitizeBackupAuthor) must agree with
// it, so the two can't silently drift.
func IsValidIdentifierRune(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '_' || r == '-'
}

// versionRe matches a complete version as ParseIdentifier accepts it, minus
// the optional "v" prefix (which the parser strips). Kept next to the
// identifier regex below so the two can't drift.
var versionRe = regexp.MustCompile(`^([0-9]+\.[0-9]+|latest)$`)

// IsValidSegment reports whether s is a complete, usable author or theme
// segment: non-empty and made up entirely of IsValidIdentifierRune runes.
//
// Callers that emit segments they didn't parse themselves (e.g. shell
// completion listing cache directories, or reading an API response) must gate
// on this, so a name that ParseIdentifier would reject is never handed back to
// the user as a suggestion - and so a hostile name can't smuggle control
// characters into a terminal.
func IsValidSegment(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !IsValidIdentifierRune(r) {
			return false
		}
	}
	return true
}

// IsValidVersion reports whether s is a complete version ParseIdentifier
// accepts ("1.2" or "latest"), without the optional "v" prefix. Same contract
// as IsValidSegment: gate emitted versions on it, since a cache directory can
// hold arbitrary *.toml filenames.
func IsValidVersion(s string) bool {
	return versionRe.MatchString(s)
}

// ParseIdentifier parses "alice/rainbow@1.2", "alice/rainbow@latest", or "alice/rainbow"
func ParseIdentifier(identifier string) (*Theme, error) {
	// Normalize: remove leading/trailing whitespace
	identifier = strings.TrimSpace(identifier)

	// Match pattern: author/name[@version]
	// Version can be numeric (e.g., "1.2", "v1.2") or "latest"
	// Note: Only X.Y format is supported intentionally. Themes use minor/patch updates only;
	// major breaking changes should be published as a new theme (e.g., "mytheme-v2").
	re := regexp.MustCompile(`^([a-zA-Z0-9_-]+)/([a-zA-Z0-9_-]+)(?:@v?([0-9]+\.[0-9]+|latest))?$`)
	matches := re.FindStringSubmatch(identifier)

	if matches == nil {
		return nil, fmt.Errorf("invalid theme identifier: %s (expected format: author/theme[@version])", identifier)
	}

	theme := &Theme{
		Author:          matches[1],
		Name:            matches[2],
		Version:         "latest", // Default
		VersionExplicit: matches[3] != "",
	}

	if matches[3] != "" {
		theme.Version = matches[3]
	}

	return theme, nil
}

func (t *Theme) String() string {
	if t.Version == "latest" {
		return fmt.Sprintf("%s/%s", t.Author, t.Name)
	}
	return fmt.Sprintf("%s/%s@%s", t.Author, t.Name, t.Version)
}

func (t *Theme) CachePath() (string, error) {
	return paths.ThemeCachePath(t.Author, t.Name, t.Version)
}

// CacheDir returns the directory path for this theme (without version file)
func (t *Theme) CacheDir() (string, error) {
	return paths.ThemeCacheDir(t.Author, t.Name)
}

// FindLatestLocalVersion scans a theme directory and returns the highest semver version found.
// Falls back to "latest" if only latest.toml exists (backward compatibility).
// Returns error if no .toml files are found.
func FindLatestLocalVersion(themeDir string) (string, error) {
	entries, err := os.ReadDir(themeDir)
	if err != nil {
		return "", err
	}

	var versions []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
			ver := strings.TrimSuffix(e.Name(), ".toml")
			versions = append(versions, ver)
		}
	}

	if len(versions) == 0 {
		return "", fmt.Errorf("no versions found in %s", themeDir)
	}

	// Sort by semver descending, "latest" goes last as fallback
	sort.Slice(versions, func(i, j int) bool {
		return CompareSemver(versions[i], versions[j]) > 0
	})

	return versions[0], nil
}

// CompareSemver compares two version strings.
// Returns >0 if a > b, <0 if a < b, 0 if equal.
// Non-numeric versions (like "latest") are sorted to the end.
func CompareSemver(a, b string) int {
	aMajor, aMinor, aOk := parseSemver(a)
	bMajor, bMinor, bOk := parseSemver(b)

	// Non-semver versions go to the end
	if !aOk && !bOk {
		return strings.Compare(a, b)
	}
	if !aOk {
		return -1 // a goes after b
	}
	if !bOk {
		return 1 // b goes after a
	}

	// Compare major version
	if aMajor != bMajor {
		return aMajor - bMajor
	}
	// Compare minor version
	return aMinor - bMinor
}

// parseSemver parses a version string like "1.2" into major and minor components.
// Returns false if the string is not a valid semver.
func parseSemver(v string) (major, minor int, ok bool) {
	parts := strings.Split(v, ".")
	if len(parts) != 2 {
		return 0, 0, false
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}

	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}

	return major, minor, true
}
