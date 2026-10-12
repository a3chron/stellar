// cmd/cli_api_errors.go holds the error translations shared by every command
// that talks to the CLI auth/drafts endpoints (auth, new, fork, publish):
// turning api.ErrHubTooOld and api.ErrUnauthorized into the clear, specific
// message stellar should show instead of a raw status code or - worse - a
// misleading "run stellar auth" for a hub that doesn't even have that route
// yet.
package cmd

import (
	"errors"

	"github.com/a3chron/stellar/internal/api"
)

// notLoggedInError is what every command shows when the stored (or
// STELLAR_TOKEN) credential is missing or no longer accepted.
func notLoggedInError() error {
	return newHintedError("not logged in to stellar-hub", hint{
		label:  "Run",
		values: []string{"stellar auth"},
	})
}

// wrapHubAPIError translates the two sentinel failure modes every CLI
// auth/drafts call can hit into their user-facing form:
//
//   - api.ErrHubTooOld: the hub doesn't implement these routes at all (the
//     CLI shipped ahead of the hub, or the hub was rolled back behind it).
//     Showing "Unauthorized" or "run stellar auth" here would send the user
//     on a pointless loop re-authenticating against a hub that will never
//     accept it - so this gets its own distinct message instead, with a
//     hint pointing at either upgrading the hub or the web upload form.
//   - api.ErrUnauthorized: a real, contract-shaped 401 (invalid or revoked
//     token) -> notLoggedInError().
//
// Any other error (offline, a different APIError, ...) is returned
// unchanged. err == nil returns nil.
func wrapHubAPIError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, api.ErrHubTooOld) {
		return newHintedError(err.Error(), hint{
			label:  "Hint",
			values: []string{"ask the hub owner to update it, or publish at https://stellar.a3chron.dev/upload for now"},
		})
	}
	if errors.Is(err, api.ErrUnauthorized) {
		return notLoggedInError()
	}
	return err
}
