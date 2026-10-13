// E2E tests for `stellar publish`, driven through NewRootCmd().Execute()
// against internal/testutil's mock API.
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a3chron/stellar/internal/auth"
	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupPublishEnv sets up an isolated test env with a mock API and a
// logged-in "testuser", ready for `stellar publish` tests.
func setupPublishEnv(t *testing.T) (*testutil.TestEnv, *testutil.MockAPIHandler) {
	t.Helper()
	env := testutil.SetupTestEnv(t)
	resetFlags()
	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)
	token := mockAPI.Token("testuser", "Test User")
	require.NoError(t, auth.Save(env.MockServer.URL, token))
	return env, mockAPI
}

func pngBytes(size int) []byte {
	data := make([]byte, size)
	copy(data, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	return data
}

func TestE2E_PublishNewThemeDraft(t *testing.T) {
	env, _ := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())

	out, err := runStellar(t, nil, "publish", path, "--name", "My Theme", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Draft")
	assert.Contains(t, out, "my-theme")
	assert.Contains(t, out, "Finish it on the hub")
}

func TestE2E_PublishNewVersionDraft(t *testing.T) {
	env, mockAPI := setupPublishEnv(t)
	mockAPI.AddTheme(testutil.MockTheme{
		ID: "t1", Author: "testuser", Slug: "my-theme", Name: "My Theme",
		Versions: []testutil.MockVersion{{Version: "1.0", ConfigContent: testutil.SampleTOML(), Dependencies: []string{}}},
	})
	path := env.CreateThemeFile("testuser", "my-theme", "1.1", testutil.SampleTOML()+"\n# tweak\n")

	out, err := runStellar(t, nil, "publish", path, "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Draft")
	assert.Contains(t, out, "my-theme")
}

func TestE2E_PublishReusesMatchingSlugDraft(t *testing.T) {
	env, mockAPI := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())

	_, err := runStellar(t, nil, "publish", path, "--name", "My Theme", "--yes")
	require.NoError(t, err)
	require.Equal(t, 1, mockAPI.DraftCount("testuser"))

	out, err := runStellar(t, nil, "publish", path, "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Draft")
	assert.Equal(t, 1, mockAPI.DraftCount("testuser"), "a second publish of the same slug must attach, not create another draft")
}

func TestE2E_PublishReusesMatchingSlugDraftWithPipedConfirm(t *testing.T) {
	// Exercises the interactive confirm path (not just --yes): a piped "y"
	// answers it even though stdin isn't a real terminal, matching
	// promptConfirmation's convention elsewhere in this package.
	env, mockAPI := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())

	_, err := runStellar(t, nil, "publish", path, "--name", "My Theme", "--yes")
	require.NoError(t, err)

	out, err := runStellar(t, strings.NewReader("y\n"), "publish", path)
	require.NoError(t, err)
	assert.Contains(t, out, "Draft")
	assert.Equal(t, 1, mockAPI.DraftCount("testuser"))
}

func TestE2E_PublishExplicitDraftFlag(t *testing.T) {
	env, mockAPI := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())
	code := mockAPI.SeedThemeDraft("testuser", "other-slug", nil)

	out, err := runStellar(t, nil, "publish", path, "--draft", code, "--name", "My Theme", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, code)
	assert.Equal(t, 1, mockAPI.DraftCount("testuser"))
}

func TestE2E_PublishPublishHappyPath(t *testing.T) {
	env, _ := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())
	shot := filepath.Join(env.RootDir, "shot.png")
	require.NoError(t, os.WriteFile(shot, pngBytes(256), 0644))

	out, err := runStellar(t, nil, "publish", path, "--name", "My Theme", "--screenshot", shot, "--publish", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Published")
	assert.Contains(t, out, "testuser/my-theme")
}

func TestE2E_PublishMissingNameOnAttachNonInteractive(t *testing.T) {
	env, mockAPI := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())
	// Seed a nameless theme draft, as the web wizard's first "Next" would
	// before the user has typed a name.
	code := mockAPI.SeedThemeDraft("testuser", "my-theme", nil)

	_, err := runStellar(t, strings.NewReader(""), "publish", path, "--draft", code, "--publish", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--name")
	// The draft itself must still be there - the error should point back at it.
	assert.Contains(t, err.Error(), code)
	assert.Equal(t, 1, mockAPI.DraftCount("testuser"))
}

func TestE2E_PublishInvalidTOML(t *testing.T) {
	env, _ := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "broken", "1.0", testutil.InvalidTOML())

	_, err := runStellar(t, nil, "publish", path, "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

func TestE2E_PublishScreenshotTooLarge(t *testing.T) {
	env, _ := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())
	shot := filepath.Join(env.RootDir, "big.png")
	require.NoError(t, os.WriteFile(shot, pngBytes(5*1024*1024), 0644))

	_, err := runStellar(t, nil, "publish", path, "--name", "My Theme", "--screenshot", shot, "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestE2E_PublishScreenshotWrongType(t *testing.T) {
	env, _ := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())
	shot := filepath.Join(env.RootDir, "not-an-image.txt")
	require.NoError(t, os.WriteFile(shot, []byte("just text"), 0644))

	_, err := runStellar(t, nil, "publish", path, "--name", "My Theme", "--screenshot", shot, "--yes")
	require.Error(t, err)
}

func TestE2E_PublishNotLoggedInNonInteractive(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	resetFlags()
	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)
	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())

	_, err := runStellar(t, strings.NewReader(""), "publish", path, "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not logged in")
}

func TestE2E_PublishRefusesOtherAuthorsDownloadedTheme(t *testing.T) {
	env, _ := setupPublishEnv(t)
	path := env.CreateThemeFile("alice", "rainbow", "1.0", testutil.SampleTOML())
	env.CreateConfig(`{"downloaded_themes": ["alice/rainbow"]}`)

	_, err := runStellar(t, nil, "publish", path, "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fork")
}

func TestE2E_PublishAllowsOwnTheme(t *testing.T) {
	// A theme under the logged-in user's own author dir is fine even if it's
	// also in DownloadedThemes (e.g. they forked their own published theme).
	env, _ := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())
	env.CreateConfig(`{"downloaded_themes": ["testuser/my-theme"]}`)

	_, err := runStellar(t, nil, "publish", path, "--name", "My Theme", "--yes")
	require.NoError(t, err)
}

func TestE2E_PublishRefusesBackupSlug(t *testing.T) {
	env, _ := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "backup", "1.0", testutil.SampleTOML())

	_, err := runStellar(t, nil, "publish", path, "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup")
}

func TestE2E_PublishWaitingDraftsAlwaysCreateNewNonInteractive(t *testing.T) {
	env, mockAPI := setupPublishEnv(t)
	mockAPI.SeedThemeDraft("testuser", "other-theme", nil)

	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())
	_, err := runStellar(t, strings.NewReader(""), "publish", path, "--name", "My Theme", "--yes")
	require.NoError(t, err)

	assert.Equal(t, 2, mockAPI.DraftCount("testuser"), "a non-interactive run must create a new draft rather than touch the unrelated waiting one")
}

func TestE2E_PublishExplicitSlugOverridesDerived(t *testing.T) {
	env, _ := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "original-name", "1.0", testutil.SampleTOML())

	out, err := runStellar(t, nil, "publish", path, "--slug", "custom_Slug!!", "--name", "My Theme", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "custom-slug")
}
