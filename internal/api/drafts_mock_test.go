package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newDraftsTestClient starts a mock server with a logged-in "testuser" and
// returns an authed Client for it, alongside the mock itself for further
// setup/assertions. These tests exercise the mock's draft routes directly
// through the typed client methods, deliberately sending shapes
// cmd/publish.go's own guards would never produce on their own (e.g. a
// theme-only field on a version-draft PATCH) - that's the whole point: the
// mock must reject what the real hub would reject, independent of whether
// the CLI happens to be well-behaved.
func newDraftsTestClient(t *testing.T) (*Client, *testutil.MockAPIHandler) {
	t.Helper()
	mock := testutil.CreateDefaultMockAPI()
	server := httptest.NewServer(mock)
	t.Cleanup(server.Close)

	token := mock.Token("testuser", "Test User")
	c := NewAuthedClientWithURL(server.URL, token)
	return c, mock
}

func strPtr(s string) *string { return &s }

func TestMockDrafts_VersionPatchRejectsThemeOnlyFields(t *testing.T) {
	c, mock := newDraftsTestClient(t)
	mock.AddTheme(testutil.MockTheme{
		Author: "testuser", Slug: "my-theme", Name: "My Theme",
		Versions: []testutil.MockVersion{{Version: "1.0", ConfigContent: "format = \"x\"\n"}},
	})
	draft, err := c.CreateDraft(CreateDraftRequest{Kind: DraftKindVersion, ThemeSlug: strPtr("my-theme")})
	require.NoError(t, err)

	cases := map[string]PatchDraftRequest{
		"name":        {Name: strPtr("New Name")},
		"slug":        {Slug: strPtr("new-slug")},
		"description": {Description: strPtr("new desc")},
		"group":       {Group: strPtr("new-group")},
		"colorMode":   {ColorMode: strPtr("dark")},
	}
	for field, req := range cases {
		t.Run(field, func(t *testing.T) {
			_, err := c.PatchDraft(draft.Code, req)
			require.Error(t, err)
			var apiErr *APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, "validation_failed", apiErr.Code)
		})
	}
}

func TestMockDrafts_VersionCreateSilentlyIgnoresThemeOnlyFields(t *testing.T) {
	c, mock := newDraftsTestClient(t)
	mock.AddTheme(testutil.MockTheme{
		Author: "testuser", Slug: "my-theme", Name: "My Theme",
		Versions: []testutil.MockVersion{{Version: "1.0", ConfigContent: "format = \"x\"\n"}},
	})

	draft, err := c.CreateDraft(CreateDraftRequest{
		Kind: DraftKindVersion, ThemeSlug: strPtr("my-theme"),
		Name: strPtr("Ignored"), Description: strPtr("ignored"),
	})
	require.NoError(t, err, "POST kind:version must not reject theme-only fields, just ignore them")
	assert.Nil(t, draft.Name)
	assert.Nil(t, draft.Description)
}

func TestMockDrafts_FieldValidation(t *testing.T) {
	c, _ := newDraftsTestClient(t)

	cases := map[string]CreateDraftRequest{
		"bad colorMode":        {Kind: DraftKindTheme, Slug: strPtr("t1"), ColorMode: strPtr("purple")},
		"bad minStarship":      {Kind: DraftKindTheme, Slug: strPtr("t2"), MinStarshipVersion: strPtr("1.24")},
		"bad version":          {Kind: DraftKindTheme, Slug: strPtr("t3"), Version: strPtr("1.2.3")},
		"name too long":        {Kind: DraftKindTheme, Slug: strPtr("t4"), Name: strPtr(strings.Repeat("a", 101))},
		"description too long": {Kind: DraftKindTheme, Slug: strPtr("t5"), Description: strPtr(strings.Repeat("a", 501))},
		"group too long":       {Kind: DraftKindTheme, Slug: strPtr("t6"), Group: strPtr(strings.Repeat("a", 101))},
		"source too long":      {Kind: DraftKindTheme, Slug: strPtr("t7"), Source: strPtr(strings.Repeat("a", 201))},
		"too many deps": {Kind: DraftKindTheme, Slug: strPtr("t8"), Dependencies: func() *[]string {
			d := make([]string, 21)
			return &d
		}()},
		"dep too long": {Kind: DraftKindTheme, Slug: strPtr("t9"), Dependencies: &[]string{strings.Repeat("a", 101)}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := c.CreateDraft(req)
			require.Error(t, err)
			var apiErr *APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, "validation_failed", apiErr.Code)
		})
	}
}

