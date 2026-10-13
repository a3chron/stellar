package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/a3chron/stellar/internal/paths"
	"github.com/a3chron/stellar/internal/term"
)

// ErrUnauthorized is returned by any bearer-authenticated call when the hub
// answers 401 (an invalid or revoked token). Callers use errors.Is(err,
// ErrUnauthorized) to show a "run stellar auth" style message instead of a
// raw status code.
var ErrUnauthorized = errors.New("not logged in, or the token was revoked")

// ErrHubTooOld is returned when a response to one of the CLI auth/drafts
// endpoints doesn't match the contract at all - which is exactly what
// happens when the CLI talks to a stellar-hub deployment from before CLI
// publishing existed (shipped ahead of the hub, or the hub was rolled back):
// those routes don't exist yet, so the request falls through to an older
// catch-all handler instead of ever reaching contract-shaped code. See
// classifyNewEndpointError for the exact detection rules. Callers use
// errors.Is(err, ErrHubTooOld); the error's own text already names the base
// URL, so it can be shown to the user as-is.
var ErrHubTooOld = errors.New("stellar-hub doesn't support CLI publishing yet")

// hubTooOldError is what ErrHubTooOld is actually returned as - a distinct
// type so its Error() can name the hub's base URL while errors.Is(err,
// ErrHubTooOld) still matches via Is below.
type hubTooOldError struct {
	baseURL string
}

func (e *hubTooOldError) Error() string {
	return fmt.Sprintf("stellar-hub at %s doesn't support CLI publishing yet", e.baseURL)
}

func (e *hubTooOldError) Is(target error) bool { return target == ErrHubTooOld }

// APIError is a parsed error response from one of the CLI auth/drafts
// endpoints: JSON shaped as {error, code?, details?, missing?} per the
// contract. Status is always set; Code is "" only for the non-JSON/legacy
// fallback built by classifyNewEndpointError (e.g. a raw "stellar-hub
// returned 504" or "screenshot too large"), which callers branch on by
// Status/Message rather than Code in that case.
type APIError struct {
	Status  int
	Code    string
	Message string
	Missing []string
	Details json.RawMessage
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("stellar-hub returned %d", e.Status)
}

// Is implements errors.Is(err, ErrUnauthorized) for a 401 APIError,
// regardless of its Code (an invalid/revoked token always carries
// code:"invalid_token" per the contract, but this also catches the rare
// case of a 401 with no body at all).
func (e *APIError) Is(target error) bool {
	return target == ErrUnauthorized && e.Status == http.StatusUnauthorized
}

