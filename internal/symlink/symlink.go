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
	"github.com/a3chron/stellar/internal/theme"
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
// It returns the SHA-256 hex hash of the copied content, computed while the
// data streams through so callers that need the hash (e.g. ApplyTheme) don't
// have to re-read the file afterward.
func copyFile(src, dst string) (hash string, err error) {
	source, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() {
		if cerr := source.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close source file: %w", cerr)
		}
	}()

	destination, err := os.Create(dst)
	if err != nil {
		return "", fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() {
		if cerr := destination.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close destination file: %w", cerr)
		}
	}()

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(destination, h), source); err != nil {
		return "", fmt.Errorf("failed to copy file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
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

// BackupInfo describes a backup of the user's original config created by
// backupOriginalConfig/ApplyTheme.
type BackupInfo struct {
	// Path is the on-disk location the original config was copied to.
	Path string
	// Identifier is the theme identifier ("<author>/backup@<version>") that
	// targets this exact backup, built at creation time from the same
	// author/version values used to construct Path (rather than re-derived
	// from Path later), so it always matches where the backup actually lives.
	Identifier string
}

// backupOriginalConfig backs up the user's original starship.toml under
// ~/.config/stellar/<author>/backup/. Backups are versioned: the first one is
// 1.0.toml (the user's genuine original), and every later unmanaged
// starship.toml stellar finds gets the next major version (2.0.toml, 3.0.toml,
// …) so no earlier backup is ever clobbered.
// Returns a *BackupInfo if a backup was created, nil otherwise.
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
func backupOriginalConfig(configPath string, cfg *config.Config) (info *BackupInfo, err error) {
	// Check if the file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, nil // No file to back up
	}

	if cfg == nil {
		cfg = &config.Config{}
	}

	managed := isSymlink(configPath) ||
		(cfg.AppliedHash != "" && hashOf(configPath) == cfg.AppliedHash) ||
		(cfg.CurrentTheme != "" && filesEqual(configPath, cfg.CurrentPath))
	if managed {
		return nil, nil
	}

	author := BackupAuthor()

	// Construct backup directory: ~/.config/stellar/<author>/backup
	backupDir, err := paths.ThemeCacheDir(author, "backup")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve backup directory: %w", err)
	}

	// Pick the first free version slot so an existing backup is never
	// overwritten. The first backup (1.0.toml) holds the user's real original
	// config; any later unmanaged starship.toml (e.g. a hand-written one) is
	// preserved as the next version rather than clobbering the original.
	var backupPath, version string
	for n := 1; ; n++ {
		version = fmt.Sprintf("%d.0", n)
		candidate, perr := paths.ThemeCachePath(author, "backup", version)
		if perr != nil {
			return nil, fmt.Errorf("failed to resolve backup path: %w", perr)
		}
		if _, statErr := os.Stat(candidate); os.IsNotExist(statErr) {
			backupPath = candidate
			break
		}
	}

	// Create backup directory
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Copy the original file to backup location
	if _, err := copyFile(configPath, backupPath); err != nil {
		return nil, fmt.Errorf("failed to back up config: %w", err)
	}

	// Build the identifier from the same author/version values used above,
	// via the identifier format theme.Theme.String() already owns, instead of
	// re-deriving it later by parsing backupPath.
	identifier := (&theme.Theme{Author: author, Name: "backup", Version: version}).String()

	return &BackupInfo{Path: backupPath, Identifier: identifier}, nil
}

// ApplyTheme points ~/.config/starship.toml at the target theme file.
// On Unix a symlink is created; on Windows (or when STELLAR_APPLY_MODE=copy) the
// theme file is copied over starship.toml instead, since Windows handles symlinks
// poorly.
//
// cfg is the caller's loaded config, used to recognize stellar's own
// previously-applied file so it isn't mistaken for a user's original (see
// backupOriginalConfig). It may be nil. On a successful apply, ApplyTheme also
// records the hash of the applied content into cfg.AppliedHash (when cfg is
// non-nil) so future calls can recognize this exact file as stellar-managed;
// this mutates the in-memory cfg, but the caller is still responsible for
// persisting it via cfg.Save().
//
// If an original (non-symlink) starship.toml exists, it's backed up first.
// Returns a *BackupInfo if a backup was created (nil if no backup was needed).
//
// Uses atomic replacement (temp-then-rename) to prevent data loss:
// if applying fails, the original config remains intact.
func ApplyTheme(target string, cfg *config.Config) (info *BackupInfo, err error) {
	configPath, err := StarshipConfigPath()
	if err != nil {
		return nil, err
	}

	// Back up original config if it exists and isn't recognized as stellar's own
	info, err = backupOriginalConfig(configPath, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to backup original config: %w", err)
	}

	// Use atomic replacement to avoid a data-loss window.
	// Strategy: write the new symlink/copy to a temp path, then rename over target.
	configDir := filepath.Dir(configPath)
	tempPath := filepath.Join(configDir, ".starship.toml.stellar-tmp")

	// Remove any stale temp file from a previous failed attempt
	_ = os.Remove(tempPath)

	// Best-effort: compute the hash of what's actually being applied so it can
	// be recorded into cfg below. A failure here just leaves appliedHash empty;
	// that degrades a future run back to the legacy CurrentPath comparison
	// instead of failing this apply outright.
	var appliedHash string

	if IsCopyMode() {
		// copyFile already reads the file in full; compute the hash while the
		// data streams through instead of re-reading it afterward.
		h, err := copyFile(target, tempPath)
		if err != nil {
			return info, fmt.Errorf("failed to copy theme: %w", err)
		}
		appliedHash = h
	} else {
		if err := os.Symlink(target, tempPath); err != nil {
			return info, fmt.Errorf("failed to create symlink: %w", err)
		}
		// In symlink mode there's no copy to piggyback on; hash the target
		// file once.
		appliedHash = hashOf(target)
	}

	// Atomic rename over the target (replaces the destination on POSIX and Windows)
	// If this fails, the original file/symlink is still intact
	if err := os.Rename(tempPath, configPath); err != nil {
		// Clean up temp file on failure
		_ = os.Remove(tempPath)
		return info, fmt.Errorf("failed to replace config: %w", err)
	}

	// Best-effort: record the hash of what was actually applied so future
	// runs can recognize this exact file as stellar-managed regardless of
	// apply mode. Empty on error; that just means a future run falls back to
	// the legacy CurrentPath comparison instead of failing outright.
	if cfg != nil {
		cfg.AppliedHash = appliedHash
	}

	return info, nil
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
