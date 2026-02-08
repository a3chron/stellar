package cache

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/a3chron/stellar/internal/theme"
)

func EnsureCacheDir() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	cacheDir := filepath.Join(home, ".config", "stellar")
	return os.MkdirAll(cacheDir, 0755)
}

func SaveTheme(t *theme.Theme, content string) error {
	path, err := t.CachePath()
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	return os.WriteFile(path, []byte(content), 0644)
}

func ThemeExists(t *theme.Theme) bool {
	path, err := t.CachePath()
	if err != nil {
		return false
	}

	_, err = os.Stat(path)
	return err == nil
}

func ListCachedThemes() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	cacheDir := filepath.Join(home, ".config", "stellar")

	var themes []string

	// Walk through author directories
	authors, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil, err
	}

	for _, author := range authors {
		if !author.IsDir() || author.Name() == "config.json" {
			continue
		}

		authorPath := filepath.Join(cacheDir, author.Name())
		themeNames, err := os.ReadDir(authorPath)
		if err != nil {
			continue
		}

		for _, themeName := range themeNames {
			if !themeName.IsDir() {
				continue
			}

			themePath := filepath.Join(authorPath, themeName.Name())
			versions, err := os.ReadDir(themePath)
			if err != nil {
				continue
			}

			for _, version := range versions {
				if filepath.Ext(version.Name()) == ".toml" {
					ver := strings.TrimSuffix(version.Name(), ".toml")
					themes = append(themes, fmt.Sprintf("%s/%s@%s",
						author.Name(), themeName.Name(), ver))
				}
			}
		}
	}

	return themes, nil
}

func CleanCache(excludeCurrent string) error {
	themes, err := ListCachedThemes()
	if err != nil {
		return err
	}

	for _, themeID := range themes {
		if themeID == excludeCurrent {
			continue
		}

		t, err := theme.ParseIdentifier(themeID)
		if err != nil {
			continue
		}

		path, err := t.CachePath()
		if err != nil {
			continue
		}

		if err := os.Remove(path); err != nil {
			log.Printf("warning: failed to remove %s: %v", path, err)
		}
	}

	return nil
}
