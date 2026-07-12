package cache

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/a3chron/stellar/internal/paths"
	"github.com/a3chron/stellar/internal/theme"
)

func EnsureCacheDir() error {
	cacheDir, err := paths.StellarHome()
	if err != nil {
		return err
	}
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
	cacheDir, err := paths.StellarHome()
	if err != nil {
		return nil, err
	}

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

func CleanCache(excludeCurrentPath string) error {
	themes, err := ListCachedThemes()
	if err != nil {
		return err
	}

	// Track directories to potentially remove
	dirsToCheck := make(map[string]bool)

	for _, themeID := range themes {
		t, err := theme.ParseIdentifier(themeID)
		if err != nil {
			continue
		}

		// Backups of the user's original config live at <author>/backup
		// (see internal/symlink.backupOriginalConfig) and must survive
		// cleaning: they're the only copy of whatever starship.toml the user
		// had before ever running stellar. If clean swept them up, the
		// restore hint stellar itself prints ("stellar apply
		// <author>/backup@<version>") would become permanently
		// unrecoverable, since "backup" isn't a real author on the hub and
		// there's nothing to re-download. This applies to every author, and
		// to every clean variant (CleanCache is the only removal path for
		// both `stellar clean` and `stellar clean --all`; they differ only
		// in excludeCurrentPath). Backups are still removable explicitly via
		// `stellar remove <author>/backup[@version]`.
		if t.Name == theme.BackupThemeName {
			continue
		}

		path, err := t.CachePath()
		if err != nil {
			continue
		}

		// Compare by actual file path
		if excludeCurrentPath != "" && path == excludeCurrentPath {
			continue
		}

		// Remove the theme file
		if err := os.Remove(path); err != nil {
			log.Printf("warning: failed to remove %s: %v", path, err)
		}

		// Track parent directories for cleanup
		themeDir := filepath.Dir(path) // e.g., ~/.config/stellar/author/theme
		dirsToCheck[themeDir] = true
	}

	// If removeDirectories, clean up empty theme and author directories
	for themeDir := range dirsToCheck {
		// Remove theme directory if empty
		if isEmpty, _ := isDirEmpty(themeDir); isEmpty {
			if err := os.Remove(themeDir); err != nil {
				log.Printf("warning: failed to remove directory %s: %v", themeDir, err)
			}

			// Also try to remove author directory if empty
			authorDir := filepath.Dir(themeDir)
			if isEmpty, _ := isDirEmpty(authorDir); isEmpty {
				if err := os.Remove(authorDir); err != nil {
					log.Printf("warning: failed to remove directory %s: %v", authorDir, err)
				}
			}
		}
	}

	return nil
}

func TmpCachePath(t *theme.Theme) string {
	return paths.TmpThemePath(t.Author, t.Name, t.Version)
}

func TmpCacheDir(t *theme.Theme) string {
	return paths.TmpThemeDir(t.Author, t.Name)
}

func TmpThemeExists(t *theme.Theme) bool {
	_, err := os.Stat(TmpCachePath(t))
	return err == nil
}

func SaveThemeToTmp(t *theme.Theme, content string) error {
	path := TmpCachePath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func isDirEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}
