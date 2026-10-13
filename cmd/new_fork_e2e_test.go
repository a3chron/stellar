// E2E tests for `stellar new` and `stellar fork`.
package cmd

import (
	"os"
	"path/filepath"
	"strings"
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
		pinVersion(t, "1.2.3") // IncrementDownloadCount is skipped for dev builds, like apply
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

	t.Run("a dev build does not increment the hub's download count", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags() // dev build: versionInfo.version defaults to "dev", so IsDev() is true
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		env.CreateConfig(`{"username": "me"}`)
		before := mockAPI.GetDownloadCount("testuser", "sample-theme")

		_, err := runStellar(t, nil, "fork", "testuser/sample-theme@1.2", "my-blue-dev")
		require.NoError(t, err)

		assert.Equal(t, before, mockAPI.GetDownloadCount("testuser", "sample-theme"), "a dev build must not inflate the public download count")

		cfg, cerr := config.Load()
		require.NoError(t, cerr)
		assert.True(t, cfg.HasDownloaded("testuser/sample-theme"), "local bookkeeping still happens regardless of IsDev")
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
// protection: `stellar new`/`stellar fork` output must survive every
// `stellar clean` variant that doesn't pass --include-own, since there's no
// hub copy to fall back to for a theme that was never published - the same
// as a hand-made theme.
func TestE2E_AuthoredThemesSurviveClean(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	_, err := runStellar(t, nil, "new", "me/my-theme")
	require.NoError(t, err)
	authoredPath := filepath.Join(env.StellarDir, "me", "my-theme", "1.0.toml")
	require.True(t, env.FileExists(authoredPath))

	// An ordinary downloaded theme, to confirm a plain clean --all still
	// removes what it's supposed to (downloads, not own/authored themes).
	ordinaryPath := env.CreateThemeFile("someone", "else-theme", "1.0", testutil.SampleTOML())
	cfg, cerr := config.Load()
	require.NoError(t, cerr)
	cfg.MarkDownloaded("someone/else-theme")
	require.NoError(t, cfg.Save())

	out, err := runStellar(t, nil, "clean", "--all")
	require.NoError(t, err)
	assert.Contains(t, out, "Removed 1 downloaded theme(s)")

	assert.True(t, env.FileExists(authoredPath), "an authored (not yet published) theme must survive a plain clean --all")
	assert.False(t, env.FileExists(ordinaryPath), "a downloaded theme must still be removed by clean --all")

	// --include-own is the only thing that removes it.
	out, err = runStellar(t, nil, "clean", "--all", "--include-own", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Removed 1 of your own theme(s)")
	assert.False(t, env.FileExists(authoredPath), "--include-own removes authored themes too")
}

// TestE2E_PublishedThenRedownloadedThemeStaysOwn guards the specific case
// config.Config.AuthoredThemes exists for: a theme that's in BOTH
// DownloadedThemes (you fetched it, e.g. on another machine) and
// AuthoredThemes (you published it yourself) must still count as "own", not
// a plain download - a plain `stellar clean` must never sweep it away just
// because it also happens to be downloaded.
func TestE2E_PublishedThenRedownloadedThemeStaysOwn(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()

	path := env.CreateThemeFile("me", "my-theme", "1.0", testutil.SampleTOML())
	env.CreateConfig(`{"downloaded_themes": ["me/my-theme"], "authored_themes": ["me/my-theme"]}`)

	out, err := runStellar(t, nil, "clean")
	require.NoError(t, err)
	assert.Contains(t, out, "Nothing to clean")
	assert.Contains(t, out, "Kept 1 of your own themes")
	assert.True(t, env.FileExists(path))
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

// A theme folder that already exists - even without a 1.0.toml - is someone's
// theme: neither command may add a file to it, let alone replace one.
func TestE2E_NewAndForkLeaveExistingThemeFoldersAlone(t *testing.T) {
	own := testutil.SampleTOML() + "\n# my own work\n"

	t.Run("new", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		env.CreateThemeFile("me", "my-theme", "2.0", own)

		_, err := runStellar(t, nil, "new", "me/my-theme")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")

		dir := filepath.Join(env.StellarDir, "me", "my-theme")
		assert.False(t, env.FileExists(filepath.Join(dir, "1.0.toml")))
		assert.Equal(t, own, env.ReadFile(filepath.Join(dir, "2.0.toml")))
	})

	t.Run("fork", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		env.CreateConfig(`{"username": "me"}`)
		env.CreateThemeFile("me", "my-blue", "2.0", own)
		before := mockAPI.GetDownloadCount("testuser", "sample-theme")

		_, err := runStellar(t, nil, "fork", "testuser/sample-theme@1.2", "my-blue")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")

		dir := filepath.Join(env.StellarDir, "me", "my-blue")
		assert.False(t, env.FileExists(filepath.Join(dir, "1.0.toml")))
		assert.Equal(t, own, env.ReadFile(filepath.Join(dir, "2.0.toml")))
		// Refused before downloading anything.
		assert.Equal(t, before, mockAPI.GetDownloadCount("testuser", "sample-theme"))
	})
}

// TestE2E_ForkSecurityGate guards review item 1: fork writes downloaded
// content as the user's own theme, and `apply` never re-prompts for a theme
// already on disk - so fork's own [custom]-command confirmation is the only
// chance to warn before that content becomes silently trusted.
func TestE2E_ForkSecurityGate(t *testing.T) {
	t.Run("declining aborts and writes nothing", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		env.CreateConfig(`{"username": "me"}`)

		out, err := runStellar(t, strings.NewReader("n\n"), "fork", "testuser/custom-theme@1.0", "my-custom")
		require.Error(t, err)
		assert.Contains(t, out, "SECURITY WARNING")
		assert.Contains(t, out, "Community themes aren't reviewed")

		destPath := filepath.Join(env.StellarDir, "me", "my-custom", "1.0.toml")
		assert.False(t, env.FileExists(destPath), "declining must write nothing")
	})

	t.Run("accepting via a piped y forks it", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		env.CreateConfig(`{"username": "me"}`)

		_, err := runStellar(t, strings.NewReader("y\n"), "fork", "testuser/custom-theme@1.0", "my-custom")
		require.NoError(t, err)

		destPath := filepath.Join(env.StellarDir, "me", "my-custom", "1.0.toml")
		assert.True(t, env.FileExists(destPath))
	})

	t.Run("--force skips the prompt entirely", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		env.CreateConfig(`{"username": "me"}`)

		out, err := runStellar(t, nil, "fork", "testuser/custom-theme@1.0", "my-custom", "--force")
		require.NoError(t, err)
		assert.NotContains(t, out, "SECURITY WARNING")

		destPath := filepath.Join(env.StellarDir, "me", "my-custom", "1.0.toml")
		assert.True(t, env.FileExists(destPath))
	})

	t.Run("EOF on a non-TTY without --force errors naming --force", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		env.CreateConfig(`{"username": "me"}`)

		_, err := runStellar(t, pipeStdin(t), "fork", "testuser/custom-theme@1.0", "my-custom")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--force")

		destPath := filepath.Join(env.StellarDir, "me", "my-custom", "1.0.toml")
		assert.False(t, env.FileExists(destPath))
	})

	t.Run("a theme without custom commands never shows the warning", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		env.CreateConfig(`{"username": "me"}`)

		out, err := runStellar(t, nil, "fork", "testuser/sample-theme@1.2", "my-blue")
		require.NoError(t, err)
		assert.NotContains(t, out, "SECURITY WARNING")
	})
}

