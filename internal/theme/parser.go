package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Theme struct {
	Author  string
	Name    string
	Version string // Optional, defaults to "latest"
}

// ParseIdentifier parses "alice/rainbow@1.2" or "alice/rainbow"
func ParseIdentifier(identifier string) (*Theme, error) {
	// Normalize: remove leading/trailing whitespace
	identifier = strings.TrimSpace(identifier)

	// Match pattern: author/name[@version]
	re := regexp.MustCompile(`^([a-zA-Z0-9_-]+)/([a-zA-Z0-9_-]+)(?:@v?([0-9]+\.[0-9]+))?$`)
	matches := re.FindStringSubmatch(identifier)

	if matches == nil {
		return nil, fmt.Errorf("invalid theme identifier: %s (expected format: author/theme[@version])", identifier)
	}

	theme := &Theme{
		Author:  matches[1],
		Name:    matches[2],
		Version: "latest", // Default
	}

	if matches[3] != "" {
		theme.Version = matches[3] // Strip 'v' prefix if present
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
