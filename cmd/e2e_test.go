// Package cmd contains E2E tests for stellar CLI commands.
// These tests cover complete user workflows and are the primary test suite.
package cmd

import (
	"bytes"
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
		assert.Contains(t, configContent, themePath)
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

		previousPath := env.StellarDir + "/testuser/sample-theme/1.2.toml"

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
// Helper functions
// =============================================================================

// resetFlags resets all global command flags to their default values
func resetFlags() {
	forceApply = false
	updateTheme = false
	forceRemove = false
	cleanAll = false
}

// init ensures flags are reset at test start
func init() {
	resetFlags()
}
