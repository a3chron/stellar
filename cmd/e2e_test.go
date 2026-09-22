// Package cmd contains E2E tests for stellar CLI commands.
// These tests cover complete user workflows and are the primary test suite.
package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a3chron/stellar/internal/paths"
	"github.com/a3chron/stellar/internal/symlink"
	"github.com/a3chron/stellar/internal/testutil"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Apply Tests
// =============================================================================

func TestE2E_Apply(t *testing.T) {
	t.Run("Download and apply remote theme", func(t *testing.T) {
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)
		resetFlags()

		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"apply", "testuser/sample-theme@1.2"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		expectedPath := filepath.Join(env.StellarDir, "testuser", "sample-theme", "1.2.toml")
		assert.True(t, env.FileExists(expectedPath))
		assert.True(t, env.IsSymlink(env.StarshipPath))
		assert.Equal(t, expectedPath, env.ReadSymlink(env.StarshipPath))
	})

	t.Run("Apply cached theme", func(t *testing.T) {
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)
		resetFlags()

		themePath := env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"apply", "local/mytheme@1.0"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		assert.True(t, env.IsSymlink(env.StarshipPath))
		assert.Equal(t, themePath, env.ReadSymlink(env.StarshipPath))
	})

	t.Run("Apply in copy mode (Windows behavior)", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		t.Setenv(paths.EnvApplyMode, "copy")
		resetFlags()

		env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"apply", "local/mytheme@1.0"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		// In copy mode starship.toml is a regular file with the theme's content
		assert.True(t, env.FileExists(env.StarshipPath))
		assert.False(t, env.IsSymlink(env.StarshipPath))
		assert.Equal(t, testutil.SampleTOML(), env.ReadFile(env.StarshipPath))
	})

	t.Run("Backup hint is a valid identifier (copy mode)", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		t.Setenv(paths.EnvApplyMode, "copy")
		resetFlags()

		// A pre-existing, unmanaged starship.toml that will be backed up.
		env.CreateStarshipConfig("# my hand-written original config")
		env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"apply", "local/mytheme@1.0"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)

		// Find the printed restore hint and pull the identifier out of it.
		var hint string
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, "stellar apply ") {
				hint = line
				break
			}
		}
		require.NotEmpty(t, hint, "expected a backup restore hint in output:\n%s", output)

		idx := strings.Index(hint, "stellar apply ")
		identifier := strings.TrimSpace(hint[idx+len("stellar apply "):])

		// The regression: on Windows a raw "DOMAIN\user/backup" hint would not
		// parse. The sanitized identifier must both parse and target the backup.
		parsed, perr := theme.ParseIdentifier(identifier)
		require.NoError(t, perr, "backup hint %q must be a valid identifier", identifier)
		assert.True(t, strings.HasSuffix(parsed.String(), "/backup@1.0"),
			"backup hint %q should target <author>/backup@1.0", identifier)
	})

	t.Run("Hand-edited config is backed up (copy mode)", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		t.Setenv(paths.EnvApplyMode, "copy")
		resetFlags()

		env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())
		env.CreateThemeFile("local", "other", "1.0", testutil.SampleTOMLWithCustom())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"apply", "local/mytheme@1.0"})
		cmd.SetOut(new(bytes.Buffer))
		require.NoError(t, cmd.Execute())

		// The user edits the applied copy directly, then applies another theme.
		editedContent := "# MY HAND-EDITED CONFIG"
		env.CreateStarshipConfig(editedContent)
		resetFlags()

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"apply", "local/other@1.0"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)

		// The edit was detected and preserved as a backup, and the user was told.
		assert.Contains(t, output, "has been backed up")
		backupPath := filepath.Join(env.StellarDir, symlink.BackupAuthor(), "backup", "1.0.toml")
		assert.Equal(t, editedContent, env.ReadFile(backupPath))
		assert.Equal(t, testutil.SampleTOMLWithCustom(), env.ReadFile(env.StarshipPath))
	})

	t.Run("Editing cached theme file then re-applying creates no backup (copy mode)", func(t *testing.T) {
		// Regression for finding #1: the README-documented workflow of editing
		// a cached theme file directly and re-applying it (for a copy-mode
		// hot-reload equivalent) must not be mistaken for a hand-edited
		// starship.toml.
		env := testutil.SetupTestEnv(t)
		t.Setenv(paths.EnvApplyMode, "copy")
		resetFlags()

		themePath := env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"apply", "local/mytheme@1.0"})
		cmd.SetOut(new(bytes.Buffer))
		require.NoError(t, cmd.Execute())

		// The user edits the cached theme file itself, not starship.toml.
		editedThemeContent := "# EDITED CACHED THEME\nformat = \"$all\"\n"
		require.NoError(t, os.WriteFile(themePath, []byte(editedThemeContent), 0644))
		resetFlags()

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"apply", "local/mytheme@1.0"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)

		assert.NotContains(t, output, "backed up", "re-applying an edited cached theme must not trigger a backup")
		assert.Equal(t, editedThemeContent, env.ReadFile(env.StarshipPath))
	})

	t.Run("Clean --all then applying another theme creates no backup (copy mode)", func(t *testing.T) {
		// Regression for finding #1: "stellar clean --all" removes the cached
		// theme file that stellar's copy came from, but the copy on disk (and
		// its recorded applied_hash) don't change, so it must still be
		// recognized as stellar's own file.
		env := testutil.SetupTestEnv(t)
		t.Setenv(paths.EnvApplyMode, "copy")
		resetFlags()

		env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"apply", "local/mytheme@1.0"})
		cmd.SetOut(new(bytes.Buffer))
		require.NoError(t, cmd.Execute())
		resetFlags()

		cleanRunner := NewRootCmd()
		cleanRunner.SetArgs([]string{"clean", "--all"})
		cleanRunner.SetOut(new(bytes.Buffer))
		require.NoError(t, cleanRunner.Execute())
		resetFlags()

		// The next theme becomes available only after the clean (e.g. a fresh
		// download); what matters for this regression is that stellar's own
		// existing copy on disk is still recognized despite its cache source
		// being gone.
		env.CreateThemeFile("local", "other", "1.0", testutil.SampleTOMLWithCustom())

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"apply", "local/other@1.0"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)

		assert.NotContains(t, output, "backed up", "clean --all followed by apply must not trigger a junk backup")
		assert.Equal(t, testutil.SampleTOMLWithCustom(), env.ReadFile(env.StarshipPath))
	})

	t.Run("Switching apply mode copy to symlink creates no backup", func(t *testing.T) {
		// Regression for finding #2: the managed-file check must not be
		// mode-gated. Applying in copy mode, then applying again in symlink
		// mode, must recognize the copy-mode file via applied_hash instead of
		// backing it up as if it were a foreign original.
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)
		resetFlags()

		env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())
		env.CreateThemeFile("local", "other", "1.0", testutil.SampleTOMLWithCustom())

		t.Setenv(paths.EnvApplyMode, "copy")
		cmd := NewRootCmd()
		cmd.SetArgs([]string{"apply", "local/mytheme@1.0"})
		cmd.SetOut(new(bytes.Buffer))
		require.NoError(t, cmd.Execute())
		resetFlags()

		t.Setenv(paths.EnvApplyMode, "symlink")
		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"apply", "local/other@1.0"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)

		assert.NotContains(t, output, "backed up", "mode switch must not trigger a junk backup")
		assert.True(t, env.IsSymlink(env.StarshipPath))
	})

	t.Run("Apply previewed theme from tmp", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()

		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		tmpContent := "# Previewed version\n[character]\nsuccess_symbol = \"PREVIEW\""
		env.CreateTmpThemeFile("testuser", "sample-theme", "1.2", tmpContent)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"apply", "testuser/sample-theme@1.2"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		themePath := filepath.Join(env.StellarDir, "testuser", "sample-theme", "1.2.toml")
		assert.True(t, env.FileExists(themePath))
		assert.Contains(t, env.ReadFile(themePath), "PREVIEW")
	})

	t.Run("Apply with update flag", func(t *testing.T) {
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)
		resetFlags()

		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		env.CreateThemeFile("testuser", "sample-theme", "1.0", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"apply", "--update", "testuser/sample-theme"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		newPath := filepath.Join(env.StellarDir, "testuser", "sample-theme", "1.2.toml")
		assert.True(t, env.FileExists(newPath))
		assert.Equal(t, newPath, env.ReadSymlink(env.StarshipPath))
	})

	t.Run("Apply uses local cache without update", func(t *testing.T) {
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)
		resetFlags()

		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		oldPath := env.CreateThemeFile("testuser", "sample-theme", "1.0", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"apply", "testuser/sample-theme"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		assert.Equal(t, oldPath, env.ReadSymlink(env.StarshipPath))
		newPath := filepath.Join(env.StellarDir, "testuser", "sample-theme", "1.2.toml")
		assert.False(t, env.FileExists(newPath))
	})

	t.Run("Apply specific version", func(t *testing.T) {
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)
		resetFlags()

		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"apply", "testuser/sample-theme@1.1"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		expectedPath := filepath.Join(env.StellarDir, "testuser", "sample-theme", "1.1.toml")
		assert.True(t, env.FileExists(expectedPath))
		assert.Equal(t, expectedPath, env.ReadSymlink(env.StarshipPath))
	})

	t.Run("Apply backs up original config", func(t *testing.T) {
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)
		resetFlags()

		originalContent := "# My original config\n[character]\nsuccess_symbol = \"OLD\""
		env.CreateStarshipConfig(originalContent)
		env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"apply", "local/mytheme@1.0"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		entries, err := os.ReadDir(env.StellarDir)
		require.NoError(t, err)

		foundBackup := false
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != "local" {
				backupPath := filepath.Join(env.StellarDir, entry.Name(), "backup", "1.0.toml")
				if env.FileExists(backupPath) {
					foundBackup = true
					assert.Equal(t, originalContent, env.ReadFile(backupPath))
				}
			}
		}
		assert.True(t, foundBackup, "backup should have been created")
	})

	t.Run("Apply updates config file", func(t *testing.T) {
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)
		resetFlags()

		themePath := env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"apply", "local/mytheme@1.0"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		configContent := env.ReadFile(filepath.Join(env.StellarDir, "config.json"))
		assert.Contains(t, configContent, "local/mytheme@1.0")
		// The config is JSON, so a Windows path is stored with its separators
		// escaped - compare against the encoded form, not the raw path.
		assert.Contains(t, configContent, strings.ReplaceAll(themePath, `\`, `\\`))
		assert.Contains(t, configContent, "applied_hash", "config should record the hash of the applied theme")
	})

	t.Run("Apply nonexistent theme errors", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()

		mockAPI := testutil.NewMockAPIHandler()
		env.SetupMockAPI(mockAPI)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"apply", "nobody/nonexistent@1.0"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Apply invalid identifier errors", func(t *testing.T) {
		_ = testutil.SetupTestEnv(t)
		resetFlags()

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"apply", "invalid-identifier"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		assert.Error(t, err)
	})
}

// =============================================================================
// List Tests
// =============================================================================

func TestE2E_List(t *testing.T) {
	t.Run("Empty cache", func(t *testing.T) {
		_ = testutil.SetupTestEnv(t)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"list"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)
	})

	t.Run("Multiple themes", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)

		env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"list"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)
	})

	t.Run("Current theme marked", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)

		env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + env.StellarDir + `/alice/rainbow/1.0.toml"
}`
		env.CreateConfig(config)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"list"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)
	})

	t.Run("Multiple versions", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)

		env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		env.CreateThemeFile("alice", "rainbow", "1.5", testutil.SampleTOML())
		env.CreateThemeFile("alice", "rainbow", "2.0", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"list"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		assert.NoError(t, err)
	})
}

// =============================================================================
// Current Tests
// =============================================================================

func TestE2E_Current(t *testing.T) {
	t.Run("No theme applied", func(t *testing.T) {
		_ = testutil.SetupTestEnv(t)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"current"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)
	})

	t.Run("Shows applied theme", func(t *testing.T) {
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)

		themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		require.NoError(t, os.Symlink(themePath, env.StarshipPath))

		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + themePath + `"
}`
		env.CreateConfig(config)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"current"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)
	})

	t.Run("Shows applied theme (copy mode)", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		t.Setenv(paths.EnvApplyMode, "copy")

		themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		// Copy mode: starship.toml is a regular file, not a symlink
		env.CreateStarshipConfig(testutil.SampleTOML())

		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + themePath + `"
}`
		env.CreateConfig(config)

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"current"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)
		assert.Contains(t, output, "alice/rainbow@1.0")
	})

	t.Run("Missing starship.toml in copy mode", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		t.Setenv(paths.EnvApplyMode, "copy")

		// No starship.toml on disk, but config claims a theme is applied
		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + env.StellarDir + `/alice/rainbow/1.0.toml"
}`
		env.CreateConfig(config)

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"current"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		assert.NoError(t, execErr)
		assert.Contains(t, output, "Starship config missing")
		assert.Contains(t, output, "stellar apply alice/rainbow@1.0")
	})

	t.Run("Copy mode with cached theme file deleted reports missing", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		t.Setenv(paths.EnvApplyMode, "copy")

		// starship.toml (the copy) is present and healthy, but the cached
		// theme file it was copied from has since been removed (e.g. via
		// "stellar clean --all"). current.go has no symlink to follow in
		// copy mode, so cfg.CurrentPath is the only thing it can check.
		env.CreateStarshipConfig(testutil.SampleTOML())
		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + env.StellarDir + `/alice/rainbow/1.0.toml"
}`
		env.CreateConfig(config)

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"current"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		assert.NoError(t, execErr)
		assert.Contains(t, output, "Theme file missing")
		assert.Contains(t, output, "Cached theme file missing", "wording should acknowledge the file is a standalone copy")
		assert.Contains(t, output, "stellar apply alice/rainbow@1.0")
	})

	t.Run("Copy-applied config inspected with mode=symlink env still reports healthy", func(t *testing.T) {
		// Regression for the mode-switch misclassification bug: current.go
		// must branch on what's actually on disk (a regular file here), not
		// on STELLAR_APPLY_MODE, which only describes how a *future* apply
		// would behave.
		env := testutil.SetupTestEnv(t)
		t.Setenv(paths.EnvApplyMode, "symlink")

		themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		env.CreateStarshipConfig(testutil.SampleTOML())

		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + themePath + `"
}`
		env.CreateConfig(config)

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"current"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)
		assert.Contains(t, output, "Current Theme")
		assert.NotContains(t, output, "missing")
		assert.NotContains(t, output, "broken")
	})

	t.Run("Broken symlink", func(t *testing.T) {
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)

		require.NoError(t, os.Symlink("/nonexistent/path.toml", env.StarshipPath))

		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "/nonexistent/path.toml"
}`
		env.CreateConfig(config)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"current"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		assert.NoError(t, err)
	})

	t.Run("Missing symlink", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)

		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + env.StellarDir + `/alice/rainbow/1.0.toml"
}`
		env.CreateConfig(config)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"current"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		assert.NoError(t, err)
	})

	t.Run("Copy-mode apply left untouched reports healthy", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		t.Setenv(paths.EnvApplyMode, "copy")
		resetFlags()

		env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

		applyCmd := NewRootCmd()
		applyCmd.SetArgs([]string{"apply", "alice/rainbow@1.0"})
		applyCmd.SetOut(new(bytes.Buffer))
		require.NoError(t, applyCmd.Execute())
		resetFlags()

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"current"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)
		assert.Contains(t, output, "Current Theme")
		assert.NotContains(t, output, "modified")
	})

	t.Run("Copy-mode apply then hand-edit reports modified, not healthy", func(t *testing.T) {
		// Regression: current.go used to only check that cfg.CurrentPath (the
		// cached theme file) existed, never comparing starship.toml's actual
		// content against cfg.AppliedHash, so a modified/replaced starship.toml
		// was reported as a healthy applied theme.
		env := testutil.SetupTestEnv(t)
		t.Setenv(paths.EnvApplyMode, "copy")
		resetFlags()

		env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

		applyCmd := NewRootCmd()
		applyCmd.SetArgs([]string{"apply", "alice/rainbow@1.0"})
		applyCmd.SetOut(new(bytes.Buffer))
		require.NoError(t, applyCmd.Execute())
		resetFlags()

		// Hand-edit the applied copy directly (no re-apply).
		env.CreateStarshipConfig("# hand-edited after apply, no longer matches the theme")

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"current"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)
		assert.Contains(t, output, "modified or replaced")
		assert.NotContains(t, output, "Current Theme", "a modified starship.toml must not be reported as healthy")
	})

	t.Run("Empty current path reports broken, not healthy", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		t.Setenv(paths.EnvApplyMode, "copy")

		env.CreateStarshipConfig(testutil.SampleTOML())
		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": ""
}`
		env.CreateConfig(config)

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"current"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)
		assert.Contains(t, output, "Current theme path is unknown")
		assert.NotContains(t, output, "Current Theme")
	})

	t.Run("Starship config replaced by a directory reports broken", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)

		require.NoError(t, os.MkdirAll(env.StarshipPath, 0755))

		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + env.StellarDir + `/alice/rainbow/1.0.toml"
}`
		env.CreateConfig(config)

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"current"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)
		assert.Contains(t, output, "directory, not a file")
	})
}

// =============================================================================
// Remove Tests
// =============================================================================

func TestE2E_Remove(t *testing.T) {
	t.Run("Specific version", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()

		themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"remove", "alice/rainbow@1.0"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)
		assert.False(t, env.FileExists(themePath))
	})

	t.Run("All versions", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()

		env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		env.CreateThemeFile("alice", "rainbow", "1.5", testutil.SampleTOML())
		env.CreateThemeFile("alice", "rainbow", "2.0", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"remove", "alice/rainbow"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		themeDir := filepath.Join(env.StellarDir, "alice", "rainbow")
		assert.False(t, env.FileExists(themeDir))
	})

	t.Run("Multiple themes at once", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()

		path1 := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		path2 := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())
		path3 := env.CreateThemeFile("charlie", "moon", "1.0", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"remove", "alice/rainbow@1.0", "bob/sunset@2.0"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		assert.False(t, env.FileExists(path1))
		assert.False(t, env.FileExists(path2))
		assert.True(t, env.FileExists(path3))
	})

	t.Run("Current theme blocked without force", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()

		themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + themePath + `"
}`
		env.CreateConfig(config)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"remove", "alice/rainbow@1.0"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)
		assert.True(t, env.FileExists(themePath))
	})

	t.Run("Current theme with force", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()

		themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + themePath + `"
}`
		env.CreateConfig(config)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"remove", "--force", "alice/rainbow@1.0"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)
		assert.False(t, env.FileExists(themePath))
	})

	t.Run("Nonexistent theme graceful", func(t *testing.T) {
		_ = testutil.SetupTestEnv(t)
		resetFlags()

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"remove", "nobody/nothing@1.0"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		assert.NoError(t, err)
	})

	t.Run("Invalid identifier errors", func(t *testing.T) {
		_ = testutil.SetupTestEnv(t)
		resetFlags()

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"remove", "invalid-identifier"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		assert.Error(t, err)
	})

	t.Run("Removes empty directories", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()

		env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"remove", "alice/rainbow@1.0"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		assert.False(t, env.FileExists(filepath.Join(env.StellarDir, "alice", "rainbow")))
		assert.False(t, env.FileExists(filepath.Join(env.StellarDir, "alice")))
	})
}

// =============================================================================
// Rollback Tests
// =============================================================================

func TestE2E_Rollback(t *testing.T) {
	t.Run("No previous theme", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)

		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + env.StellarDir + `/alice/rainbow/1.0.toml"
}`
		env.CreateConfig(config)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"rollback"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)
	})

	t.Run("Swaps current and previous", func(t *testing.T) {
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)

		currentPath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		previousPath := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())
		require.NoError(t, os.Symlink(currentPath, env.StarshipPath))

		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + currentPath + `",
  "previous_theme": "bob/sunset@2.0",
  "previous_path": "` + previousPath + `"
}`
		env.CreateConfig(config)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"rollback"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		assert.True(t, env.IsSymlink(env.StarshipPath))
		assert.Equal(t, previousPath, env.ReadSymlink(env.StarshipPath))
	})

	t.Run("Swaps current and previous (copy mode)", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		t.Setenv(paths.EnvApplyMode, "copy")

		currentPath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		previousPath := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOMLWithCustom())
		// Copy mode: starship.toml is a regular file with the current theme's content
		env.CreateStarshipConfig(testutil.SampleTOML())

		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + currentPath + `",
  "previous_theme": "bob/sunset@2.0",
  "previous_path": "` + previousPath + `"
}`
		env.CreateConfig(config)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"rollback"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		// starship.toml now holds the previous theme's content, still a regular file
		assert.False(t, env.IsSymlink(env.StarshipPath))
		assert.Equal(t, testutil.SampleTOMLWithCustom(), env.ReadFile(env.StarshipPath))
	})

	t.Run("Rollback backup notice (copy mode, hand-edited config)", func(t *testing.T) {
		// Regression for the silent-rollback-backup bug: rollback must print
		// the same backup notice apply does whenever backupPath != "".
		env := testutil.SetupTestEnv(t)
		t.Setenv(paths.EnvApplyMode, "copy")

		currentPath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		previousPath := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOMLWithCustom())

		// The user hand-edited the applied copy; config has no applied_hash
		// (fabricated directly), so this can't be recognized as stellar's own.
		editedContent := "# MY HAND-EDITED CONFIG BEFORE ROLLBACK"
		env.CreateStarshipConfig(editedContent)

		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + currentPath + `",
  "previous_theme": "bob/sunset@2.0",
  "previous_path": "` + previousPath + `"
}`
		env.CreateConfig(config)

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"rollback"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)

		assert.Contains(t, output, "has been backed up")
		backupPath := filepath.Join(env.StellarDir, symlink.BackupAuthor(), "backup", "1.0.toml")
		assert.Equal(t, editedContent, env.ReadFile(backupPath))
	})

	t.Run("Double rollback returns to original", func(t *testing.T) {
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)

		currentPath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		previousPath := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())
		require.NoError(t, os.Symlink(currentPath, env.StarshipPath))

		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + currentPath + `",
  "previous_theme": "bob/sunset@2.0",
  "previous_path": "` + previousPath + `"
}`
		env.CreateConfig(config)

		cmd1 := NewRootCmd()
		cmd1.SetArgs([]string{"rollback"})
		require.NoError(t, cmd1.Execute())

		cmd2 := NewRootCmd()
		cmd2.SetArgs([]string{"rollback"})
		require.NoError(t, cmd2.Execute())

		assert.Equal(t, currentPath, env.ReadSymlink(env.StarshipPath))
	})

	t.Run("Redownloads missing theme", func(t *testing.T) {
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)

		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		currentPath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		require.NoError(t, os.Symlink(currentPath, env.StarshipPath))

		// filepath.Join, not "/": this is compared against the symlink target
		// stellar itself writes, which uses the platform separator.
		previousPath := filepath.Join(env.StellarDir, "testuser", "sample-theme", "1.2.toml")

		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + currentPath + `",
  "previous_theme": "testuser/sample-theme@1.2",
  "previous_path": "` + previousPath + `"
}`
		env.CreateConfig(config)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"rollback"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		assert.True(t, env.FileExists(previousPath))
		assert.True(t, env.IsSymlink(env.StarshipPath))
		assert.Equal(t, previousPath, env.ReadSymlink(env.StarshipPath))
	})

	t.Run("Missing previous path errors", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)

		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + env.StellarDir + `/alice/rainbow/1.0.toml",
  "previous_theme": "bob/sunset@2.0"
}`
		env.CreateConfig(config)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"rollback"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		assert.Error(t, err)
	})
}

// =============================================================================
// Clean Tests
// =============================================================================

func TestE2E_Clean(t *testing.T) {
	t.Run("Empty cache", func(t *testing.T) {
		_ = testutil.SetupTestEnv(t)
		resetFlags()

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"clean"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)
	})

	t.Run("Preserves current theme", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()

		currentPath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		otherPath := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())

		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + currentPath + `"
}`
		env.CreateConfig(config)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"clean"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		assert.True(t, env.FileExists(currentPath))
		assert.False(t, env.FileExists(otherPath))
	})

	t.Run("All flag removes everything", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()

		currentPath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())

		config := `{
  "current_theme": "alice/rainbow@1.0",
  "current_path": "` + currentPath + `"
}`
		env.CreateConfig(config)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"clean", "--all"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		assert.False(t, env.FileExists(currentPath))
	})

	t.Run("Removes empty directories", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()

		env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"clean"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		assert.False(t, env.FileExists(filepath.Join(env.StellarDir, "alice", "rainbow")))
		assert.False(t, env.FileExists(filepath.Join(env.StellarDir, "alice")))
	})

	t.Run("No current theme removes all", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()

		path1 := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		path2 := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"clean"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		assert.False(t, env.FileExists(path1))
		assert.False(t, env.FileExists(path2))
	})

	t.Run("Only preserves current version", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()

		env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		currentPath := env.CreateThemeFile("alice", "rainbow", "1.5", testutil.SampleTOML())
		env.CreateThemeFile("alice", "rainbow", "2.0", testutil.SampleTOML())

		config := `{
  "current_theme": "alice/rainbow@1.5",
  "current_path": "` + currentPath + `"
}`
		env.CreateConfig(config)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"clean"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)

		assert.True(t, env.FileExists(currentPath))
		assert.False(t, env.FileExists(filepath.Join(env.StellarDir, "alice", "rainbow", "1.0.toml")))
		assert.False(t, env.FileExists(filepath.Join(env.StellarDir, "alice", "rainbow", "2.0.toml")))
	})

	t.Run("Preserves backups", func(t *testing.T) {
		// Regression: CleanCache used to sweep <author>/backup like any other
		// cached theme, permanently destroying the user's original config and
		// making the printed restore hint ("stellar apply <author>/backup@1.0")
		// unrecoverable, since "backup" isn't a real author on the hub.
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)
		resetFlags()

		// An unmanaged starship.toml gets backed up when a theme is applied.
		originalContent := "# my hand-written original config"
		env.CreateStarshipConfig(originalContent)
		env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())

		applyCmd := NewRootCmd()
		applyCmd.SetArgs([]string{"apply", "local/mytheme@1.0"})
		applyCmd.SetOut(new(bytes.Buffer))
		require.NoError(t, applyCmd.Execute())
		resetFlags()

		backupPath := filepath.Join(env.StellarDir, symlink.BackupAuthor(), "backup", "1.0.toml")
		require.True(t, env.FileExists(backupPath), "precondition: backup should exist before cleaning")

		cleanCmd := NewRootCmd()
		cleanCmd.SetArgs([]string{"clean"})
		cleanCmd.SetOut(new(bytes.Buffer))
		require.NoError(t, cleanCmd.Execute())

		assert.True(t, env.FileExists(backupPath), "stellar clean must never delete backups of the user's original config")
		resetFlags()

		// The restore hint stellar printed must still work.
		restoreCmd := NewRootCmd()
		restoreCmd.SetArgs([]string{"apply", symlink.BackupAuthor() + "/backup@1.0"})
		restoreCmd.SetOut(new(bytes.Buffer))
		require.NoError(t, restoreCmd.Execute())
		assert.Equal(t, originalContent, env.ReadFile(env.StarshipPath))
	})

	t.Run("--all also preserves backups", func(t *testing.T) {
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)
		resetFlags()

		env.CreateStarshipConfig("# another hand-written original config")
		env.CreateThemeFile("local", "mytheme", "1.0", testutil.SampleTOML())

		applyCmd := NewRootCmd()
		applyCmd.SetArgs([]string{"apply", "local/mytheme@1.0"})
		applyCmd.SetOut(new(bytes.Buffer))
		require.NoError(t, applyCmd.Execute())
		resetFlags()

		backupPath := filepath.Join(env.StellarDir, symlink.BackupAuthor(), "backup", "1.0.toml")
		require.True(t, env.FileExists(backupPath), "precondition: backup should exist before cleaning")

		cleanCmd := NewRootCmd()
		cleanCmd.SetArgs([]string{"clean", "--all"})
		cleanCmd.SetOut(new(bytes.Buffer))
		require.NoError(t, cleanCmd.Execute())

		assert.True(t, env.FileExists(backupPath), "stellar clean --all must never delete backups either")
	})
}

// =============================================================================
// Info Tests
// =============================================================================

func TestE2E_Info(t *testing.T) {
	t.Run("Shows theme metadata", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)

		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"info", "testuser/sample-theme"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)
	})

	t.Run("Nonexistent theme errors", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)

		mockAPI := testutil.NewMockAPIHandler()
		env.SetupMockAPI(mockAPI)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"info", "nobody/nonexistent"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		assert.Error(t, err)
	})

	t.Run("Invalid identifier errors", func(t *testing.T) {
		_ = testutil.SetupTestEnv(t)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"info", "invalid-identifier"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		assert.Error(t, err)
	})

	// A3C-158: `stellar info` should mark which of the hub's versions are
	// already present in the local cache, and which one (if any) is the
	// currently applied version.

	t.Run("No local cache marks nothing installed", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)

		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"info", "testuser/sample-theme"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)

		assert.NotContains(t, output, "installed",
			"no version should be marked installed with an empty cache:\n%s", output)
	})

	t.Run("Marks cached versions as installed", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)

		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		// testuser/sample-theme has versions 1.2, 1.1, 1.0 on the hub (see
		// testutil.CreateDefaultMockAPI). Cache only 1.1 and 1.0 locally.
		env.CreateThemeFile("testuser", "sample-theme", "1.1", testutil.SampleTOML())
		env.CreateThemeFile("testuser", "sample-theme", "1.0", testutil.SampleTOML())

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"info", "testuser/sample-theme"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)

		lines := strings.Split(output, "\n")
		var line12, line11, line10 string
		for _, l := range lines {
			switch {
			case strings.Contains(l, "1.2"):
				line12 = l
			case strings.Contains(l, "1.1"):
				line11 = l
			case strings.Contains(l, "1.0"):
				line10 = l
			}
		}

		assert.NotContains(t, line12, "installed", "1.2 is not cached locally:\n%s", output)
		assert.Contains(t, line11, "installed", "1.1 is cached locally:\n%s", output)
		assert.Contains(t, line10, "installed", "1.0 is cached locally:\n%s", output)
		assert.NotContains(t, line11, "current", "1.1 is cached but not applied:\n%s", output)
		assert.NotContains(t, line10, "current", "1.0 is cached but not applied:\n%s", output)
	})

	t.Run("Marks the currently applied version", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)

		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		themePath := env.CreateThemeFile("testuser", "sample-theme", "1.1", testutil.SampleTOML())
		config := `{
  "current_theme": "testuser/sample-theme@1.1",
  "current_path": "` + themePath + `"
}`
		env.CreateConfig(config)

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"info", "testuser/sample-theme"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)

		var line11 string
		for _, l := range strings.Split(output, "\n") {
			if strings.Contains(l, "1.1") {
				line11 = l
				break
			}
		}
		assert.Contains(t, line11, "current", "applied version should be marked current:\n%s", output)
	})

	t.Run("Cached version the hub no longer lists is handled gracefully", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)

		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		// 0.9 is cached locally but not among the hub's versions for
		// testuser/sample-theme (1.2, 1.1, 1.0). The command must not error
		// or crash over the mismatch.
		env.CreateThemeFile("testuser", "sample-theme", "0.9", testutil.SampleTOML())

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"info", "testuser/sample-theme"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		require.NoError(t, err)
	})
}

// =============================================================================
// Preview Tests
// =============================================================================

func TestE2E_Preview(t *testing.T) {
	t.Run("Downloads to tmp cache", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)

		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"preview", "testuser/sample-theme@1.2"})
		cmd.SetOut(new(bytes.Buffer))

		// Preview spawns a terminal, which will fail in tests,
		// but we can verify the download happened
		_ = cmd.Execute()

		// Check theme was downloaded to tmp
		tmpPath := filepath.Join(env.TmpDir, "testuser", "sample-theme", "1.2.toml")
		// Theme may be in main cache if it existed, or tmp if downloaded for preview
		mainPath := filepath.Join(env.StellarDir, "testuser", "sample-theme", "1.2.toml")
		assert.True(t, env.FileExists(tmpPath) || env.FileExists(mainPath),
			"theme should be in tmp or main cache")
	})

	t.Run("Uses existing cache", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)

		// Create theme in main cache
		content := "# Cached version"
		env.CreateThemeFile("local", "mytheme", "1.0", content)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"preview", "local/mytheme@1.0"})
		cmd.SetOut(new(bytes.Buffer))

		// Preview spawns terminal which fails in tests, but should not error before that
		// if theme is found in cache
		_ = cmd.Execute()
	})

	t.Run("Nonexistent theme errors", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)

		mockAPI := testutil.NewMockAPIHandler()
		env.SetupMockAPI(mockAPI)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"preview", "nobody/nonexistent@1.0"})
		cmd.SetOut(new(bytes.Buffer))

		err := cmd.Execute()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

// =============================================================================
// Update Tests
// =============================================================================

// TestE2E_Update exercises `stellar update` against a fake GitHub-shaped
// server, using the LatestReleaseAPIURL/LatestReleaseURL test seams (see
// cmd/version.go and cmd/update.go). versionInfo is pinned to a known
// non-dev version via SetVersionInfo (and restored afterward) so
// IsUpdateAvailable's comparison is deterministic.
//
// The "successful update" subtest deliberately lets the command rename a
// fresh file over os.Executable() - which in a test binary is the compiled
// go-test executable itself. Overwriting it is safe for *this* running
// process (Linux keeps the old, now-nameless inode mapped until the process
// exits), but backupBinaryForUpdateTest restores the original bytes
// afterward so later test runs (and any cached test binary on disk) aren't
// left corrupted.
func TestE2E_Update(t *testing.T) {
	binaryName := platformBinaryName()

	// pointAtServer starts an httptest server for mux, redirects both URL
	// seams at it, and restores the real GitHub URLs afterward.
	pointAtServer := func(t *testing.T, mux *http.ServeMux) {
		t.Helper()
		server := httptest.NewServer(mux)
		t.Cleanup(server.Close)

		origAPIURL, origReleaseURL := LatestReleaseAPIURL, LatestReleaseURL
		LatestReleaseAPIURL = server.URL + "/api/latest"
		LatestReleaseURL = server.URL + "/release"
		t.Cleanup(func() {
			LatestReleaseAPIURL = origAPIURL
			LatestReleaseURL = origReleaseURL
		})
	}

	releaseJSON := func(tag string) string {
		return fmt.Sprintf(
			`{"tag_name":%q,"name":%q,"published_at":"2024-01-01T00:00:00Z","html_url":"https://example.com/%s"}`,
			tag, tag, tag,
		)
	}

	t.Run("Already up to date", func(t *testing.T) {
		_ = testutil.SetupTestEnv(t)
		pinVersion(t, "1.2.3")

		mux := http.NewServeMux()
		mux.HandleFunc("/api/latest", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(releaseJSON("v1.2.3")))
		})
		pointAtServer(t, mux)

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"update"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)
		assert.Contains(t, output, "already on the latest version")
	})

	t.Run("Checksum mismatch aborts and leaves binary untouched", func(t *testing.T) {
		_ = testutil.SetupTestEnv(t)
		pinVersion(t, "1.0.0")

		fakeBody := []byte("fake stellar binary contents for checksum-mismatch test")

		execPath, err := os.Executable()
		require.NoError(t, err)
		originalContent, err := os.ReadFile(execPath)
		require.NoError(t, err)

		mux := http.NewServeMux()
		mux.HandleFunc("/api/latest", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(releaseJSON("v1.1.0")))
		})
		mux.HandleFunc("/release/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
			// A well-formed but wrong hash (64 hex chars).
			wrongHash := strings.Repeat("a", 64)
			_, _ = fmt.Fprintf(w, "%s  %s\n", wrongHash, binaryName)
		})
		mux.HandleFunc("/release/"+binaryName, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(fakeBody)
		})
		pointAtServer(t, mux)

		cmd := NewRootCmd()
		cmd.SetArgs([]string{"update"})
		cmd.SetOut(new(bytes.Buffer))
		err = cmd.Execute()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "checksum verification failed")

		// Nothing was ever renamed into place: the real binary is untouched.
		currentContent, rerr := os.ReadFile(execPath)
		require.NoError(t, rerr)
		assert.Equal(t, originalContent, currentContent, "binary must be untouched when checksum verification fails")
	})

	t.Run("Successful update replaces the binary", func(t *testing.T) {
		_ = testutil.SetupTestEnv(t)
		pinVersion(t, "1.0.0")
		backupBinaryForUpdateTest(t)

		fakeBody := []byte("fake stellar binary contents for successful-update test")
		sum := sha256.Sum256(fakeBody)
		hexSum := hex.EncodeToString(sum[:])

		mux := http.NewServeMux()
		mux.HandleFunc("/api/latest", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(releaseJSON("v1.1.0")))
		})
		mux.HandleFunc("/release/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprintf(w, "%s  %s\n", hexSum, binaryName)
		})
		mux.HandleFunc("/release/"+binaryName, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(fakeBody)
		})
		pointAtServer(t, mux)

		var execErr error
		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"update"})
			cmd.SetOut(new(bytes.Buffer))
			execErr = cmd.Execute()
		})
		require.NoError(t, execErr)
		assert.Contains(t, output, "Successfully updated to version v1.1.0")

		execPath, err := os.Executable()
		require.NoError(t, err)
		content, err := os.ReadFile(execPath)
		require.NoError(t, err)
		assert.Equal(t, fakeBody, content, "the file at os.Executable() should now hold the downloaded content")
	})
}

// =============================================================================
// Version Tests
// =============================================================================

// TestE2E_Version covers A3C-213 ("offline version ux ui"):
//   - `stellar version` and `stellar --version` must show the current
//     version block even when the GitHub update check fails or is
//     unreachable, rather than blocking on it or omitting it.
//   - a failed update check renders a short warning, never the raw network
//     error.
//   - dev builds, and every non-version command, must never call the
//     update-check endpoint at all (the eager-evaluation bug this ticket
//     fixes made every single command pay that network round trip).
func TestE2E_Version(t *testing.T) {
	releaseJSON := func(tag string) string {
		return fmt.Sprintf(
			`{"tag_name":%q,"name":%q,"published_at":"2024-01-01T00:00:00Z","html_url":"https://example.com/%s"}`,
			tag, tag, tag,
		)
	}

	// closedServerURL returns a URL that refuses connections immediately - a
	// fast, deterministic stand-in for "offline" that doesn't depend on
	// updateCheckTimeout actually expiring.
	closedServerURL := func(t *testing.T) string {
		t.Helper()
		server := httptest.NewServer(http.NewServeMux())
		url := server.URL
		server.Close()
		return url
	}

	// pointAt redirects LatestReleaseAPIURL (the var version.go exposes
	// specifically so tests can do this - see its doc comment) and restores
	// the real GitHub URL afterward.
	pointAt := func(t *testing.T, url string) {
		t.Helper()
		orig := LatestReleaseAPIURL
		LatestReleaseAPIURL = url
		t.Cleanup(func() {
			LatestReleaseAPIURL = orig
		})
	}

	// execOnRoot runs args against the package's singleton rootCmd, not a
	// fresh NewRootCmd() instance. The version-flag wiring SetVersionInfo
	// installs (rootCmd.Version, rootCmd.SetVersionTemplate) and the
	// "version" subcommand version.go's own init() registers both live on
	// that specific *cobra.Command - main.go's real Execute() path runs
	// through the very same singleton, so this is the faithful way to
	// exercise --version/`version`, not a shortcut. The pflag boolean behind
	// --version has no automatic per-run reset, so it's forced back to false
	// afterward to keep it from leaking true into a later subtest that
	// doesn't pass --version itself.
	execOnRoot := func(t *testing.T, args []string) (string, error) {
		t.Helper()
		var execErr error
		output := testutil.CaptureOutput(t, func() {
			rootCmd.SetArgs(args)
			rootCmd.SetOut(new(bytes.Buffer))
			execErr = rootCmd.Execute()
		})
		if f := rootCmd.Flags().Lookup("version"); f != nil {
			_ = f.Value.Set("false")
		}
		return output, execErr
	}

	t.Run("Offline: shows the version block and a short warning, not the raw error", func(t *testing.T) {
		_ = testutil.SetupTestEnv(t)
		pinVersion(t, "1.0.0")
		pointAt(t, closedServerURL(t))

		output, execErr := execOnRoot(t, []string{"version"})
		require.NoError(t, execErr)

		assert.Contains(t, output, "version: 1.0.0", "version block must be shown even when offline")
		assert.Contains(t, output, "Checking for updates...")
		assert.Contains(t, output, "couldn't fetch latest version, are you online?")
		assert.NotContains(t, output, "connection refused", "raw network error must not reach the user")
		assert.NotContains(t, output, "Failed to check for updates:", "raw error wrapper must not reach the user")
	})

	t.Run("--version behaves the same as the version subcommand", func(t *testing.T) {
		_ = testutil.SetupTestEnv(t)
		pinVersion(t, "1.0.0")
		pointAt(t, closedServerURL(t))

		output, execErr := execOnRoot(t, []string{"--version"})
		require.NoError(t, execErr)

		assert.Contains(t, output, "version: 1.0.0")
		assert.Contains(t, output, "Checking for updates...")
		assert.Contains(t, output, "couldn't fetch latest version, are you online?")
	})

	t.Run("Online and up to date shows the green success line", func(t *testing.T) {
		_ = testutil.SetupTestEnv(t)
		pinVersion(t, "1.2.3")

		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(releaseJSON("v1.2.3")))
		})
		server := httptest.NewServer(mux)
		t.Cleanup(server.Close)
		pointAt(t, server.URL)

		output, execErr := execOnRoot(t, []string{"version"})
		require.NoError(t, execErr)
		assert.Contains(t, output, "You have the latest version (v1.2.3)")
	})

	t.Run("Dev build skips the update check entirely and never touches the network", func(t *testing.T) {
		_ = testutil.SetupTestEnv(t)

		calls := 0
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			calls++
			_, _ = w.Write([]byte(releaseJSON("v9.9.9")))
		})
		server := httptest.NewServer(mux)
		t.Cleanup(server.Close)
		pointAt(t, server.URL)

		origVersion, origCommit, origDate := versionInfo.version, versionInfo.commit, versionInfo.date
		SetVersionInfo("dev", "none", "unknown")
		t.Cleanup(func() {
			SetVersionInfo(origVersion, origCommit, origDate)
		})

		output, execErr := execOnRoot(t, []string{"version"})
		require.NoError(t, execErr)

		assert.Contains(t, output, "version: dev")
		assert.NotContains(t, output, "Checking for updates...")
		assert.Equal(t, 0, calls, "dev builds must never call the update-check endpoint")
	})

	t.Run("Non-version commands never call the update-check endpoint", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		pinVersion(t, "1.0.0")

		// This is the regression this ticket's item (a) fixes: SetVersionInfo
		// (called from main on every invocation) used to eagerly evaluate
		// getFullVersionOutput(), which called this same GitHub endpoint on
		// every single command, offline timeout and all. Asserting calls == 0
		// after running ordinary commands would have caught that.
		calls := 0
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			calls++
			_, _ = w.Write([]byte(releaseJSON("v9.9.9")))
		})
		server := httptest.NewServer(mux)
		t.Cleanup(server.Close)
		pointAt(t, server.URL)

		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		for _, args := range [][]string{
			{"list"},
			{"current"},
			{"info", "testuser/sample-theme"},
		} {
			cmd := NewRootCmd()
			cmd.SetArgs(args)
			cmd.SetOut(new(bytes.Buffer))
			require.NoError(t, cmd.Execute(), "args: %v", args)
		}

		assert.Equal(t, 0, calls, "non-version commands must never hit the GitHub update-check endpoint")
	})
}

// =============================================================================
// Helper functions
// =============================================================================

// resetFlags resets all global command flags to their default values
func resetFlags() {
	forceApply = false
	updateTheme = false
	forceRemove = false
	cleanAll = false
	uninstallYes = false
	uninstallKeepConfig = false
}

// pinVersion sets versionInfo to a known non-dev version (IsDev() gates
// update-checking and telemetry on the version not being "dev") and restores
// the original values via t.Cleanup. versionInfo is an unexported
// package-level var in this same package, so the test can read/write it
// directly instead of needing an exported getter.
func pinVersion(t *testing.T, version string) {
	t.Helper()
	origVersion, origCommit, origDate := versionInfo.version, versionInfo.commit, versionInfo.date
	SetVersionInfo(version, "testcommit", "2024-01-01")
	t.Cleanup(func() {
		SetVersionInfo(origVersion, origCommit, origDate)
	})
}

// backupBinaryForUpdateTest saves the current test binary's bytes and mode
// (os.Executable() in a test process resolves to the compiled test binary)
// and restores them via t.Cleanup. It must be called by any test that lets
// "stellar update" actually replace os.Executable(), so the on-disk test
// binary isn't left holding fake content for later test runs.
func backupBinaryForUpdateTest(t *testing.T) {
	t.Helper()

	execPath, err := os.Executable()
	require.NoError(t, err)

	original, err := os.ReadFile(execPath)
	require.NoError(t, err)

	info, err := os.Stat(execPath)
	require.NoError(t, err)
	mode := info.Mode()

	t.Cleanup(func() {
		_ = os.WriteFile(execPath, original, mode)
		_ = os.Chmod(execPath, mode)
	})
}

// init ensures flags are reset at test start
func init() {
	resetFlags()
}

// The security warning for [custom] commands is the only thing standing
// between a user and shell code that runs on every prompt render, so the link
// it offers has to point at the exact version being installed - and has to be
// openable.
func TestE2E_ApplySecurityWarningReviewLink(t *testing.T) {
	t.Run("Links to the reviewed version on the configured hub", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()

		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		// Declining at the prompt keeps the assertion about the warning itself
		// rather than about applying, and proves the abort path still works.
		restoreStdin := replaceStdin(t, "n\n")
		defer restoreStdin()

		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"apply", "testuser/custom-theme"})
			cmd.SetOut(new(bytes.Buffer))
			_ = cmd.Execute()
		})

		assert.Contains(t, output, "SECURITY WARNING")

		// Built from the same base the API client used, so it follows
		// STELLAR_API_URL here instead of pointing at production.
		expectedURL := env.MockServer.URL + "/testuser/custom-theme?review=1.0"
		assert.Contains(t, output, expectedURL,
			"the review link must name the exact version being applied")

		// Declined, so nothing should have been written or linked.
		assert.False(t, env.FileExists(
			filepath.Join(env.StellarDir, "testuser", "custom-theme", "1.0.toml")),
			"a declined theme must not be cached")
	})

	t.Run("Does not warn for a theme without custom commands", func(t *testing.T) {
		testutil.RequireSymlinks(t)
		env := testutil.SetupTestEnv(t)
		resetFlags()

		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)

		output := testutil.CaptureOutput(t, func() {
			cmd := NewRootCmd()
			cmd.SetArgs([]string{"apply", "testuser/sample-theme@1.2"})
			cmd.SetOut(new(bytes.Buffer))
			_ = cmd.Execute()
		})

		assert.NotContains(t, output, "SECURITY WARNING")
		assert.NotContains(t, output, "?review=")
	})
}

// replaceStdin swaps os.Stdin for a pipe preloaded with input, so a test can
// answer promptConfirmation. The returned func restores the original.
func replaceStdin(t *testing.T, input string) func() {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)

	_, err = w.WriteString(input)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	orig := os.Stdin
	os.Stdin = r

	return func() {
		os.Stdin = orig
		_ = r.Close()
	}
}
