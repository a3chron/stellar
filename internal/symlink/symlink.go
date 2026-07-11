package symlink

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

// filesEqual reports whether both files exist and have identical content.
// Any read error counts as "not equal", which for backup decisions errs on
// the side of preserving the file.
func filesEqual(a, b string) bool {
	contentA, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	contentB, err := os.ReadFile(b)
	if err != nil {
		return false
	}
	return bytes.Equal(contentA, contentB)
}

// HashFile computes the SHA-256 hash of a file and returns it as a hex string.
func HashFile(path string) (hash string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file for hashing: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("failed to hash file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashOf returns the SHA-256 hex hash of path, or "" if it can't be read.
// Used for "is this managed" comparisons, where an unreadable file should
// simply fail to match rather than propagate an error.
func hashOf(path string) string {
	h, err := HashFile(path)
	if err != nil {
		return ""
	}
	return h
}

// sanitizeBackupAuthor turns a raw OS username into a valid theme-author
// segment. Windows usernames from user.Current() look like "DOMAIN\user" and
// may contain spaces or other characters the theme-identifier parser rejects
// (it allows only [a-zA-Z0-9_-]). We drop any domain prefix and replace every
// disallowed character with '-', falling back to "local" if nothing usable is
// left, so the printed restore hint always parses.
func sanitizeBackupAuthor(name string) string {
	// Drop everything up to and including the last path separator so a
	// "DOMAIN\user" (or "domain/user") input keeps only the "user" part.
	if i := strings.LastIndexAny(name, `\/`); i >= 0 {
		name = name[i+1:]
	}

	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}

	sanitized := b.String()
	if sanitized == "" {
		return "local"
	}
	return sanitized
}

// BackupAuthor returns the theme-author segment used for backups of the user's
// original config: the sanitized current username, or "local" if it can't be
// determined. Exported so callers can build a restore hint that matches where
// the backup was actually written.
func BackupAuthor() string {
	u, err := user.Current()
	if err != nil {
		return "local"
	}
	return sanitizeBackupAuthor(u.Username)
}

// backupOriginalConfig backs up the user's original starship.toml under
// ~/.config/stellar/<author>/backup/. Backups are versioned: the first one is
// 1.0.toml (the user's genuine original), and every later unmanaged
// starship.toml stellar finds gets the next major version (2.0.toml, 3.0.toml,
// …) so no earlier backup is ever clobbered.
// Returns the backup path if a backup was created, empty string otherwise.
//
// The "is this stellar's own file" check is mode-independent: it never asks
// whether we're in symlink or copy mode, only what's actually on disk and
// recorded in cfg. Each signal only ever *prevents* a backup; a false
// negative just means an extra (harmless) backup, so this fails safe:
//   - configPath is a symlink (symlink mode's own marker), or
//   - cfg.AppliedHash is set and matches configPath's current content, or
//   - cfg.CurrentTheme is set and configPath's content matches the cached
//     theme file byte-for-byte (legacy fallback for configs saved before
//     AppliedHash existed).
//
// cfg may be nil, treated the same as an empty config (i.e. no known state,
// so nothing is recognized as managed and an unmanaged file always backs up).
func backupOriginalConfig(configPath string, cfg *config.Config) (backupPath string, err error) {
	// Check if the file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return "", nil // No file to back up
	}

	if cfg == nil {
		cfg = &config.Config{}
	}

	managed := isSymlink(configPath) ||
		(cfg.AppliedHash != "" && hashOf(configPath) == cfg.AppliedHash) ||
		(cfg.CurrentTheme != "" && filesEqual(configPath, cfg.CurrentPath))
	if managed {
		return "", nil
	}

	// Construct backup directory: ~/.config/stellar/<author>/backup
	backupDir, err := paths.ThemeCacheDir(BackupAuthor(), "backup")
	if err != nil {
		return "", fmt.Errorf("failed to resolve backup directory: %w", err)
	}

	// Pick the first free version slot so an existing backup is never
	// overwritten. The first backup (1.0.toml) holds the user's real original
	// config; any later unmanaged starship.toml (e.g. a hand-written one) is
	// preserved as the next version rather than clobbering the original.
	for n := 1; ; n++ {
		candidate, perr := paths.ThemeCachePath(BackupAuthor(), "backup", fmt.Sprintf("%d.0", n))
		if perr != nil {
			return "", fmt.Errorf("failed to resolve backup path: %w", perr)
		}
		if _, statErr := os.Stat(candidate); os.IsNotExist(statErr) {
			backupPath = candidate
			break
		}
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

// BackupIdentifier derives the theme identifier ("<author>/backup@<version>")
// for a backup file from its path alone, so a printed restore hint always
// matches where the backup was actually written, regardless of how BackupAuthor
// resolves at call time.
func BackupIdentifier(backupPath string) string {
	version := strings.TrimSuffix(filepath.Base(backupPath), ".toml")
	author := filepath.Base(filepath.Dir(filepath.Dir(backupPath)))
	return fmt.Sprintf("%s/backup@%s", author, version)
}

// ApplyTheme points ~/.config/starship.toml at the target theme file.
// On Unix a symlink is created; on Windows (or when STELLAR_APPLY_MODE=copy) the
// theme file is copied over starship.toml instead, since Windows handles symlinks
// poorly.
//
// cfg is the caller's loaded config, used only to recognize stellar's own
// previously-applied file so it isn't mistaken for a user's original (see
// backupOriginalConfig). It may be nil. This function does not read or write
// cfg's persisted state itself; the caller is responsible for saving it.
//
// If an original (non-symlink) starship.toml exists, it's backed up first.
// Returns the backup path if a backup was created (empty string if no backup was needed).
//
// Uses atomic replacement (temp-then-rename) to prevent data loss:
// if applying fails, the original config remains intact.
func ApplyTheme(target string, cfg *config.Config) (backupPath string, err error) {
	configPath, err := StarshipConfigPath()
	if err != nil {
		return "", err
	}

	// Back up original config if it exists and isn't recognized as stellar's own
	backupPath, err = backupOriginalConfig(configPath, cfg)
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
