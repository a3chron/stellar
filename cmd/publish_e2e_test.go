// E2E tests for `stellar publish`, driven through NewRootCmd().Execute()
// against internal/testutil's mock API.
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a3chron/stellar/internal/auth"
	"github.com/a3chron/stellar/internal/config"
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

func TestE2E_PublishPipedStdinNeverPromptsForName(t *testing.T) {
	// The name prompt is terminal-only: on piped stdin the title-cased slug is
	// used without reading anything, so input meant for a y/N confirmation can
	// never end up as the theme's name.
	env, mockAPI := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())

	out, err := runStellar(t, strings.NewReader("y\n"), "publish", path)
	require.NoError(t, err)
	assert.NotContains(t, out, "Theme name")
	assert.Equal(t, []string{"My Theme"}, mockAPI.DraftNames("testuser"))
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
	assert.Contains(t, err.Error(), "invalid TOML")
	assert.NotContains(t, err.Error(), "invalid config: invalid TOML", "the message must not stack two redundant prefixes")
}

func TestE2E_PublishScreenshotTooLarge(t *testing.T) {
	env, _ := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())
	shot := filepath.Join(env.RootDir, "big.png")
	require.NoError(t, os.WriteFile(shot, pngBytes(5*1024*1024), 0644))

	_, err := runStellar(t, nil, "publish", path, "--name", "My Theme", "--screenshot", shot, "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "big.png is")
	assert.Contains(t, err.Error(), "MB")
	assert.Contains(t, err.Error(), "screenshots can be at most")
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

// TestE2E_PublishDraftCodeValidation guards review item 5: --draft is
// checked against the contract's code shape before any request is made.
func TestE2E_PublishDraftCodeValidation(t *testing.T) {
	env, _ := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())

	_, err := runStellar(t, nil, "publish", path, "--draft", "nope", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--draft")
}

func TestE2E_PublishDraftCodeValidation_RejectsAmbiguousLetters(t *testing.T) {
	env, _ := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())

	// The code alphabet excludes 0/O and 1/I.
	_, err := runStellar(t, nil, "publish", path, "--draft", "O1O1O1", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--draft")
}

func TestE2E_PublishDraftCodeValidation_AcceptsLowercase(t *testing.T) {
	env, mockAPI := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())
	code := mockAPI.SeedThemeDraft("testuser", "other-slug", nil)

	_, err := runStellar(t, nil, "publish", path, "--draft", strings.ToLower(code), "--name", "My Theme", "--yes")
	require.NoError(t, err, "the code is validated case-insensitively")
}

// TestE2E_PublishDraftFlagSkipsSlugResolution guards review item 8: the
// wizard's own suggested "stellar publish ./path/to/starship.toml --draft
// CODE" must work non-interactively - it used to ask for an unused slug (or
// fail without a TTY) because slug resolution ran unconditionally.
func TestE2E_PublishDraftFlagSkipsSlugResolution(t *testing.T) {
	env, mockAPI := setupPublishEnv(t)
	code := mockAPI.SeedThemeDraft("testuser", "some-theme", nil)

	dir := filepath.Join(env.RootDir, "project")
	require.NoError(t, os.MkdirAll(dir, 0755))
	starship := filepath.Join(dir, "starship.toml")
	require.NoError(t, os.WriteFile(starship, []byte(testutil.SampleTOML()), 0644))

	out, err := runStellar(t, strings.NewReader(""), "publish", starship, "--draft", code, "--name", "My Theme", "--yes")
	require.NoError(t, err, "a loose starship.toml with --draft must never prompt for (or require) a slug")
	assert.Contains(t, out, code)
}

// TestE2E_PublishVersionDraftIgnoresThemeOnlyFields guards review item 7:
// attaching to (or creating) a version draft must never send the theme-only
// fields, and must warn once instead.
func TestE2E_PublishVersionDraftIgnoresThemeOnlyFields(t *testing.T) {
	env, mockAPI := setupPublishEnv(t)
	mockAPI.AddTheme(testutil.MockTheme{
		ID: "t1", Author: "testuser", Slug: "my-theme", Name: "My Theme",
		Versions: []testutil.MockVersion{{Version: "1.0", ConfigContent: testutil.SampleTOML(), Dependencies: []string{}}},
	})
	path := env.CreateThemeFile("testuser", "my-theme", "1.1", testutil.SampleTOML()+"\n# tweak\n")

	out, err := runStellar(t, nil, "publish", path, "--name", "Ignored Name", "--description", "ignored", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "only apply to new themes")
}

