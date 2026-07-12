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
	"time"

	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/paths"
	"github.com/a3chron/stellar/internal/theme"
)

func StarshipConfigPath() (string, error) {
	return paths.StarshipConfig()
}

// renameRetryEnabled gates RenameWithRetry's retry loop. It is true only on
// Windows, the one platform where os.Rename intermittently fails on a lock that
// clears by itself; everywhere else a rename either succeeds or fails for a
// real, persistent reason, so retrying would only add latency. It's a
// package-level var (rather than an inline runtime.GOOS check) purely so a unit
// test running on Linux can flip it on and exercise the retry path.
var renameRetryEnabled = runtime.GOOS == "windows"

// renameRetryBackoff is the sequence of sleeps between rename attempts when the
// retry path is active. Four retries at 50/100/200/400ms cap the total added
// latency of a genuinely persistent failure at 750ms — comfortably under a
// second and a half — while giving a transient Defender/indexer lock ample time
// to clear. It's a package-level var so a test can shrink or replace it.
var renameRetryBackoff = []time.Duration{
	50 * time.Millisecond,
	100 * time.Millisecond,
	200 * time.Millisecond,
	400 * time.Millisecond,
}

// renameSleep is the sleep used between rename retries, injectable so a unit
// test can verify the retry/backoff behavior without actually sleeping.
var renameSleep = time.Sleep

// RenameWithRetry renames oldpath to newpath, retrying a few times on Windows
// when the rename fails.
//
// Why: on Windows os.Rename intermittently fails with ERROR_SHARING_VIOLATION
// or ERROR_ACCESS_DENIED when another process holds a brief lock on the source
// or destination. Windows Defender's real-time scan and the Search Indexer both
// do exactly this to freshly written files and brand-new .exe binaries — which
// is precisely what stellar renames when applying a theme (a just-written temp
// file) or installing an update (a just-downloaded executable). These locks
// clear on their own within milliseconds, so a short, bounded retry turns an
// intermittent, user-visible failure into a silent success.
//
// On every other OS renameRetryEnabled is false, so this is a single os.Rename
// with no sleep and no retry — behavior there is identical to calling os.Rename
// directly.
//
// We retry on any error rather than trying to match specific Windows error
// codes: the sharing-violation codes don't map cleanly onto Go's portable error
// types, and retrying indiscriminately is safe here because a genuinely
// persistent failure still returns promptly — the total backoff is bounded by
// renameRetryBackoff.
func RenameWithRetry(oldpath, newpath string) error {
	err := os.Rename(oldpath, newpath)
	if err == nil || !renameRetryEnabled {
		return err
	}

	// Retry with growing backoff, returning the last error if none succeed.
	for _, backoff := range renameRetryBackoff {
		renameSleep(backoff)
		if err = os.Rename(oldpath, newpath); err == nil {
			return nil
		}
	}

	return err
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
		if theme.IsValidIdentifierRune(r) {
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

// IsManaged reports whether the file at configPath is one stellar itself
// applied, as opposed to a user's own original config that must be preserved.
//
// The check is mode-independent: it never asks whether we're in symlink or copy
// mode, only what's actually on disk and recorded in cfg. Each signal only ever
// *confirms* management, so a false negative just means an unmanaged file is
// (harmlessly) backed up rather than data being lost — it fails safe toward
// preserving the file:
//   - configPath is a symlink (symlink mode's own marker), or
//   - cfg.AppliedHash is set and matches configPath's current content, or
//   - cfg.CurrentTheme is set and configPath's content matches the cached
//     theme file byte-for-byte (legacy fallback for configs saved before
//     AppliedHash existed).
//
// cfg may be nil, treated the same as an empty config (i.e. no known state, so
// nothing is recognized as managed).
func IsManaged(configPath string, cfg *config.Config) bool {
	if cfg == nil {
		cfg = &config.Config{}
	}
	return isSymlink(configPath) ||
		(cfg.AppliedHash != "" && hashOf(configPath) == cfg.AppliedHash) ||
		(cfg.CurrentTheme != "" && filesEqual(configPath, cfg.CurrentPath))
}

// backupOriginalConfig backs up the user's original starship.toml under
// ~/.config/stellar/<author>/backup/. Backups are versioned: the first one is
// 1.0.toml (the user's genuine original), and every later unmanaged
// starship.toml stellar finds gets the next major version (2.0.toml, 3.0.toml,
// …) so no earlier backup is ever clobbered.
// Returns a *BackupInfo if a backup was created, nil otherwise.
//
// Whether the current file is stellar's own (and so must not be backed up) is
// decided by IsManaged; an unmanaged file always backs up.
//
// cfg may be nil, treated the same as an empty config.
func backupOriginalConfig(configPath string, cfg *config.Config) (info *BackupInfo, err error) {
	// Check if the file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, nil // No file to back up
	}

	if IsManaged(configPath, cfg) {
		return nil, nil
	}

	author := BackupAuthor()

	// Construct backup directory: ~/.config/stellar/<author>/backup
	backupDir, err := paths.ThemeCacheDir(author, theme.BackupThemeName)
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
		candidate, perr := paths.ThemeCachePath(author, theme.BackupThemeName, version)
		if perr != nil {
			return nil, fmt.Errorf("failed to resolve backup path: %w", perr)
		}
		// A free slot (ENOENT) ends the search; a slot that already exists
		// (statErr == nil) moves to the next version. Any other stat error
		// (e.g. ENOTDIR because the backup dir is actually a plain file, or
		// EACCES) is persistent and would otherwise spin this loop forever, so
		// fail loudly instead of hanging.
		_, statErr := os.Stat(candidate)
		if os.IsNotExist(statErr) {
			backupPath = candidate
			break
		}
		if statErr != nil {
			return nil, fmt.Errorf("failed to probe backup slot %s: %w", candidate, statErr)
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
	identifier := (&theme.Theme{Author: author, Name: theme.BackupThemeName, Version: version}).String()

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
	// If this fails, the original file/symlink is still intact. RenameWithRetry
	// rides out the transient Defender/indexer locks Windows takes on the
	// freshly written temp file (a no-op single rename everywhere else).
	if err := RenameWithRetry(tempPath, configPath); err != nil {
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
