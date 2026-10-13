// E2E tests for `stellar new` and `stellar fork`.
package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/a3chron/stellar/internal/config"
	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_New(t *testing.T) {
	t.Run("creates a starter theme under the given author/name", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()

		out, err := runStellar(t, nil, "new", "me/my-theme")
		require.NoError(t, err)
		assert.Contains(t, out, "me/my-theme")

		path := filepath.Join(env.StellarDir, "me", "my-theme", "1.0.toml")
		assert.True(t, env.FileExists(path))

		cfg, cerr := config.Load()
		require.NoError(t, cerr)
		assert.True(t, cfg.HasAuthored("me/my-theme"))
	})

	t.Run("refuses to overwrite an existing theme", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		env.CreateThemeFile("me", "my-theme", "1.0", testutil.SampleTOML())

		_, err := runStellar(t, nil, "new", "me/my-theme")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
		// The existing file must survive untouched.
		assert.Equal(t, testutil.SampleTOML(), env.ReadFile(filepath.Join(env.StellarDir, "me", "my-theme", "1.0.toml")))
	})

	t.Run("without an author uses the cached username", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		env.CreateConfig(`{"username": "cacheduser"}`)

		out, err := runStellar(t, nil, "new", "solo-theme")
		require.NoError(t, err)
		assert.Contains(t, out, "cacheduser/solo-theme")
	})
}

func TestE2E_Fork(t *testing.T) {
	t.Run("forks a hub theme with a header and counts a download once", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		env.CreateConfig(`{"username": "me"}`)
		before := mockAPI.GetDownloadCount("testuser", "sample-theme")

		out, err := runStellar(t, nil, "fork", "testuser/sample-theme@1.2", "my-blue")
		require.NoError(t, err)
		assert.Contains(t, out, "me/my-blue")

		destPath := filepath.Join(env.StellarDir, "me", "my-blue", "1.0.toml")
		require.True(t, env.FileExists(destPath))
		content := env.ReadFile(destPath)
		assert.Contains(t, content, "# Forked from testuser/sample-theme@1.2")
		assert.Contains(t, content, "[character]")

		assert.Equal(t, before+1, mockAPI.GetDownloadCount("testuser", "sample-theme"))

		cfg, cerr := config.Load()
		require.NoError(t, cerr)
		assert.True(t, cfg.HasAuthored("me/my-blue"))
		assert.True(t, cfg.HasDownloaded("testuser/sample-theme"))
	})

	t.Run("does not double-count a download for an already-downloaded theme", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		env.CreateConfig(`{"username": "me", "downloaded_themes": ["testuser/sample-theme"]}`)
		env.CreateThemeFile("testuser", "sample-theme", "1.2", testutil.SampleTOML())
		before := mockAPI.GetDownloadCount("testuser", "sample-theme")

		_, err := runStellar(t, nil, "fork", "testuser/sample-theme@1.2", "my-blue")
		require.NoError(t, err)

		assert.Equal(t, before, mockAPI.GetDownloadCount("testuser", "sample-theme"), "a cached theme must not be fetched or counted again")
	})

	t.Run("refuses to overwrite an existing destination", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		env.CreateConfig(`{"username": "me"}`)
		env.CreateThemeFile("me", "my-blue", "1.0", testutil.SampleTOML())

		_, err := runStellar(t, nil, "fork", "testuser/sample-theme@1.2", "my-blue")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})
}

// TestE2E_AuthoredThemesSurviveClean guards the config.AuthoredThemes
// protection: `stellar new`/`stellar fork` output must survive `stellar
// clean --all` the same way <author>/backup already does, since there's no
// hub copy to fall back to for a theme that was never published.
func TestE2E_AuthoredThemesSurviveClean(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	_, err := runStellar(t, nil, "new", "me/my-theme")
	require.NoError(t, err)
	authoredPath := filepath.Join(env.StellarDir, "me", "my-theme", "1.0.toml")
	require.True(t, env.FileExists(authoredPath))

	// An ordinary downloaded/cached theme, to confirm clean --all still
	// removes what it's supposed to.
	ordinaryPath := env.CreateThemeFile("someone", "else-theme", "1.0", testutil.SampleTOML())

	out, err := runStellar(t, nil, "clean", "--all")
	require.NoError(t, err)
	assert.Contains(t, out, "Cleaned cache")

	assert.True(t, env.FileExists(authoredPath), "an authored (not yet published) theme must survive clean --all")
	assert.False(t, env.FileExists(ordinaryPath), "an ordinary cached theme must still be removed by clean --all")
}

func TestE2E_ForkOfflineFallsBackToCache(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()
	_ = os.Setenv("STELLAR_API_URL", "http://127.0.0.1:1")
	t.Cleanup(func() { _ = os.Unsetenv("STELLAR_API_URL") })
	env.CreateConfig(`{"username": "me"}`)
	env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())

	out, err := runStellar(t, nil, "fork", "alice/rainbow@1.0", "my-rainbow")
	require.NoError(t, err)
	assert.Contains(t, out, "me/my-rainbow")

	destPath := filepath.Join(env.StellarDir, "me", "my-rainbow", "1.0.toml")
	assert.True(t, env.FileExists(destPath))
}
