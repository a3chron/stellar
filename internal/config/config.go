package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/a3chron/stellar/internal/paths"
)

type Config struct {
	CurrentTheme  string `json:"current_theme"` // "alice/rainbow@1.2"
	CurrentPath   string `json:"current_path"`  // Full path to .toml
	PreviousTheme string `json:"previous_theme,omitempty"`
	PreviousPath  string `json:"previous_path,omitempty"`
	// Note: a "previous_hash" field used to live here (rollback used it to
	// tell a hand-edited previous theme from an unchanged one, prompting for
	// [custom] commands only in the "changed" case). It's gone now: rollback
	// no longer prompts for [custom] commands on a cached previous theme at
	// all, matching how apply treats an already-cached theme - see
	// cmd/rollback.go. encoding/json silently ignores an unknown
	// "previous_hash" key, so a config.json written by an older stellar
	// build still loads fine; the field is just never populated or read
	// again.
	DownloadedThemes []string `json:"downloaded_themes,omitempty"` // ["alice/rainbow", "bob/sunset"]
	// AppliedHash is the SHA-256 hex hash of the content stellar last wrote to
	// starship.toml. It lets stellar recognize its own applied file regardless
	// of apply mode (symlink vs copy) or OS, without depending on how the file
	// happens to be on disk right now. Absent on configs written before this
	// field existed; callers must treat that as "unknown" rather than
	// "mismatch".
	AppliedHash string `json:"applied_hash,omitempty"`

	// Telemetry identity (see internal/telemetry). InstallID is a random UUID
	// minted once per install; InstallKind records whether config.json was
	// absent when it was minted ("install") or already existed from a build
	// that predates telemetry ("existing"). Both are fixed for the life of the
	// install so a retried report is classified the same way every time.
	InstallID   string `json:"install_id,omitempty"`
	InstallKind string `json:"install_kind,omitempty"`
	// ReportedVersion / ReportedAt describe the last report the hub accepted.
	// A report is only sent when the running version differs from
	// ReportedVersion, so an empty value means "never reported yet".
	ReportedVersion string `json:"reported_version,omitempty"`
	ReportedAt      string `json:"reported_at,omitempty"`
}

func ConfigPath() (string, error) {
	return paths.ConfigPath()
}

func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil // Return empty config
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) Save() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// HasDownloaded checks if a theme (author/slug) was previously downloaded
func (c *Config) HasDownloaded(themeID string) bool {
	for _, t := range c.DownloadedThemes {
		if t == themeID {
			return true
		}
	}
	return false
}

// MarkDownloaded adds a theme to the downloaded list if not already present
func (c *Config) MarkDownloaded(themeID string) {
	if !c.HasDownloaded(themeID) {
		c.DownloadedThemes = append(c.DownloadedThemes, themeID)
	}
}
