package symlink

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/paths"
	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyTheme_NoExistingFile(t *testing.T) {
	testutil.RequireSymlinks(t)
	env := testutil.SetupTestEnv(t)

	themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

	info, err := ApplyTheme(themePath, &config.Config{})
	require.NoError(t, err)
	assert.Nil(t, info, "should not create backup when no existing file")

	assert.True(t, env.IsSymlink(env.StarshipPath))
	assert.Equal(t, themePath, env.ReadSymlink(env.StarshipPath))
}

func TestApplyTheme_ExistingRegularFile(t *testing.T) {
	testutil.RequireSymlinks(t)
	env := testutil.SetupTestEnv(t)

	originalContent := "# Original config\n[character]\nsuccess_symbol = \"OLD\""
	env.CreateStarshipConfig(originalContent)

	themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

	info, err := ApplyTheme(themePath, &config.Config{})
	require.NoError(t, err)

	require.NotNil(t, info)
	assert.True(t, env.FileExists(info.Path))
	assert.Equal(t, originalContent, env.ReadFile(info.Path))

	expectedBackupPath := filepath.Join(env.StellarDir, BackupAuthor(), "backup", "1.0.toml")
	assert.Equal(t, expectedBackupPath, info.Path)
	assert.Equal(t, BackupAuthor()+"/backup@1.0", info.Identifier)

	assert.True(t, env.IsSymlink(env.StarshipPath))
	assert.Equal(t, themePath, env.ReadSymlink(env.StarshipPath))
}

func TestApplyTheme_ExistingSymlink(t *testing.T) {
	testutil.RequireSymlinks(t)
	env := testutil.SetupTestEnv(t)

	themePath1 := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	_, err := ApplyTheme(themePath1, &config.Config{})
	require.NoError(t, err)

	themePath2 := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())
	info, err := ApplyTheme(themePath2, &config.Config{})
	require.NoError(t, err)

	assert.Nil(t, info, "should not create backup when replacing symlink")

	assert.True(t, env.IsSymlink(env.StarshipPath))
	assert.Equal(t, themePath2, env.ReadSymlink(env.StarshipPath))
}

func TestApplyTheme_CopyMode(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	t.Setenv(paths.EnvApplyMode, "copy")

	assert.True(t, IsCopyMode())

	themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

	info, err := ApplyTheme(themePath, &config.Config{})
	require.NoError(t, err)
	assert.Nil(t, info, "should not create backup when no existing file")

	// In copy mode starship.toml is a regular file, not a symlink
	assert.True(t, env.FileExists(env.StarshipPath))
	assert.False(t, env.IsSymlink(env.StarshipPath))
	assert.Equal(t, testutil.SampleTOML(), env.ReadFile(env.StarshipPath))
}

func TestApplyTheme_CopyMode_ExistingRegularFile(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	t.Setenv(paths.EnvApplyMode, "copy")

	originalContent := "# Original config\n[character]\nsuccess_symbol = \"OLD\""
	env.CreateStarshipConfig(originalContent)

	themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

	info, err := ApplyTheme(themePath, &config.Config{})
	require.NoError(t, err)

	// Original is backed up before being overwritten by the copy
	require.NotNil(t, info)
	assert.True(t, env.FileExists(info.Path))
	assert.Equal(t, originalContent, env.ReadFile(info.Path))

	assert.False(t, env.IsSymlink(env.StarshipPath))
	assert.Equal(t, testutil.SampleTOML(), env.ReadFile(env.StarshipPath))
}

func TestApplyTheme_CopyMode_ReplacesPreviousCopy(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	t.Setenv(paths.EnvApplyMode, "copy")

	themePath1 := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	_, err := ApplyTheme(themePath1, &config.Config{})
	require.NoError(t, err)

	// Simulate the config state that cmd/apply.go would have saved after the
	// first apply, so the second apply knows the current file is stellar's copy.
	env.CreateConfig(`{"current_theme":"alice/rainbow@1.0","current_path":"` + themePath1 + `"}`)
	cfg, err := config.Load()
	require.NoError(t, err)

	themePath2 := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOMLWithCustom())
	info, err := ApplyTheme(themePath2, cfg)
	require.NoError(t, err)

	// A stellar-managed copy should not be backed up when replaced
	assert.Nil(t, info, "should not back up a config stellar already manages")
	assert.False(t, env.IsSymlink(env.StarshipPath))
	assert.Equal(t, testutil.SampleTOMLWithCustom(), env.ReadFile(env.StarshipPath))
}