// isLoopbackHost reports whether host (an http.Request.URL.Host, which may
// include a port) refers to the local machine - "localhost" or a loopback
// IP literal. Used to allow plain http:// only for local development
// (STELLAR_API_URL=http://127.0.0.1:3000) while refusing to ever send a
// bearer token over plain http to anything else.
func isLoopbackHost(host string) bool {
	h := host
	if hostOnly, _, err := net.SplitHostPort(host); err == nil {
		h = hostOnly
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// authedTransport is the RoundTripper used by an authed Client
// (NewAuthedClient): it sets the User-Agent and (when present) the bearer
// Authorization header on every request, and refuses outright to send
// either over plain HTTP to a non-loopback host - a token sent in the clear
// to a real hub would be a credential leak over the network.
type authedTransport struct {
	base  http.RoundTripper
	token string
}

func (t *authedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "http" && !isLoopbackHost(req.URL.Host) {
		return nil, fmt.Errorf("refusing to send credentials over plain http to %s (use https)", req.URL.Host)
	}

	req = req.Clone(req.Context())
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", userAgent())
	}
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(req)
}

// NewAuthedClient creates a Client for the CLI auth and drafts endpoints
// (the only ones that accept bearer auth - see the contract). It never
// follows redirects (CheckRedirect -> ErrUseLastResponse): a redirect to an
// unexpected host would otherwise silently carry the Authorization header
// wherever it points, via authedTransport above refusing non-loopback plain
// http outright on every hop it would otherwise take. token may be "" for
// start/token (the two auth endpoints that take no bearer token at all).
func NewAuthedClient(token string) *Client {
	return NewAuthedClientWithURL(paths.APIURL(BaseURL), token)
}

// NewAuthedClientWithURL is NewAuthedClient for a specific base URL, rather
// than paths.APIURL(BaseURL) (the current STELLAR_API_URL/production
// default). Used where a caller must talk to a hub that isn't necessarily
// "the" current one - e.g. `stellar uninstall` revoking every stored
// credentials.json entry against ITS OWN base URL, not just whichever one
// STELLAR_API_URL happens to point at right now.
func NewAuthedClientWithURL(baseURL, token string) *Client {
	c := newClient(baseURL, 30*time.Second)
	c.token = token
	c.httpClient.Transport = &authedTransport{base: http.DefaultTransport, token: token}
	c.httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return c
}

// classifyNewEndpointError builds the error for a non-2xx response from one
// of the CLI auth/drafts endpoints - the only routes a pre-upgrade hub
// deployment (shipped before CLI publishing, or rolled back behind it) won't
// have at all. Three shapes are distinguished:
//
//  1. A contract error body (JSON with a non-empty "code") -> *APIError,
//     exactly as documented (the CLI branches on Code).
//  2. Valid JSON but no "code" at all (e.g. an old hub's generic
//     {"error":"Unauthorized"}, or {"error":"Author not found"} from a
//     request that fell through to the legacy /api/[author]/[slug] route) ->
//     hubTooOldError. A contract error response always carries a code, so
//     this shape only happens when the route itself doesn't exist yet.
//  3. Not JSON at all: a 404 or 405 (the route genuinely isn't there, e.g. a
//     plain-text or HTML catch-all) -> hubTooOldError. Any other status with
//     a non-JSON body (a Vercel infra error such as 413/504 on a route that
//     DOES exist) falls back to the plain "stellar-hub returned <status>"
//     message instead - that's a real request failure on an up-to-date hub,
//     not a sign the endpoint is missing.
func (c *Client) classifyNewEndpointError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var payload struct {
		Error   string          `json:"error"`
		Code    string          `json:"code"`
		Details json.RawMessage `json:"details"`
		Missing []string        `json:"missing"`
	}
	jsonErr := json.Unmarshal(body, &payload)

	if jsonErr == nil && payload.Code != "" {
		return &APIError{Status: resp.StatusCode, Code: payload.Code, Message: term.SanitizeForTerminal(payload.Error), Missing: payload.Missing, Details: payload.Details}
	}

	isLegacyStatus := resp.StatusCode == http.StatusUnauthorized ||
		resp.StatusCode == http.StatusNotFound ||
		resp.StatusCode == http.StatusMethodNotAllowed

	if jsonErr == nil {
		// Valid JSON but no "code" at all. On 401/404/405 that's exactly a
		// pre-upgrade hub's legacy shape ({"error":"Unauthorized"} from the
		// old bearer-less routes, {"error":"Author not found"} from the old
		// author/slug parsing) -> hubTooOldError. On any OTHER status - a
		// raw 500 "Internal server error" and the like - an up-to-date hub
		// can still omit a code, and that must not be misreported as "doesn't
		// support CLI publishing yet"; it's a genuine failure on a route that
		// does exist.
		if isLegacyStatus {
			return &hubTooOldError{baseURL: c.baseURL}
		}
		return &APIError{Status: resp.StatusCode, Message: term.SanitizeForTerminal(payload.Error)}
	}

	// Not JSON at all: a 404/405 means the route doesn't exist (hubTooOld);
	// any other status with a non-JSON body (a Vercel infra error like
	// 413/504 on a route that DOES exist) is a real request failure, not a
	// missing-route signal.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return &hubTooOldError{baseURL: c.baseURL}
	}
	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		return &APIError{Status: resp.StatusCode, Message: "screenshot too large"}
	}
	return &APIError{Status: resp.StatusCode, Message: fmt.Sprintf("stellar-hub returned %d", resp.StatusCode)}
}
