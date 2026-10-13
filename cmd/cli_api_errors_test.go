package cmd

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/a3chron/stellar/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWrapHubAPIError_CommonCodesGetHints guards review item 1: a handful of
// contract error codes come up often enough in normal use that a bare
// "Error: ..." leaves the user to figure out what to do next by themselves -
// these should carry an actionable hint instead.
func TestWrapHubAPIError_CommonCodesGetHints(t *testing.T) {
	t.Run("duplicate_slug", func(t *testing.T) {
		err := wrapHubAPIError(&api.APIError{
			Status:  http.StatusConflict,
			Code:    "duplicate_slug",
			Message: "you already have a published theme with that slug",
		})
		require.Error(t, err)
		var he *hintedError
		require.ErrorAs(t, err, &he)
		assert.Equal(t, "you already have a published theme with that slug", he.msg)
		require.Len(t, he.hints, 1)
		assert.Contains(t, he.hints[0].values[0], "--slug")
	})

	t.Run("draft_exists with a code in details points at --draft", func(t *testing.T) {
		details, derr := json.Marshal(map[string]string{"code": "AB2345"})
		require.NoError(t, derr)
		err := wrapHubAPIError(&api.APIError{
			Status:  http.StatusConflict,
			Code:    "draft_exists",
			Message: "you already have a draft with that slug",
			Details: details,
		})
		require.Error(t, err)
		var he *hintedError
		require.ErrorAs(t, err, &he)
		require.Len(t, he.hints, 1)
		assert.Contains(t, he.hints[0].values[0], "--draft AB2345")
	})

	t.Run("draft_exists without details falls back to a generic hint", func(t *testing.T) {
		err := wrapHubAPIError(&api.APIError{
			Status:  http.StatusConflict,
			Code:    "draft_exists",
			Message: "you already have a draft with that slug",
		})
		require.Error(t, err)
		var he *hintedError
		require.ErrorAs(t, err, &he)
		require.Len(t, he.hints, 1)
		assert.NotContains(t, he.hints[0].values[0], "--draft")
	})

	t.Run("too_many_drafts points at the hub's drafts page", func(t *testing.T) {
		err := wrapHubAPIError(&api.APIError{
			Status:  http.StatusConflict,
			Code:    "too_many_drafts",
			Message: "you already have 10 live drafts",
		})
		require.Error(t, err)
		var he *hintedError
		require.ErrorAs(t, err, &he)
		require.Len(t, he.hints, 1)
		assert.Contains(t, he.hints[0].values[0], "/settings#drafts")
	})

	t.Run("theme_not_found suggests stellar info", func(t *testing.T) {
		err := wrapHubAPIError(&api.APIError{
			Status:  http.StatusNotFound,
			Code:    "theme_not_found",
			Message: "theme not found",
		})
		require.Error(t, err)
		var he *hintedError
		require.ErrorAs(t, err, &he)
		require.Len(t, he.hints, 1)
		assert.Contains(t, he.hints[0].values[0], "stellar info")
	})

	t.Run("unknown code is returned unchanged", func(t *testing.T) {
		orig := &api.APIError{Status: http.StatusBadRequest, Code: "something_else", Message: "nope"}
		err := wrapHubAPIError(orig)
		assert.Same(t, error(orig), err)
	})

	t.Run("nil stays nil", func(t *testing.T) {
		assert.NoError(t, wrapHubAPIError(nil))
	})
}

// TestWrapHubAPIError_HubTooOldPointsAtTheWebUpload guards review item 10:
// the hub-too-old hint is one short pointer to the web upload on the actual
// base URL - not a run-on sentence, and not "ask the hub owner" (for the
// official hub, that's the person who'd ship the update anyway).
//
// fakeHubTooOldError satisfies errors.Is(err, api.ErrHubTooOld) without
// needing api's own unexported hubTooOldError type (used by the real client
// when a pre-upgrade hub deployment is detected - see TestE2E_AuthHubTooOld
// for that path end to end).
type fakeHubTooOldError struct{ baseURL string }

func (e *fakeHubTooOldError) Error() string {
	return "stellar-hub at " + e.baseURL + " doesn't support CLI publishing yet"
}

func (e *fakeHubTooOldError) Is(target error) bool { return target == api.ErrHubTooOld }

func TestWrapHubAPIError_HubTooOldPointsAtTheWebUpload(t *testing.T) {
	orig := &fakeHubTooOldError{baseURL: "https://stellar.example"}
	err := wrapHubAPIError(orig)
	require.Error(t, err)
	var he *hintedError
	require.ErrorAs(t, err, &he)
	require.Len(t, he.hints, 1)
	require.Len(t, he.hints[0].values, 1)
	assert.Contains(t, he.hints[0].values[0], "publish on the web for now: ")
	assert.Contains(t, he.hints[0].values[0], "/upload")
}

func TestWrapHubAPIError_VersionAndDraftHints(t *testing.T) {
	t.Run("version_not_newer suggests the next version after latest", func(t *testing.T) {
		details, derr := json.Marshal(map[string]string{"latest": "1.4"})
		require.NoError(t, derr)
		err := wrapHubAPIError(&api.APIError{
			Status:  http.StatusBadRequest,
			Code:    "version_not_newer",
			Message: "Version 1.2 isn't newer than the latest version 1.4",
			Details: details,
		})
		var he *hintedError
		require.ErrorAs(t, err, &he)
		require.Len(t, he.hints, 1)
		assert.Contains(t, he.hints[0].values[0], "--version 2.0")
	})

	t.Run("version_not_newer without details still names the flag", func(t *testing.T) {
		err := wrapHubAPIError(&api.APIError{Status: http.StatusBadRequest, Code: "version_not_newer", Message: "not newer"})
		var he *hintedError
		require.ErrorAs(t, err, &he)
		require.Len(t, he.hints, 1)
		assert.Contains(t, he.hints[0].values[0], "--version")
	})

	t.Run("draft_not_found suggests starting a new draft", func(t *testing.T) {
		err := wrapHubAPIError(&api.APIError{Status: http.StatusNotFound, Code: "draft_not_found", Message: "Draft not found"})
		var he *hintedError
		require.ErrorAs(t, err, &he)
		require.Len(t, he.hints, 1)
		assert.Contains(t, he.hints[0].values[0], "without --draft")
	})
}

func TestExistingDraftCodeFromDetails(t *testing.T) {
	t.Run("extracts the code", func(t *testing.T) {
		raw, err := json.Marshal(map[string]string{"code": "ZZ9988"})
		require.NoError(t, err)
		assert.Equal(t, "ZZ9988", existingDraftCodeFromDetails(raw))
	})

	t.Run("empty details", func(t *testing.T) {
		assert.Equal(t, "", existingDraftCodeFromDetails(nil))
	})

	t.Run("unparseable details", func(t *testing.T) {
		assert.Equal(t, "", existingDraftCodeFromDetails(json.RawMessage(`not json`)))
	})
}