// TestApplyTheme_CopyMode_BacksUpHandEditedConfig verifies that copy mode
// detects a hand-edited starship.toml: even though config.json says a theme is
// applied, the file no longer matches the cached theme (nor the recorded
// applied hash, since this fabricated config predates that field), so it must
// be backed up instead of being treated as stellar's own copy.
func TestApplyTheme_CopyMode_BacksUpHandEditedConfig(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	t.Setenv(paths.EnvApplyMode, "copy")

	themePath1 := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	_, err := ApplyTheme(themePath1, &config.Config{})
	require.NoError(t, err)
	env.CreateConfig(`{"current_theme":"alice/rainbow@1.0","current_path":"` + themePath1 + `"}`)
	cfg, err := config.Load()
	require.NoError(t, err)

	// The user edits the applied copy directly.
	editedContent := "# MY HAND-EDITED CONFIG"
	env.CreateStarshipConfig(editedContent)

	themePath2 := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOMLWithCustom())
	info, err := ApplyTheme(themePath2, cfg)
	require.NoError(t, err)

	expectedBackup := filepath.Join(env.StellarDir, BackupAuthor(), "backup", "1.0.toml")
	require.NotNil(t, info, "hand-edited config should be backed up")
	assert.Equal(t, expectedBackup, info.Path)
	assert.Equal(t, editedContent, env.ReadFile(info.Path))
	assert.Equal(t, testutil.SampleTOMLWithCustom(), env.ReadFile(env.StarshipPath))
}

// TestApplyTheme_CopyMode_MissingCachedThemeBacksUp covers the fallback when the
// cached theme referenced by config.json is gone (e.g. removed via
// "stellar clean --all"): the file can't be verified as stellar's copy via the
// legacy CurrentPath comparison, and there is no applied_hash recorded either
// (this fabricated config predates that field), so it is backed up rather than
// risking data loss.
func TestApplyTheme_CopyMode_MissingCachedThemeBacksUp(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	t.Setenv(paths.EnvApplyMode, "copy")

	// config.json points at a cached theme file that no longer exists.
	env.CreateConfig(`{"current_theme":"alice/rainbow@1.0","current_path":"` + env.StellarDir + `/alice/rainbow/1.0.toml"}`)
	env.CreateStarshipConfig("# unverifiable stellar copy")
	cfg, err := config.Load()
	require.NoError(t, err)

	themePath := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())
	info, err := ApplyTheme(themePath, cfg)
	require.NoError(t, err)

	expectedBackup := filepath.Join(env.StellarDir, BackupAuthor(), "backup", "1.0.toml")
	require.NotNil(t, info, "unverifiable config should be backed up")
	assert.Equal(t, expectedBackup, info.Path)
	assert.Equal(t, "# unverifiable stellar copy", env.ReadFile(info.Path))
}

// TestApplyTheme_CopyMode_AppliedHashRecognizesCopy is the hash-present
// counterpart to TestApplyTheme_CopyMode_MissingCachedThemeBacksUp: when
// applied_hash is recorded and still matches what's on disk, stellar
// recognizes its own copy and skips the backup even though the cached theme
// file it originally came from is gone.
func TestApplyTheme_CopyMode_AppliedHashRecognizesCopy(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	t.Setenv(paths.EnvApplyMode, "copy")

	appliedContent := "# stellar copy, cached source now missing"
	env.CreateStarshipConfig(appliedContent)
	hash, err := HashFile(env.StarshipPath)
	require.NoError(t, err)

	// config.json points at a cached theme file that no longer exists, but
	// applied_hash matches the current starship.toml content.
	env.CreateConfig(`{"current_theme":"alice/rainbow@1.0","current_path":"` + env.StellarDir + `/alice/rainbow/1.0.toml","applied_hash":"` + hash + `"}`)
	cfg, err := config.Load()
	require.NoError(t, err)

	themePath := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())
	info, err := ApplyTheme(themePath, cfg)
	require.NoError(t, err)

	assert.Nil(t, info, "applied_hash match should prevent a junk backup")
}

