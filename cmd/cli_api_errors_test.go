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

// TestWrapHubAPIError_HubTooOldHintIsTwoItems guards review item 10: the
// hub-too-old hint used to be one run-on sentence ("ask the hub owner to
// update it, or publish at ... for now"); it now renders as two separate
// list items under one "Hint:" label, matching how a multi-option hint
// renders everywhere else in the CLI (e.g. "Did you mean").
// fakeHubTooOldError satisfies errors.Is(err, api.ErrHubTooOld) without
// needing api's own unexported hubTooOldError type (used by the real client
// when a pre-upgrade hub deployment is detected - see TestE2E_AuthHubTooOld
// for that path end to end).
type fakeHubTooOldError struct{ baseURL string }

func (e *fakeHubTooOldError) Error() string {
	return "stellar-hub at " + e.baseURL + " doesn't support CLI publishing yet"
}

func (e *fakeHubTooOldError) Is(target error) bool { return target == api.ErrHubTooOld }

func TestWrapHubAPIError_HubTooOldHintIsTwoItems(t *testing.T) {
	orig := &fakeHubTooOldError{baseURL: "https://stellar.example"}
	err := wrapHubAPIError(orig)
	require.Error(t, err)
	var he *hintedError
	require.ErrorAs(t, err, &he)
	require.Len(t, he.hints, 1)
	require.Len(t, he.hints[0].values, 2)
	assert.Equal(t, "ask the hub owner to update it", he.hints[0].values[0])
	assert.Contains(t, he.hints[0].values[1], "or publish at")
	assert.Contains(t, he.hints[0].values[1], "/upload for now")
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
