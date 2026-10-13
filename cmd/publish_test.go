package cmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSuggestNextVersion(t *testing.T) {
	cases := []struct {
		name   string
		latest string
		want   string
	}{
		{"bumps to the next major, like the hub", "1.2", "2.0"},
		{"from a .0 version", "2.0", "3.0"},
		{"double-digit minor", "1.10", "2.0"},
		{"empty latest has no default", "", ""},
		{"non-numeric major", "a.2", ""},
		{"non-numeric minor", "1.b", ""},
		{"no dot at all", "latest", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := suggestNextVersion(tc.latest); got != tc.want {
				t.Errorf("suggestNextVersion(%q) = %q, want %q", tc.latest, got, tc.want)
			}
		})
	}
}

// TestFillMissingForPublish_VersionDefaultBeatsDeletedFloor covers the hub's
// "delete a single version" feature end to end through the draft JSON: a
// version draft's suggested default must beat the theme's HighestVersion
// (latest existing OR highest-ever-deleted), not just its LatestVersion -
// otherwise the default offered here would be the very number that was
// deleted.
func TestFillMissingForPublish_VersionDefaultBeatsDeletedFloor(t *testing.T) {
	resetFlags()
	env := testutil.SetupTestEnv(t)
	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)
	// testuser/sample-theme's real latest is 1.2; 2.0 was since deleted from
	// the hub, raising the floor above it.
	mockAPI.SetHighestDeletedVersion("testuser", "sample-theme", "2.0")

	// As if the version field had been cleared on the web: config set, no
	// version - exactly what fillMissingForPublish's "version" case handles.
	code := mockAPI.SeedVersionDraft("testuser", "sample-theme")
	token := mockAPI.Token("testuser", "Test User")
	client := api.NewAuthedClientWithURL(env.MockServer.URL, token)

	d, err := client.GetDraft(code, false)
	require.NoError(t, err)
	require.Contains(t, d.Missing, "version")
	require.NotNil(t, d.Theme)
	assert.Equal(t, "1.2", d.Theme.LatestVersion)
	assert.Equal(t, "2.0", d.Theme.HighestVersion)

	r := bufio.NewReader(strings.NewReader("\n")) // accept the suggested default
	var updated *api.Draft
	out := testutil.CaptureOutput(t, func() {
		updated, err = fillMissingForPublish(r, true, client, d)
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Version)
	// 3.0 - the next major above the deleted 2.0, NOT 2.0 (the next major
	// above the real latest existing version, 1.2 - which would just
	// suggest the very number that was deleted).
	assert.Equal(t, "3.0", *updated.Version)
	assert.Contains(t, out, "Version [3.0]")
}

// TestPromptForScreenshotWithRetries guards review item 11: a bad screenshot
// path (missing file, wrong type, too large) used to end the whole publish
// run on the first try - it now retries up to maxScreenshotPromptAttempts
// times before giving up.
func TestPromptForScreenshotWithRetries(t *testing.T) {
	t.Run("gives up after the limit, returning the last error", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		token := mockAPI.Token("testuser", "Test User")
		client := api.NewAuthedClientWithURL(env.MockServer.URL, token)
		code := mockAPI.SeedThemeDraft("testuser", "my-theme", nil)

		missing := filepath.Join(env.RootDir, "nope.png")
		input := strings.Repeat(missing+"\n", maxScreenshotPromptAttempts)
		r := bufio.NewReader(strings.NewReader(input))

		_, err := promptForScreenshotWithRetries(r, client, code)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "couldn't read screenshot")
	})

	t.Run("succeeds after a bad path then a good one", func(t *testing.T) {
		env := testutil.SetupTestEnv(t)
		mockAPI := testutil.CreateDefaultMockAPI()
		env.SetupMockAPI(mockAPI)
		token := mockAPI.Token("testuser", "Test User")
		client := api.NewAuthedClientWithURL(env.MockServer.URL, token)
		code := mockAPI.SeedThemeDraft("testuser", "my-theme", nil)

		missing := filepath.Join(env.RootDir, "nope.png")
		good := filepath.Join(env.RootDir, "shot.png")
		require.NoError(t, os.WriteFile(good, pngBytes(256), 0644))

		r := bufio.NewReader(strings.NewReader(missing + "\n" + good + "\n"))

		d, err := promptForScreenshotWithRetries(r, client, code)
		require.NoError(t, err)
		require.NotNil(t, d.ScreenshotURL)
	})
}