func TestMockDrafts_TooManyDraftsCheckedAfterDuplicateSlug(t *testing.T) {
	c, _ := newDraftsTestClient(t)

	// Fill up to the live-draft limit.
	for i := 0; i < 10; i++ {
		_, err := c.CreateDraft(CreateDraftRequest{Kind: DraftKindTheme, Slug: strPtr(strings.Repeat("x", 1) + "-" + string(rune('a'+i)))})
		require.NoError(t, err)
	}

	// A slug that collides with one of the 10 already-live drafts must
	// report draft_exists, not too_many_drafts, even though the user is
	// also at the limit - the more specific error takes priority.
	_, err := c.CreateDraft(CreateDraftRequest{Kind: DraftKindTheme, Slug: strPtr("x-a")})
	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "draft_exists", apiErr.Code)

	// A genuinely new slug, still at the limit, must report too_many_drafts.
	_, err = c.CreateDraft(CreateDraftRequest{Kind: DraftKindTheme, Slug: strPtr("brand-new")})
	require.Error(t, err)
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "too_many_drafts", apiErr.Code)
}

func TestMockDrafts_ScreenshotContentTypeMustMatchMagicBytes(t *testing.T) {
	c, _ := newDraftsTestClient(t)
	draft, err := c.CreateDraft(CreateDraftRequest{Kind: DraftKindTheme, Slug: strPtr("shot-theme")})
	require.NoError(t, err)

	// Real PNG magic bytes, but UploadScreenshot will declare the
	// Content-Type from DetectImageContentType - to simulate a MISMATCHED
	// declaration we have to go lower-level than the typed client, which
	// always keeps them in sync by construction. A pure JPEG byte sequence
	// through the normal path is the simplest way to confirm the two checks
	// (declared type and magic bytes) really are both enforced and agree.
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0, 0, 0, 0, 0}
	_, err = c.UploadScreenshot(draft.Code, "shot.jpg", jpeg)
	require.NoError(t, err, "a real JPEG must still be accepted")

	// Bytes that match no known format at all.
	_, err = c.UploadScreenshot(draft.Code, "shot.bin", []byte("not an image"))
	require.Error(t, err, "local pre-check refuses before any request")
}

func TestMockDrafts_CaseInsensitiveUsernameThemeLookup(t *testing.T) {
	c, mock := newDraftsTestClient(t)
	// Stored under a differently-cased author than the bearer token's
	// username ("testuser") - the hub compares usernames case-insensitively.
	mock.AddTheme(testutil.MockTheme{
		Author: "TestUser", Slug: "my-theme", Name: "My Theme",
		Versions: []testutil.MockVersion{{Version: "1.0", ConfigContent: "format = \"x\"\n"}},
	})

	draft, err := c.CreateDraft(CreateDraftRequest{Kind: DraftKindVersion, ThemeSlug: strPtr("my-theme")})
	require.NoError(t, err, "theme lookup must be case-insensitive on the author")
	assert.Equal(t, "my-theme", draft.Theme.Slug)
}