// TestApplyTheme_CopyMode_VersionedBackupPreservesOriginal covers the case where
// stellar's config state is lost (CurrentTheme == "") but the current
// starship.toml is actually a stellar copy. Rather than skipping the backup (and
// risking silent data loss on a genuinely new original), stellar writes the file
// as the next backup version. The genuine original at 1.0.toml is never touched:
// the unrecognized file gets 2.0.toml instead.
func TestApplyTheme_CopyMode_VersionedBackupPreservesOriginal(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	t.Setenv(paths.EnvApplyMode, "copy")

	// A real original backup already exists from an earlier apply.
	origBackupPath := filepath.Join(env.StellarDir, BackupAuthor(), "backup", "1.0.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(origBackupPath), 0755))
	require.NoError(t, os.WriteFile(origBackupPath, []byte("# PRECIOUS ORIGINAL"), 0644))

	// starship.toml holds a stellar copy, but there is no config.json state
	// (CurrentTheme == ""), so the config-based marker can't help here.
	env.CreateStarshipConfig("# stellar copy of some theme")

	themePath := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())
	newInfo, err := ApplyTheme(themePath, &config.Config{})
	require.NoError(t, err)

	// The unrecognized file is preserved as the next version (2.0.toml)...
	require.NotNil(t, newInfo, "new backup should use the next version")
	expectedNewBackup := filepath.Join(env.StellarDir, BackupAuthor(), "backup", "2.0.toml")
	assert.Equal(t, expectedNewBackup, newInfo.Path)
	assert.Equal(t, "# stellar copy of some theme", env.ReadFile(newInfo.Path))

	// ...and the genuine original at 1.0.toml is never clobbered.
	assert.Equal(t, "# PRECIOUS ORIGINAL", env.ReadFile(origBackupPath), "original backup must be preserved")
	assert.Equal(t, testutil.SampleTOML(), env.ReadFile(env.StarshipPath))
}

// TestApplyTheme_VersionedBackup verifies that each unmanaged original starship
// config is preserved under its own version instead of overwriting an earlier
// backup.
func TestApplyTheme_VersionedBackup(t *testing.T) {
	testutil.RequireSymlinks(t)
	env := testutil.SetupTestEnv(t)

	// First original: backed up as 1.0.toml.
	firstOriginal := "# FIRST ORIGINAL"
	env.CreateStarshipConfig(firstOriginal)

	themePath1 := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	firstInfo, err := ApplyTheme(themePath1, &config.Config{})
	require.NoError(t, err)

	require.NotNil(t, firstInfo)
	expectedFirstBackup := filepath.Join(env.StellarDir, BackupAuthor(), "backup", "1.0.toml")
	assert.Equal(t, expectedFirstBackup, firstInfo.Path)
	assert.Equal(t, firstOriginal, env.ReadFile(firstInfo.Path))

	// The user later replaces starship.toml with a new hand-written config
	// (a regular file, not the symlink stellar just created).
	secondOriginal := "# SECOND ORIGINAL"
	require.NoError(t, os.Remove(env.StarshipPath))
	env.CreateStarshipConfig(secondOriginal)

	themePath2 := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())
	secondInfo, err := ApplyTheme(themePath2, &config.Config{})
	require.NoError(t, err)

	// The second original is preserved as 2.0.toml, and 1.0.toml is unchanged.
	require.NotNil(t, secondInfo)
	expectedSecondBackup := filepath.Join(env.StellarDir, BackupAuthor(), "backup", "2.0.toml")
	assert.Equal(t, expectedSecondBackup, secondInfo.Path)
	assert.Equal(t, secondOriginal, env.ReadFile(secondInfo.Path))
	assert.Equal(t, firstOriginal, env.ReadFile(firstInfo.Path), "earlier backup must stay intact")
}

