// Package paths provides centralized path resolution with environment variable overrides.
// This enables test isolation by allowing tests to redirect all paths to temporary directories.
package paths

import (
	"os"
	"path/filepath"
)

// Environment variable names for path overrides
const (
	// EnvStellarHome overrides ~/.config/stellar
	EnvStellarHome = "STELLAR_HOME"
	// EnvStarshipPath overrides ~/.config/starship.toml
	EnvStarshipPath = "STELLAR_STARSHIP_PATH"
	// EnvTmpDir overrides /tmp/stellar
	EnvTmpDir = "STELLAR_TMP_DIR"
	// EnvAPIURL overrides the API base URL
	EnvAPIURL = "STELLAR_API_URL"
	// EnvApplyMode overrides how themes are applied ("symlink" or "copy").
	// Defaults to symlink on Unix and copy on Windows.
	EnvApplyMode = "STELLAR_APPLY_MODE"
)

// StellarHome returns the stellar configuration directory.
// Returns STELLAR_HOME env var if set, otherwise ~/.config/stellar
func StellarHome() (string, error) {
	if envPath := os.Getenv(EnvStellarHome); envPath != "" {
		return envPath, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".config", "stellar"), nil
}

// StarshipConfig returns the path to starship.toml.
// Returns STELLAR_STARSHIP_PATH env var if set, otherwise ~/.config/starship.toml
func StarshipConfig() (string, error) {
	if envPath := os.Getenv(EnvStarshipPath); envPath != "" {
		return envPath, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".config", "starship.toml"), nil
}

// TmpDir returns the temporary directory for stellar.
// Returns STELLAR_TMP_DIR env var if set, otherwise /tmp/stellar
func TmpDir() string {
	if envPath := os.Getenv(EnvTmpDir); envPath != "" {
		return envPath
	}
	return filepath.Join(os.TempDir(), "stellar")
}

// APIURL returns the API base URL.
// Returns STELLAR_API_URL env var if set, otherwise the default production URL
func APIURL(defaultURL string) string {
	if envURL := os.Getenv(EnvAPIURL); envURL != "" {
		return envURL
	}
	return defaultURL
}

// ConfigPath returns the path to config.json within the stellar home directory
func ConfigPath() (string, error) {
	stellarHome, err := StellarHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(stellarHome, "config.json"), nil
}

// ThemeCachePath returns the cache path for a theme version
func ThemeCachePath(author, name, version string) (string, error) {
	stellarHome, err := StellarHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(stellarHome, author, name, version+".toml"), nil
}

// ThemeCacheDir returns the cache directory for a theme (without version)
func ThemeCacheDir(author, name string) (string, error) {
	stellarHome, err := StellarHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(stellarHome, author, name), nil
}

// TmpThemePath returns the temporary cache path for a theme
func TmpThemePath(author, name, version string) string {
	return filepath.Join(TmpDir(), author, name, version+".toml")
}

// TmpThemeDir returns the temporary cache directory for a theme
func TmpThemeDir(author, name string) string {
	return filepath.Join(TmpDir(), author, name)
}
