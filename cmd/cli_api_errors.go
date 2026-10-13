// cmd/cli_api_errors.go holds the error translations shared by every command
// that talks to the CLI auth/drafts endpoints (auth, new, fork, publish):
// turning api.ErrHubTooOld and api.ErrUnauthorized into the clear, specific
// message stellar should show instead of a raw status code or - worse - a
// misleading "run stellar auth" for a hub that doesn't even have that route
// yet.
package cmd

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/a3chron/stellar/internal/api"
	"github.com/a3chron/stellar/internal/paths"
)

// notLoggedInError is what every command shows when the stored (or
// STELLAR_TOKEN) credential is missing or no longer accepted.
func notLoggedInError() error {
	return newHintedError("not logged in to stellar-hub", hint{
		label:  "Run",
		values: []string{"stellar auth"},
	})
}

// wrapHubAPIError translates the sentinel failure modes every CLI
// auth/drafts call can hit into their user-facing form:
//
//   - api.ErrOffline: the request never reached a server at all. api's own
//     wrapTransportError builds this with TWO %w verbs (fmt.Errorf("%w: %w",
//     ErrOffline, err)), which gives the result an Unwrap() []error method -
//     and printCLIError treats anything with that method as several
//     independent errors to print on their own "Error:" lines. Left
//     unwrapped, a single offline failure would print as two confusing
//     "Error:" lines instead of one; this returns a flat hintedError (whose
//     Error() is a plain string) with the same wording hub_errors.go already
//     uses elsewhere, so it reads exactly like any other "are you offline?"
//     message.
//   - api.ErrHubTooOld: the hub doesn't implement these routes at all (the
//     CLI shipped ahead of the hub, or the hub was rolled back behind it).
//     Showing "Unauthorized" or "run stellar auth" here would send the user
//     on a pointless loop re-authenticating against a hub that will never
//     accept it - so this gets its own distinct message instead, with a
//     hint pointing at either upgrading the hub or its own web upload form
//     (the base URL actually configured, not a hard-coded production one -
//     a self-hosted or dev deployment must get a link that works for it).
//   - api.ErrUnauthorized: a real, contract-shaped 401 (invalid or revoked
//     token) -> notLoggedInError().
//
// Any other error (a different APIError, ...) is returned unchanged.
// err == nil returns nil.
func wrapHubAPIError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, api.ErrOffline) {
		return newHintedError(fmt.Sprintf("can't reach stellar-hub (are you offline?) (details: %v)", err))
	}
	if errors.Is(err, api.ErrHubTooOld) {
		base := paths.APIURL(api.BaseURL)
		return newHintedError(err.Error(), hint{
			label: "Hint",
			values: []string{
				"ask the hub owner to update it",
				fmt.Sprintf("or publish at %s/upload for now", base),
			},
		})
	}
	if errors.Is(err, api.ErrUnauthorized) {
		return notLoggedInError()
	}

	// Beyond the sentinel cases above, a handful of contract error codes
	// come up often enough in normal use (not just as edge cases) that a
	// raw "Error: you already have a draft with that slug" leaves the user
	// to go figure out what to do about it themselves. These attach the
	// same actionable hint the equivalent situation gets elsewhere in the
	// CLI, without changing the underlying message any caller might still
	// be matching with errors.Is/APIError.Code.
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case "duplicate_slug":
			return newHintedError(apiErr.Error(), hint{
				label:  "Hint",
				values: []string{"pick a different slug with --slug, or publish a new version of the existing theme instead"},
			})
		case "draft_exists":
			if code := existingDraftCodeFromDetails(apiErr.Details); code != "" {
				return newHintedError(apiErr.Error(), hint{
					label:  "Hint",
					values: []string{fmt.Sprintf("attach to it with --draft %s, or finish it on the hub", code)},
				})
			}
			return newHintedError(apiErr.Error(), hint{
				label:  "Hint",
				values: []string{"finish or delete it on the hub, then try again"},
			})
		case "too_many_drafts":
			base := paths.APIURL(api.BaseURL)
			return newHintedError(apiErr.Error(), hint{
				label:  "Hint",
				values: []string{fmt.Sprintf("finish or delete some drafts at %s/settings#drafts, then try again", base)},
			})
		case "theme_not_found":
			return newHintedError(apiErr.Error(), hint{
				label:  "Hint",
				values: []string{"check the author/slug - stellar info <author>/<slug> can confirm it exists"},
			})
		}
	}

	return err
}

// existingDraftCodeFromDetails extracts the "code" field a draft_exists
// error's details carry (the already-existing draft's code) - best-effort:
// an empty result (no details, different shape, parse failure) just means
// the caller falls back to a hint without a specific code to point at.
func existingDraftCodeFromDetails(details json.RawMessage) string {
	if len(details) == 0 {
		return ""
	}
	var parsed struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(details, &parsed); err != nil {
		return ""
	}
	return parsed.Code
}