func TestSanitizeBackupAuthor(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"windows domain prefix", `DESKTOP-ABC\kurt`, "kurt"},
		{"plain username", "kurt", "kurt"},
		{"spaces replaced", "John Doe", "John-Doe"},
		{"domain and spaces", `domain\John Doe`, "John-Doe"},
		{"empty falls back to local", "", "local"},
		{"only separator falls back to local", `\`, "local"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sanitizeBackupAuthor(tt.input))
		})
	}
}

// TestBackupInfo_Identifier verifies that BackupInfo.Identifier is built from
// the same author/version values used to construct BackupInfo.Path (rather
// than re-derived by parsing the path afterward), and that it always targets
// the backup that was actually just written.
func TestBackupInfo_Identifier(t *testing.T) {
	testutil.RequireSymlinks(t)
	env := testutil.SetupTestEnv(t)

	// First unmanaged config: backed up as version 1.0.
	env.CreateStarshipConfig("# FIRST ORIGINAL")
	themePath1 := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	firstInfo, err := ApplyTheme(themePath1, &config.Config{})
	require.NoError(t, err)

	require.NotNil(t, firstInfo)
	assert.Equal(t, BackupAuthor()+"/backup@1.0", firstInfo.Identifier)

	// A second, later unmanaged config gets the next version (2.0), and its
	// identifier tracks that version rather than staying pinned at 1.0.
	require.NoError(t, os.Remove(env.StarshipPath))
	env.CreateStarshipConfig("# SECOND ORIGINAL")
	themePath2 := env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())
	secondInfo, err := ApplyTheme(themePath2, &config.Config{})
	require.NoError(t, err)

	require.NotNil(t, secondInfo)
	assert.Equal(t, BackupAuthor()+"/backup@2.0", secondInfo.Identifier)

	// The identifier must parse back into a theme that targets the file
	// actually written to disk.
	parsedVersion := strings.TrimSuffix(filepath.Base(secondInfo.Path), ".toml")
	assert.True(t, strings.HasSuffix(secondInfo.Identifier, "/backup@"+parsedVersion))
}

func TestGetCurrentTarget(t *testing.T) {
	testutil.RequireSymlinks(t)
	env := testutil.SetupTestEnv(t)

	themePath := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	_, err := ApplyTheme(themePath, &config.Config{})
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
	testutil.RequireSymlinks(t)
	env := testutil.SetupTestEnv(t)

	regularPath := filepath.Join(env.RootDir, "regular.txt")
	require.NoError(t, os.WriteFile(regularPath, []byte("test"), 0644))

	symlinkPath := filepath.Join(env.RootDir, "symlink.txt")
	require.NoError(t, os.Symlink(regularPath, symlinkPath))

	assert.False(t, isSymlink(regularPath))
	assert.True(t, isSymlink(symlinkPath))
	assert.False(t, isSymlink("/nonexistent/path"))
}

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.toml")
	require.NoError(t, os.WriteFile(path, []byte(testutil.SampleTOML()), 0644))

	hash1, err := HashFile(path)
	require.NoError(t, err)
	assert.NotEmpty(t, hash1)

	// Same content hashes the same.
	path2 := filepath.Join(dir, "sample-copy.toml")
	require.NoError(t, os.WriteFile(path2, []byte(testutil.SampleTOML()), 0644))
	hash2, err := HashFile(path2)
	require.NoError(t, err)
	assert.Equal(t, hash1, hash2)

	// Different content hashes differently.
	path3 := filepath.Join(dir, "other.toml")
	require.NoError(t, os.WriteFile(path3, []byte(testutil.SampleTOMLWithCustom()), 0644))
	hash3, err := HashFile(path3)
	require.NoError(t, err)
	assert.NotEqual(t, hash1, hash3)

	_, err = HashFile(filepath.Join(dir, "missing.toml"))
	assert.Error(t, err)
}
