package symlink

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/a3chron/stellar/internal/paths"
	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyTheme_NoExistingFile(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

	backupPath, err := ApplyTheme(themePath)
	require.NoError(t, err)
	assert.Empty(t, backupPath, "should not create backup when no existing file")

	assert.True(t, env.IsSymlink(env.StarshipPath))
	assert.Equal(t, themePath, env.ReadSymlink(env.StarshipPath))
}

func TestApplyTheme_ExistingRegularFile(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	originalContent := "# Original config\n[character]\nsuccess_symbol = \"OLD\""
	env.CreateStarshipConfig(originalContent)

	themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

	backupPath, err := ApplyTheme(themePath)
	require.NoError(t, err)

	assert.NotEmpty(t, backupPath)
	assert.True(t, env.FileExists(backupPath))
	assert.Equal(t, originalContent, env.ReadFile(backupPath))

	currentUser, err := user.Current()
	require.NoError(t, err)
	expectedBackupPath := filepath.Join(env.StellarDir, currentUser.Username, "backup", "1.0.toml")
	assert.Equal(t, expectedBackupPath, backupPath)

	assert.True(t, env.IsSymlink(env.StarshipPath))
	assert.Equal(t, themePath, env.ReadSymlink(env.StarshipPath))
}

func TestApplyTheme_ExistingSymlink(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	themePath1 := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	_, err := ApplyTheme(themePath1)
	require.NoError(t, err)

	themePath2 := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())
	backupPath, err := ApplyTheme(themePath2)
	require.NoError(t, err)

	assert.Empty(t, backupPath, "should not create backup when replacing symlink")

	assert.True(t, env.IsSymlink(env.StarshipPath))
	assert.Equal(t, themePath2, env.ReadSymlink(env.StarshipPath))
}

func TestApplyTheme_CopyMode(t *testing.T) {
	t.Setenv(paths.EnvApplyMode, "copy")
	env := testutil.SetupTestEnv(t)

	assert.True(t, IsCopyMode())

	themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

	backupPath, err := ApplyTheme(themePath)
	require.NoError(t, err)
	assert.Empty(t, backupPath, "should not create backup when no existing file")

	// In copy mode starship.toml is a regular file, not a symlink
	assert.True(t, env.FileExists(env.StarshipPath))
	assert.False(t, env.IsSymlink(env.StarshipPath))
	assert.Equal(t, testutil.SampleTOML(), env.ReadFile(env.StarshipPath))
}

func TestApplyTheme_CopyMode_ExistingRegularFile(t *testing.T) {
	t.Setenv(paths.EnvApplyMode, "copy")
	env := testutil.SetupTestEnv(t)

	originalContent := "# Original config\n[character]\nsuccess_symbol = \"OLD\""
	env.CreateStarshipConfig(originalContent)

	themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

	backupPath, err := ApplyTheme(themePath)
	require.NoError(t, err)

	// Original is backed up before being overwritten by the copy
	assert.NotEmpty(t, backupPath)
	assert.True(t, env.FileExists(backupPath))
	assert.Equal(t, originalContent, env.ReadFile(backupPath))

	assert.False(t, env.IsSymlink(env.StarshipPath))
	assert.Equal(t, testutil.SampleTOML(), env.ReadFile(env.StarshipPath))
}

func TestApplyTheme_CopyMode_ReplacesPreviousCopy(t *testing.T) {
	t.Setenv(paths.EnvApplyMode, "copy")
	env := testutil.SetupTestEnv(t)

	themePath1 := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	_, err := ApplyTheme(themePath1)
	require.NoError(t, err)

	// Simulate the config state that cmd/apply.go would have saved after the
	// first apply, so the second apply knows the current file is stellar's copy.
	env.CreateConfig(`{"current_theme":"alice/rainbow@1.0","current_path":"` + themePath1 + `"}`)

	themePath2 := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOMLWithCustom())
	backupPath, err := ApplyTheme(themePath2)
	require.NoError(t, err)

	// A stellar-managed copy should not be backed up when replaced
	assert.Empty(t, backupPath, "should not back up a config stellar already manages")
	assert.False(t, env.IsSymlink(env.StarshipPath))
	assert.Equal(t, testutil.SampleTOMLWithCustom(), env.ReadFile(env.StarshipPath))
}

// TestApplyTheme_CopyMode_DoesNotClobberExistingBackup guards the safety net for
// the case where stellar's config state is lost (CurrentTheme == "") but the
// current starship.toml is actually a stellar copy. Without the guard, stellar
// would mistake its own copy for the user's original and overwrite the genuine
// backup.
func TestApplyTheme_CopyMode_DoesNotClobberExistingBackup(t *testing.T) {
	t.Setenv(paths.EnvApplyMode, "copy")
	env := testutil.SetupTestEnv(t)

	// A real original backup already exists from an earlier apply.
	currentUser, err := user.Current()
	require.NoError(t, err)
	backupPath := filepath.Join(env.StellarDir, currentUser.Username, "backup", "1.0.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(backupPath), 0755))
	require.NoError(t, os.WriteFile(backupPath, []byte("# PRECIOUS ORIGINAL"), 0644))

	// starship.toml holds a stellar copy, but there is no config.json state
	// (CurrentTheme == ""), so the config-based marker can't help here.
	env.CreateStarshipConfig("# stellar copy of some theme")

	themePath := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())
	newBackup, err := ApplyTheme(themePath)
	require.NoError(t, err)

	assert.Empty(t, newBackup, "should not create a new backup when one already exists")
	assert.Equal(t, "# PRECIOUS ORIGINAL", env.ReadFile(backupPath), "existing backup must be preserved")
	assert.Equal(t, testutil.SampleTOML(), env.ReadFile(env.StarshipPath))
}

func TestGetCurrentTarget(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	_, err := ApplyTheme(themePath)
	require.NoError(t, err)

	target, err := GetCurrentTarget()
	require.NoError(t, err)
	assert.Equal(t, themePath, target)
}

func TestGetCurrentTarget_NoSymlink(t *testing.T) {
	_ = testutil.SetupTestEnv(t)

	_, err := GetCurrentTarget()
	assert.Error(t, err)
}

func TestIsCopyMode(t *testing.T) {
	t.Run("forced copy", func(t *testing.T) {
		t.Setenv(paths.EnvApplyMode, "copy")
		assert.True(t, IsCopyMode())
	})

	t.Run("forced symlink", func(t *testing.T) {
		t.Setenv(paths.EnvApplyMode, "symlink")
		assert.False(t, IsCopyMode())
	})
}

func TestIsSymlink(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	regularPath := filepath.Join(env.RootDir, "regular.txt")
	require.NoError(t, os.WriteFile(regularPath, []byte("test"), 0644))

	symlinkPath := filepath.Join(env.RootDir, "symlink.txt")
	require.NoError(t, os.Symlink(regularPath, symlinkPath))

	assert.False(t, isSymlink(regularPath))
	assert.True(t, isSymlink(symlinkPath))
	assert.False(t, isSymlink("/nonexistent/path"))
}
