package symlink

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateSymlink_NoExistingFile(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

	backupPath, err := CreateSymlink(themePath)
	require.NoError(t, err)
	assert.Empty(t, backupPath, "should not create backup when no existing file")

	assert.True(t, env.IsSymlink(env.StarshipPath))
	assert.Equal(t, themePath, env.ReadSymlink(env.StarshipPath))
}

func TestCreateSymlink_ExistingRegularFile(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	originalContent := "# Original config\n[character]\nsuccess_symbol = \"OLD\""
	env.CreateStarshipConfig(originalContent)

	themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

	backupPath, err := CreateSymlink(themePath)
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

func TestCreateSymlink_ExistingSymlink(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	themePath1 := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	_, err := CreateSymlink(themePath1)
	require.NoError(t, err)

	themePath2 := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())
	backupPath, err := CreateSymlink(themePath2)
	require.NoError(t, err)

	assert.Empty(t, backupPath, "should not create backup when replacing symlink")

	assert.True(t, env.IsSymlink(env.StarshipPath))
	assert.Equal(t, themePath2, env.ReadSymlink(env.StarshipPath))
}

func TestGetCurrentTarget(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	_, err := CreateSymlink(themePath)
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