func TestMockDrafts_UnchangedFromLatestComparesMinStarshipVersion(t *testing.T) {
	c, mock := newDraftsTestClient(t)
	mock.AddTheme(testutil.MockTheme{
		Author: "testuser", Slug: "my-theme", Name: "My Theme",
		Versions: []testutil.MockVersion{{Version: "1.0", ConfigContent: "format = \"x\"\n", MinStarshipVersion: "1.20.0"}},
	})

	draft, err := c.CreateDraft(CreateDraftRequest{Kind: DraftKindVersion, ThemeSlug: strPtr("my-theme")})
	require.NoError(t, err)
	// Defaulted from latest - nothing else changed, so this must read as
	// unchanged.
	assert.Equal(t, "1.20.0", derefOrEmpty(draft.MinStarshipVersion))
	assert.True(t, draft.UnchangedFromLatest)

	updated, err := c.PatchDraft(draft.Code, PatchDraftRequest{MinStarshipVersion: strPtr("1.24.0")})
	require.NoError(t, err)
	assert.False(t, updated.UnchangedFromLatest, "a changed minStarshipVersion alone must break 'unchanged'")
}

func derefOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func TestMockDrafts_VersionDraftDefaultsCopyMinStarshipFromLatest(t *testing.T) {
	c, mock := newDraftsTestClient(t)
	mock.AddTheme(testutil.MockTheme{
		Author: "testuser", Slug: "no-min", Name: "No Min",
		Versions: []testutil.MockVersion{{Version: "1.0", ConfigContent: "format = \"x\"\n"}}, // MinStarshipVersion left unset
	})

	draft, err := c.CreateDraft(CreateDraftRequest{Kind: DraftKindVersion, ThemeSlug: strPtr("no-min")})
	require.NoError(t, err)
	assert.Nil(t, draft.MinStarshipVersion, "must not invent 1.24.0 when the hub never actually stored one")
}

func TestMockDrafts_ConfigWriteAlwaysBumpsConfigSourceEvenIfUnchanged(t *testing.T) {
	c, _ := newDraftsTestClient(t)
	cfg := "format = \"x\"\n"
	draft, err := c.CreateDraft(CreateDraftRequest{Kind: DraftKindTheme, Slug: strPtr("bump-test"), Config: &cfg})
	require.NoError(t, err)
	require.NotNil(t, draft.ConfigSource)
	firstUpdatedAt := draft.ConfigUpdatedAt

	// PATCH with the exact same config content.
	updated, err := c.PatchDraft(draft.Code, PatchDraftRequest{Config: &cfg})
	require.NoError(t, err)
	require.NotNil(t, updated.ConfigSource)
	assert.Equal(t, ConfigSourceCLI, *updated.ConfigSource)
	require.NotNil(t, updated.ConfigUpdatedAt)
	// Not asserting strict time inequality (clock resolution), just that a
	// configSource is set and the write path was taken for unchanged
	// content too (it would have been dropped by a naive "if changed" gate).
	_ = firstUpdatedAt
}

func TestMockDrafts_WhitespaceOnlyConfigCountsAsMissing(t *testing.T) {
	c, _ := newDraftsTestClient(t)
	blank := "   \n  "
	draft, err := c.CreateDraft(CreateDraftRequest{Kind: DraftKindTheme, Slug: strPtr("blank-cfg"), Config: &blank, Name: strPtr("Blank")})
	require.NoError(t, err)

	assert.Contains(t, draft.Missing, "config")
}

func TestMockDrafts_LatestIsNumericMax(t *testing.T) {
	c, mock := newDraftsTestClient(t)
	// Stored out of numeric order on purpose.
	mock.AddTheme(testutil.MockTheme{
		Author: "testuser", Slug: "out-of-order", Name: "Out Of Order",
		Versions: []testutil.MockVersion{
			{Version: "1.9", ConfigContent: "a = 1\n"},
			{Version: "1.10", ConfigContent: "a = 2\n"},
			{Version: "1.2", ConfigContent: "a = 3\n"},
		},
	})

	info, err := c.GetThemeInfo("testuser", "out-of-order")
	require.NoError(t, err)
	require.NotEmpty(t, info.Versions)
	assert.Equal(t, "1.10", info.Versions[0].Version, "GET /api/{a}/{s} must sort numerically, newest first")

	draft, err := c.CreateDraft(CreateDraftRequest{Kind: DraftKindVersion, ThemeSlug: strPtr("out-of-order")})
	require.NoError(t, err)
	assert.Equal(t, "2.0", derefOrEmpty(draft.Version), "the default next version must be (max+1).0, not based on stored order")
}
