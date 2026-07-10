package symlink

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/paths"
)

func StarshipConfigPath() (string, error) {
	return paths.StarshipConfig()
}

// IsCopyMode reports whether themes are applied by copying the file instead of
// symlinking. This is the default on Windows (where symlinks require Developer
// Mode or admin privileges) and can be forced anywhere via STELLAR_APPLY_MODE.
func IsCopyMode() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(paths.EnvApplyMode))) {
	case "copy":
		return true
	case "symlink":
		return false
	}
	return runtime.GOOS == "windows"
}

// isSymlink checks if the given path is a symlink
func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}

// copyFile copies the contents of src to dst, creating or truncating dst.
func copyFile(src, dst string) (err error) {
	source, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() {
		if cerr := source.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close source file: %w", cerr)
		}
	}()

	destination, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() {
		if cerr := destination.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close destination file: %w", cerr)
		}
	}()

	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}

// backupOriginalConfig backs up the user's original starship.toml to ~/.config/stellar/<username>/backup/1.0.toml
// Returns the backup path if successful, empty string otherwise
func backupOriginalConfig(configPath string) (backupPath string, err error) {
	// Check if the file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return "", nil // No file to back up
	}

	if IsCopyMode() {
		// Copy mode leaves starship.toml as a regular file, so there's no
		// symlink to distinguish a user's original config from one stellar
		// copied in. Fall back to stellar's own state: if a theme is already
		// applied, the current file is our copy and must not be backed up
		// (doing so would clobber the real original backup).
		if cfg, cerr := config.Load(); cerr == nil && cfg.CurrentTheme != "" {
			return "", nil
		}
	} else if isSymlink(configPath) {
		return "", nil // Already a symlink, no need to back up
	}

	// Get the current user's username
	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}

	// Construct backup path: ~/.config/stellar/<username>/backup/1.0.toml
	stellarHome, err := paths.StellarHome()
	if err != nil {
		return "", fmt.Errorf("failed to get stellar home directory: %w", err)
	}

	backupDir := filepath.Join(stellarHome, currentUser.Username, "backup")
	backupPath = filepath.Join(backupDir, "1.0.toml")

	// Never overwrite an existing backup: the first one holds the user's real
	// original config. This is a safety net for copy mode, where a lost or reset
	// config.json could otherwise make stellar mistake its own copy for the
	// original and clobber the genuine backup.
	if _, err := os.Stat(backupPath); err == nil {
		return "", nil
	}

	// Create backup directory
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Copy the original file to backup location
	if err := copyFile(configPath, backupPath); err != nil {
		return "", fmt.Errorf("failed to back up config: %w", err)
	}

	return backupPath, nil
}

// ApplyTheme points ~/.config/starship.toml at the target theme file.
// On Unix a symlink is created; on Windows (or when STELLAR_APPLY_MODE=copy) the
// theme file is copied over starship.toml instead, since Windows handles symlinks
// poorly.
//
// If an original (non-symlink) starship.toml exists, it's backed up first.
// Returns the backup path if a backup was created (empty string if no backup was needed).
//
// Uses atomic replacement (temp-then-rename) to prevent data loss:
// if applying fails, the original config remains intact.
func ApplyTheme(target string) (backupPath string, err error) {
	configPath, err := StarshipConfigPath()
	if err != nil {
		return "", err
	}

	// Back up original config if it exists and is not a symlink
	backupPath, err = backupOriginalConfig(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to backup original config: %w", err)
	}

	// Use atomic replacement to avoid a data-loss window.
	// Strategy: write the new symlink/copy to a temp path, then rename over target.
	configDir := filepath.Dir(configPath)
	tempPath := filepath.Join(configDir, ".starship.toml.stellar-tmp")

	// Remove any stale temp file from a previous failed attempt
	_ = os.Remove(tempPath)

	if IsCopyMode() {
		if err := copyFile(target, tempPath); err != nil {
			return backupPath, fmt.Errorf("failed to copy theme: %w", err)
		}
	} else {
		if err := os.Symlink(target, tempPath); err != nil {
			return backupPath, fmt.Errorf("failed to create symlink: %w", err)
		}
	}

	// Atomic rename over the target (replaces the destination on POSIX and Windows)
	// If this fails, the original file/symlink is still intact
	if err := os.Rename(tempPath, configPath); err != nil {
		// Clean up temp file on failure
		_ = os.Remove(tempPath)
		return backupPath, fmt.Errorf("failed to replace config: %w", err)
	}

	return backupPath, nil
}

// GetCurrentTarget returns the theme file that starship.toml points at.
// Only meaningful in symlink mode; in copy mode there is no link to read, so
// callers should gate this behind !IsCopyMode() and rely on config state instead.
func GetCurrentTarget() (string, error) {
	configPath, err := StarshipConfigPath()
	if err != nil {
		return "", err
	}

	target, err := os.Readlink(configPath)
	if err != nil {
		return "", err
	}

	return target, nil
}