// TestE2E_PublishVersionDraftSkipsScreenshot guards review item 9: a
// version draft uses the theme's existing screenshot, so --screenshot must
// be skipped (with a notice) rather than attempted.
func TestE2E_PublishVersionDraftSkipsScreenshot(t *testing.T) {
	env, mockAPI := setupPublishEnv(t)
	mockAPI.AddTheme(testutil.MockTheme{
		ID: "t1", Author: "testuser", Slug: "my-theme", Name: "My Theme",
		Versions: []testutil.MockVersion{{Version: "1.0", ConfigContent: testutil.SampleTOML(), Dependencies: []string{}}},
	})
	path := env.CreateThemeFile("testuser", "my-theme", "1.1", testutil.SampleTOML()+"\n# tweak\n")
	shot := filepath.Join(env.RootDir, "shot.png")
	require.NoError(t, os.WriteFile(shot, pngBytes(256), 0644))

	out, err := runStellar(t, nil, "publish", path, "--screenshot", shot, "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "skipping --screenshot")
}

// TestE2E_PublishMarksSourceAuthored guards review item 10: publishing a
// theme that lives inside StellarHome must record it as authored, even
// before downloaded_themes ever mentions it - otherwise `apply --update`
// would make it look like a plain download, and a plain `stellar clean`
// would delete the only copy of a theme that was never fetched from
// anywhere.
func TestE2E_PublishMarksSourceAuthored(t *testing.T) {
	env, _ := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "my-hand-theme", "1.0", testutil.SampleTOML())

	_, err := runStellar(t, nil, "publish", path, "--name", "My Theme", "--yes")
	require.NoError(t, err)

	cfg, cerr := config.Load()
	require.NoError(t, cerr)
	assert.True(t, cfg.HasAuthored("testuser/my-hand-theme"))

	// Simulate the theme having also been "downloaded" (e.g. published then
	// re-fetched on another machine) - it must still survive a plain clean,
	// since it's authored.
	cfg.MarkDownloaded("testuser/my-hand-theme")
	require.NoError(t, cfg.Save())

	out, cleanErr := runStellar(t, nil, "clean")
	require.NoError(t, cleanErr)
	assert.Contains(t, out, "Nothing to clean")
	assert.True(t, env.FileExists(path), "an authored (published) theme must survive a plain clean")
}

// TestE2E_PublishOwnTheme_CaseInsensitive guards review item 11: author
// comparison must be case-insensitive, matching the hub - a theme folder
// named with a different case than the logged-in username ("TestUser" on
// disk vs. "testuser" from whoami) is still the same person's own theme,
// and must NOT be refused as "someone else's theme" just because Go string
// equality is case-sensitive.
func TestE2E_PublishOwnTheme_CaseInsensitive(t *testing.T) {
	env, _ := setupPublishEnv(t)
	path := env.CreateThemeFile("TestUser", "rainbow", "1.0", testutil.SampleTOML())
	env.CreateConfig(`{"downloaded_themes": ["TestUser/rainbow"]}`)

	_, err := runStellar(t, nil, "publish", path, "--name", "Rainbow", "--yes")
	require.NoError(t, err, "a case-only difference in the author must not be treated as someone else's theme")
}

// TestE2E_PublishYesNeverPromptsOnAmbiguousMatch guards review item 12:
// --yes must never prompt, even for the "multiple local themes match"
// selection - it has no safe default, so it must fail by naming what to
// pass instead.
func TestE2E_PublishYesNeverPromptsOnAmbiguousMatch(t *testing.T) {
	env, _ := setupPublishEnv(t)
	env.CreateThemeFile("alice", "dup-theme", "1.0", testutil.SampleTOML())
	env.CreateThemeFile("bob", "dup-theme", "1.0", testutil.SampleTOML())

	_, err := runStellar(t, nil, "publish", "dup-theme", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "full path")
}

// TestE2E_PublishLooseTomlUsesFileStem guards review item 13: a loose,
// arbitrary .toml file must publish under its own name, not its parent
// folder's.
func TestE2E_PublishLooseTomlUsesFileStem(t *testing.T) {
	env, _ := setupPublishEnv(t)
	dir := filepath.Join(env.RootDir, "Downloads")
	require.NoError(t, os.MkdirAll(dir, 0755))
	path := filepath.Join(dir, "catppuccin.toml")
	require.NoError(t, os.WriteFile(path, []byte(testutil.SampleTOML()), 0644))

	out, err := runStellar(t, nil, "publish", path, "--name", "Catppuccin", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "catppuccin")
	assert.NotContains(t, out, "/downloads")
}

