package cache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/a3chron/stellar/internal/testutil"
	"github.com/a3chron/stellar/internal/theme"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveTheme(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	th := &theme.Theme{
		Author:  "testuser",
		Name:    "mytheme",
		Version: "1.0",
	}

	content := testutil.SampleTOML()
	err := SaveTheme(th, content)
	require.NoError(t, err)

	expectedPath := filepath.Join(env.StellarDir, "testuser", "mytheme", "1.0.toml")
	assert.True(t, env.FileExists(expectedPath))
	assert.Equal(t, content, env.ReadFile(expectedPath))
}

func TestThemeExists(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	th := &theme.Theme{
		Author:  "testuser",
		Name:    "mytheme",
		Version: "1.0",
	}

	assert.False(t, ThemeExists(th))

	env.CreateThemeFile("testuser", "mytheme", "1.0", testutil.SampleTOML())

	assert.True(t, ThemeExists(th))
}

func TestListCachedThemes(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	env.CreateThemeFile("alice", "rainbow", "1.5", testutil.SampleTOML())
	env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())

	themes, err := ListCachedThemes()
	require.NoError(t, err)

	assert.Len(t, themes, 3)
	assert.Contains(t, themes, "alice/rainbow@1.0")
	assert.Contains(t, themes, "alice/rainbow@1.5")
	assert.Contains(t, themes, "bob/sunset@2.0")
}

func TestListCachedThemes_IgnoresConfigJSON(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.CreateConfig(`{"current_theme": ""}`)
	env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

	themes, err := ListCachedThemes()
	require.NoError(t, err)

	assert.Len(t, themes, 1)
	assert.Equal(t, "alice/rainbow@1.0", themes[0])
}

func TestListAuthors(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	env.CreateThemeFile("bob", "sunset", "2.0", testutil.SampleTOML())
	env.CreateConfig(`{"current_theme": ""}`)

	authors, err := ListAuthors()
	require.NoError(t, err)

	assert.Equal(t, []string{"alice", "bob"}, authors)
}

func TestListAuthors_MissingDir(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	require.NoError(t, os.RemoveAll(env.StellarDir))

	authors, err := ListAuthors()
	require.NoError(t, err)
	assert.Nil(t, authors)
}

func TestListAuthorThemes(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	env.CreateThemeFile("alice", "sunset", "1.0", testutil.SampleTOML())
	env.CreateThemeFile("bob", "other", "1.0", testutil.SampleTOML())

	themes, err := ListAuthorThemes("alice")
	require.NoError(t, err)
	assert.Equal(t, []string{"rainbow", "sunset"}, themes)
}

func TestListAuthorThemes_MissingAuthor(t *testing.T) {
	testutil.SetupTestEnv(t)

	themes, err := ListAuthorThemes("nobody")
	require.NoError(t, err)
	assert.Nil(t, themes)
}

func TestListThemeVersions(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	env.CreateThemeFile("alice", "rainbow", "1.5", testutil.SampleTOML())
	env.CreateThemeFile("alice", "rainbow", "1.10", testutil.SampleTOML())

	versions, err := ListThemeVersions("alice", "rainbow")
	require.NoError(t, err)
	assert.Equal(t, []string{"1.10", "1.5", "1.0"}, versions)
}

func TestListThemeVersions_MissingTheme(t *testing.T) {
	testutil.SetupTestEnv(t)

	versions, err := ListThemeVersions("alice", "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, versions)
}

func TestTmpCacheFunctions(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	th := &theme.Theme{
		Author:  "testuser",
		Name:    "mytheme",
		Version: "1.0",
	}

	assert.False(t, TmpThemeExists(th))

	content := testutil.SampleTOML()
	err := SaveThemeToTmp(th, content)
	require.NoError(t, err)

	assert.True(t, TmpThemeExists(th))

	expectedPath := filepath.Join(env.TmpDir, "testuser", "mytheme", "1.0.toml")
	assert.Equal(t, expectedPath, TmpCachePath(th))
	assert.Equal(t, content, env.ReadFile(expectedPath))
}
