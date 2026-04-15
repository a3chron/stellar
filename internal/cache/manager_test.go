package cache

import (
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