// TestE2E_PublishBareNameFallsBackToNormalizedSlug guards the revised review
// item 7: `new`/`fork` always save a theme under its normalized slug-form
// name now, so a bare-name publish lookup must also try the normalized form
// when the name exactly as typed finds nothing - typing a theme's name "the
// way you think of it" (mixed case, underscores) must still find it.
func TestE2E_PublishBareNameFallsBackToNormalizedSlug(t *testing.T) {
	env, _ := setupPublishEnv(t)
	env.CreateThemeFile("testuser", "my-cool-theme", "1.0", testutil.SampleTOML())

	out, err := runStellar(t, nil, "publish", "My_Cool_Theme", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "my-cool-theme")
}

// TestE2E_PublishDirectoryPicksVersionedTomlOverOthers guards review item
// 14: a directory argument must only ever pick the highest "<X.Y>.toml" or
// "starship.toml" - never some other unrelated .toml that merely sorts
// after those names.
func TestE2E_PublishDirectoryPicksVersionedTomlOverOthers(t *testing.T) {
	env, _ := setupPublishEnv(t)
	dir := filepath.Join(env.RootDir, "my-theme-project")
	require.NoError(t, os.MkdirAll(dir, 0755))
	// "zellij.toml" sorts after "starship.toml" lexicographically, and
	// after "1.0.toml" too - it must never be picked.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "zellij.toml"), []byte("unrelated = true\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "starship.toml"), []byte(testutil.SampleTOML()), 0644))

	out, err := runStellar(t, nil, "publish", dir, "--slug", "my-theme", "--name", "My Theme", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "starship.toml")
	assert.NotContains(t, out, "zellij.toml")
}

func TestE2E_PublishDirectoryPrefersVersionedTomlOverStarship(t *testing.T) {
	env, _ := setupPublishEnv(t)
	dir := filepath.Join(env.RootDir, "my-theme-project2")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "starship.toml"), []byte("unrelated = true\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "1.0.toml"), []byte(testutil.SampleTOML()), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2.0.toml"), []byte(testutil.SampleTOML()), 0644))

	out, err := runStellar(t, nil, "publish", dir, "--name", "My Theme", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "2.0.toml", "the highest versioned file must win over both starship.toml and 1.0.toml")
}

// TestE2E_PublishDraftSummaryFormat guards review item 15's exact wording.
func TestE2E_PublishDraftSummaryFormat(t *testing.T) {
	t.Run("new theme draft", func(t *testing.T) {
		env, _ := setupPublishEnv(t)
		path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())

		out, err := runStellar(t, nil, "publish", path, "--name", "My Theme", "--yes")
		require.NoError(t, err)
		assert.Contains(t, out, "My Theme (testuser/my-theme@1.0)")
	})

	t.Run("new version draft", func(t *testing.T) {
		env, mockAPI := setupPublishEnv(t)
		mockAPI.AddTheme(testutil.MockTheme{
			ID: "t1", Author: "testuser", Slug: "my-theme", Name: "My Theme",
			Versions: []testutil.MockVersion{{Version: "1.0", ConfigContent: testutil.SampleTOML(), Dependencies: []string{}}},
		})
		path := env.CreateThemeFile("testuser", "my-theme", "2.0", testutil.SampleTOML()+"\n# tweak\n")

		out, err := runStellar(t, nil, "publish", path, "--yes")
		require.NoError(t, err)
		assert.Contains(t, out, "new version testuser/my-theme@2.0")
	})
}

// TestE2E_PublishFlagValidation guards review item 16: flags are validated
// locally, by name, before any request.
func TestE2E_PublishFlagValidation(t *testing.T) {
	env, _ := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "my-theme", "1.0", testutil.SampleTOML())

	cases := []struct {
		name string
		args []string
	}{
		{"color-mode", []string{"--color-mode", "purple"}},
		{"min-starship", []string{"--min-starship", "1.24"}},
		{"version", []string{"--version", "1.2.3"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetFlags()
			args := append([]string{"publish", path, "--name", "My Theme", "--yes"}, tc.args...)
			_, err := runStellar(t, nil, args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--"+tc.name)
		})
	}
}

func TestE2E_PublishConfigIsEmpty(t *testing.T) {
	env, _ := setupPublishEnv(t)
	path := env.CreateThemeFile("testuser", "empty-theme", "1.0", "")

	_, err := runStellar(t, nil, "publish", path, "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}
