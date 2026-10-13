package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/a3chron/stellar/internal/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestHome points STELLAR_HOME at a fresh temp dir and restores the
// original value afterward - this package has no shared testutil helper
// (internal/testutil imports internal/paths, and importing testutil back
// here isn't needed for anything this small).
func setupTestHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := os.Getenv(paths.EnvStellarHome)
	require.NoError(t, os.Setenv(paths.EnvStellarHome, dir))
	t.Cleanup(func() {
		if orig == "" {
			_ = os.Unsetenv(paths.EnvStellarHome)
		} else {
			_ = os.Setenv(paths.EnvStellarHome, orig)
		}
	})
	return dir
}

// TestSave_SucceedsAndRoundTrips guards against a regression where save()'s
// deferred cleanup closed the already-closed temp file a second time,
// recording "file already closed" into the named return even after a fully
// successful write+rename - which would have made every `stellar auth`
// login fail right after a real success.
func TestSave_SucceedsAndRoundTrips(t *testing.T) {
	home := setupTestHome(t)

	require.NoError(t, Save("https://stellar.example", "stl_abc123"))

	credPath := filepath.Join(home, "credentials.json")
	info, err := os.Stat(credPath)
	require.NoError(t, err)

	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	}

	token, err := Token("https://stellar.example")
	require.NoError(t, err)
	assert.Equal(t, "stl_abc123", token)
}

func TestSave_OverwritesExistingEntryForSameHost(t *testing.T) {
	setupTestHome(t)

	require.NoError(t, Save("https://stellar.example", "stl_first"))
	require.NoError(t, Save("https://stellar.example", "stl_second"))

	token, err := Token("https://stellar.example")
	require.NoError(t, err)
	assert.Equal(t, "stl_second", token)
}

func TestSave_KeepsSeparateEntriesPerHost(t *testing.T) {
	setupTestHome(t)

	require.NoError(t, Save("https://stellar.example", "stl_prod"))
	require.NoError(t, Save("http://127.0.0.1:3000", "stl_dev"))

	prod, err := Token("https://stellar.example")
	require.NoError(t, err)
	assert.Equal(t, "stl_prod", prod)

	dev, err := Token("http://127.0.0.1:3000")
	require.NoError(t, err)
	assert.Equal(t, "stl_dev", dev)
}

func TestToken_EnvOverrideWinsOverStored(t *testing.T) {
	setupTestHome(t)
	require.NoError(t, Save("https://stellar.example", "stl_stored"))

	t.Setenv(paths.EnvToken, "stl_from_env")

	token, err := Token("https://stellar.example")
	require.NoError(t, err)
	assert.Equal(t, "stl_from_env", token)
}

func TestToken_EmptyWhenNothingStored(t *testing.T) {
	setupTestHome(t)

	token, err := Token("https://stellar.example")
	require.NoError(t, err)
	assert.Empty(t, token)
}

func TestDelete_RemovesEntry(t *testing.T) {
	setupTestHome(t)
	require.NoError(t, Save("https://stellar.example", "stl_abc123"))

	require.NoError(t, Delete("https://stellar.example"))

	token, err := Token("https://stellar.example")
	require.NoError(t, err)
	assert.Empty(t, token)
}

func TestDelete_AbsentEntryIsNotAnError(t *testing.T) {
	setupTestHome(t)
	require.NoError(t, Delete("https://stellar.example"))
}

// TestStoredToken_IgnoresEnvOverride guards the case STELLAR_TOKEN exists
// for: an operation that must act on the real stored credential (revoking
// it on logout/uninstall) must never be fooled into thinking a CI-only env
// override is the thing that got saved to disk.
func TestStoredToken_IgnoresEnvOverride(t *testing.T) {
	setupTestHome(t)
	require.NoError(t, Save("https://stellar.example", "stl_stored"))
	t.Setenv(paths.EnvToken, "stl_from_env")

	token, err := StoredToken("https://stellar.example")
	require.NoError(t, err)
	assert.Equal(t, "stl_stored", token)
}

func TestStoredToken_EmptyWhenOnlyEnvIsSet(t *testing.T) {
	setupTestHome(t)
	t.Setenv(paths.EnvToken, "stl_from_env")

	token, err := StoredToken("https://stellar.example")
	require.NoError(t, err)
	assert.Empty(t, token, "StoredToken must never fall back to STELLAR_TOKEN")
}

func TestStoredEntries_ListsEveryHost(t *testing.T) {
	setupTestHome(t)
	require.NoError(t, Save("https://stellar.example", "stl_prod"))
	require.NoError(t, Save("http://127.0.0.1:3000", "stl_dev"))

	entries, err := StoredEntries()
	require.NoError(t, err)
	require.Len(t, entries, 2)

	byHost := make(map[string]string, len(entries))
	for _, e := range entries {
		byHost[e.BaseURL] = e.Token
	}
	assert.Equal(t, "stl_prod", byHost["https://stellar.example"])
	assert.Equal(t, "stl_dev", byHost["http://127.0.0.1:3000"])
}

func TestStoredEntries_EmptyWhenNothingStored(t *testing.T) {
	setupTestHome(t)

	entries, err := StoredEntries()
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://Stellar.Example/": "https://stellar.example",
		"https://stellar.example":  "https://stellar.example",
		"http://127.0.0.1:3000/":   "http://127.0.0.1:3000",
		"HTTPS://STELLAR.EXAMPLE":  "https://stellar.example",
	}
	for in, want := range cases {
		assert.Equal(t, want, NormalizeBaseURL(in), "input: %s", in)
	}
}
