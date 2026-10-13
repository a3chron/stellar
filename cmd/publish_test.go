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
		{"bumps the minor component", "1.2", "1.3"},
		{"bumps from zero", "2.0", "2.1"},
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