// TestE2E_NewAndForkRefuseBackupName guards review item 21: "backup" is
// reserved for config backups and must never be usable as a new/fork
// destination name.
func TestE2E_NewAndForkRefuseBackupName(t *testing.T) {
	t.Run("new", func(t *testing.T) {
		testutil.SetupTestEnv(t)
		resetFlags()

		_, err := runStellar(t, nil, "new", "me/backup")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup")
	})

	t.Run("fork", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		env.CreateConfig(`{"username": "me"}`)

		_, err := runStellar(t, nil, "fork", "testuser/sample-theme@1.2", "me/backup")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "backup")
	})
}

// TestE2E_NewAndFork_ConfigLoadErrorDoesNotWipeIt guards review item 23: a
// config.json that merely fails to *read* (corrupt JSON here) must never be
// silently replaced by an empty one - that would wipe out the user's real
// current/downloaded-theme state.
func TestE2E_NewAndFork_ConfigLoadErrorDoesNotWipeIt(t *testing.T) {
	t.Run("new", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		env.CreateConfig(`{not valid json`)

		_, err := runStellar(t, nil, "new", "me/my-theme")
		require.Error(t, err)

		raw := env.ReadFile(filepath.Join(env.StellarDir, "config.json"))
		assert.Equal(t, `{not valid json`, raw, "the corrupt config.json must be left exactly as it was, not overwritten with an empty one")
	})

	t.Run("fork", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		resetFlags()
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		env.CreateConfig(`{not valid json`)

		_, err := runStellar(t, nil, "fork", "testuser/sample-theme@1.2", "me/my-blue")
		require.Error(t, err)

		raw := env.ReadFile(filepath.Join(env.StellarDir, "config.json"))
		assert.Equal(t, `{not valid json`, raw)
	})
}

func TestWriteNewThemeFileNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "1.0.toml")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0644))

	err := writeNewThemeFile(path, []byte("replacement"))
	require.Error(t, err)
	data, rerr := os.ReadFile(path)
	require.NoError(t, rerr)
	assert.Equal(t, "original", string(data))

	fresh := filepath.Join(filepath.Dir(path), "2.0.toml")
	require.NoError(t, writeNewThemeFile(fresh, []byte("new")))
	data, rerr = os.ReadFile(fresh)
	require.NoError(t, rerr)
	assert.Equal(t, "new", string(data))
}
