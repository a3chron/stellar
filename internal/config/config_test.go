package config

import (
	"encoding/json"
	"testing"

	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_NonexistentFile(t *testing.T) {
	_ = testutil.SetupTestEnv(t)

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Empty(t, cfg.CurrentTheme)
	assert.Empty(t, cfg.CurrentPath)
	assert.Empty(t, cfg.PreviousTheme)
	assert.Empty(t, cfg.PreviousPath)
}

func TestLoad_ExistingFile(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	configData := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "/path/to/theme.toml",
  "previous_theme": "bob/sunset@2.0",
  "previous_path": "/path/to/previous.toml",
  "downloaded_themes": ["alice/rainbow", "bob/sunset"]
}`
	env.CreateConfig(configData)

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "alice/rainbow@1.0", cfg.CurrentTheme)
	assert.Equal(t, "/path/to/theme.toml", cfg.CurrentPath)
	assert.Equal(t, "bob/sunset@2.0", cfg.PreviousTheme)
	assert.Equal(t, "/path/to/previous.toml", cfg.PreviousPath)
	assert.Equal(t, []string{"alice/rainbow", "bob/sunset"}, cfg.DownloadedThemes)
}

func TestLoad_InvalidJSON(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.CreateConfig("{invalid json")

	_, err := Load()
	assert.Error(t, err)
}

func TestSave(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	cfg := &Config{
		CurrentTheme:     "alice/rainbow@1.0",
		CurrentPath:      "/path/to/theme.toml",
		PreviousTheme:    "bob/sunset@2.0",
		PreviousPath:     "/path/to/previous.toml",
		DownloadedThemes: []string{"alice/rainbow", "bob/sunset"},
	}

	err := cfg.Save()
	require.NoError(t, err)

	configPath := env.StellarDir + "/config.json"
	assert.True(t, env.FileExists(configPath))

	content := env.ReadFile(configPath)
	var loaded Config
	err = json.Unmarshal([]byte(content), &loaded)
	require.NoError(t, err)

	assert.Equal(t, cfg.CurrentTheme, loaded.CurrentTheme)
	assert.Equal(t, cfg.CurrentPath, loaded.CurrentPath)
}

func TestHasDownloaded(t *testing.T) {
	cfg := &Config{
		DownloadedThemes: []string{"alice/rainbow", "bob/sunset"},
	}

	assert.True(t, cfg.HasDownloaded("alice/rainbow"))
	assert.True(t, cfg.HasDownloaded("bob/sunset"))
	assert.False(t, cfg.HasDownloaded("charlie/moon"))
}

func TestMarkDownloaded(t *testing.T) {
	cfg := &Config{}

	cfg.MarkDownloaded("alice/rainbow")
	assert.True(t, cfg.HasDownloaded("alice/rainbow"))
	assert.Len(t, cfg.DownloadedThemes, 1)

	cfg.MarkDownloaded("bob/sunset")
	assert.Len(t, cfg.DownloadedThemes, 2)

	// Duplicate should not add
	cfg.MarkDownloaded("alice/rainbow")
	assert.Len(t, cfg.DownloadedThemes, 2)
}
