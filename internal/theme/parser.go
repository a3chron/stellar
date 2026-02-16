package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Theme struct {
	Author          string
	Name            string
	Version         string // Optional, defaults to "latest"
	VersionExplicit bool   // True if version was explicitly specified in the identifier
}

// ParseIdentifier parses "alice/rainbow@1.2", "alice/rainbow@latest", or "alice/rainbow"
func ParseIdentifier(identifier string) (*Theme, error) {
	// Normalize: remove leading/trailing whitespace
	identifier = strings.TrimSpace(identifier)

	// Match pattern: author/name[@version]
	// Version can be numeric (e.g., "1.2", "v1.2") or "latest"
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
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(
		home,
		".config",
		"stellar",
		t.Author,
		t.Name,
		t.Version+".toml",
	), nil
}

// CacheDir returns the directory path for this theme (without version file)
func (t *Theme) CacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(
		home,
		".config",
		"stellar",
		t.Author,
		t.Name,
	), nil
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
		return compareSemver(versions[i], versions[j]) > 0
	})

	return versions[0], nil
}

// compareSemver compares two version strings.
// Returns >0 if a > b, <0 if a < b, 0 if equal.
// Non-numeric versions (like "latest") are sorted to the end.
func compareSemver(a, b string) int {
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
