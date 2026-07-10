// Package cmd contains E2E tests for stellar CLI commands.
// These tests cover complete user workflows and are the primary test suite.
package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/a3chron/stellar/internal/paths"
	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Apply Tests
// =============================================================================

func TestE2E_Apply(t *testing.T) {
	t.Run("Download and apply remote theme", func(t *testing.T) {
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
		t.Setenv(paths.EnvApplyMode, "copy")
		env := testutil.SetupTestEnv(t)
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
		t.Setenv(paths.EnvApplyMode, "copy")
		env := testutil.SetupTestEnv(t)

		themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
		// Copy mode: starship.toml is a regular file, not a symlink
		env.CreateStarshipConfig(testutil.SampleTOML())

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

	t.Run("Missing starship.toml in copy mode", func(t *testing.T) {
		t.Setenv(paths.EnvApplyMode, "copy")
		env := testutil.SetupTestEnv(t)

		// No starship.toml on disk, but config claims a theme is applied
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

	t.Run("Broken symlink", func(t *testing.T) {
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
		t.Setenv(paths.EnvApplyMode, "copy")
		env := testutil.SetupTestEnv(t)

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

	t.Run("Double rollback returns to original", func(t *testing.T) {
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
