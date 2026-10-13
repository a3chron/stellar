// Package auth stores and retrieves the CLI's bearer token(s) for
// stellar-hub, at <StellarHome>/credentials.json (see
// internal/paths.CredentialsPath).
//
// The file holds a map keyed by base URL (scheme and host, lowercased, no
// trailing slash) rather than a single token, so a token saved against
// STELLAR_API_URL=http://127.0.0.1:3000 during development never collides
// with (or gets clobbered by) one saved against the real hub.
package auth

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/a3chron/stellar/internal/paths"
)

// Entry is one base URL's stored credential.
type Entry struct {
	Token string `json:"token"`
}

// store is the on-disk shape of credentials.json: base URL -> Entry.
type store map[string]Entry

// NormalizeBaseURL reduces a base URL to the key credentials.json stores it
// under: scheme and host only, lowercased, with no trailing slash. Exported
// so callers that build a display/lookup key (e.g. cmd/auth.go) agree with
// this package on what "the same hub" means.
func NormalizeBaseURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		// Not a parseable absolute URL - fall back to a best-effort
		// normalization rather than failing outright, so a malformed
		// STELLAR_API_URL still gets a stable (if unhelpful) key.
		return strings.ToLower(strings.TrimRight(trimmed, "/"))
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}

// load reads credentials.json, returning an empty store (not an error) when
// the file doesn't exist yet - the common case before the first login.
func load() (store, error) {
	path, err := paths.CredentialsPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return store{}, nil
		}
		return nil, err
	}

	var s store
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s == nil {
		s = store{}
	}
	return s, nil
}

// save writes s to credentials.json via a temp file + rename, so a crash or
// power loss mid-write can never leave a truncated/corrupt credentials file
// in place. os.CreateTemp creates the file with mode 0600, which the rename
// preserves - credentials.json is never world- or group-readable on Unix.
func save(s store) (err error) {
	path, err := paths.CredentialsPath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".credentials-*.json.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	// closed tracks whether the explicit tmp.Close() below already ran, so
	// the deferred cleanup never closes it a second time: Close() on an
	// already-closed *os.File returns os.ErrClosed, which the deferred
	// assignment below would otherwise record into err even after a fully
	// successful write+rename, making every save (and therefore every
	// `stellar auth` login) report a spurious "file already closed" error.
	closed := false
	defer func() {
		if !closed {
			if cerr := tmp.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}
		// Best-effort: only a leftover temp file if the rename below
		// succeeded; harmless either way since a stale .tmp file is never
		// read by load().
		_ = os.Remove(tmpPath)
	}()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	closed = true
	return os.Rename(tmpPath, path)
}

// Token returns the bearer token to use for baseURL. STELLAR_TOKEN, when
// set, always wins (so CI can authenticate without ever writing
// credentials.json) - the stored token is only consulted when it's unset.
// Returns "" (no error) when nothing is stored for this hub.
func Token(baseURL string) (string, error) {
	if t := os.Getenv(paths.EnvToken); t != "" {
		return t, nil
	}
	return StoredToken(baseURL)
}

// StoredToken returns the token actually saved in credentials.json for
// baseURL, ignoring STELLAR_TOKEN entirely. Use this wherever an operation
// must act on the real stored credential specifically - revoking it on
// logout/uninstall, say - so a CI-only STELLAR_TOKEN override can never be
// mistaken for (and revoked/deleted as though it were) a login that was
// actually saved to disk. Returns "" (no error) when nothing is stored for
// this hub.
func StoredToken(baseURL string) (string, error) {
	s, err := load()
	if err != nil {
		return "", err
	}
	e, ok := s[NormalizeBaseURL(baseURL)]
	if !ok {
		return "", nil
	}
	return e.Token, nil
}

// StoredEntry pairs a normalized base URL with its saved token, for
// iterating every hub credentials.json currently has a login for (see
// StoredEntries).
type StoredEntry struct {
	BaseURL string
	Token   string
}

// StoredEntries returns every base URL/token pair currently saved in
// credentials.json, in no particular order. Used by `stellar uninstall` to
// revoke every stored login (not just whichever one STELLAR_API_URL/
// STELLAR_TOKEN currently points at) before the file holding them is
// removed.
func StoredEntries() ([]StoredEntry, error) {
	s, err := load()
	if err != nil {
		return nil, err
	}
	out := make([]StoredEntry, 0, len(s))
	for baseURL, e := range s {
		out = append(out, StoredEntry{BaseURL: baseURL, Token: e.Token})
	}
	return out, nil
}

// Save stores token for baseURL, replacing any existing entry for that hub.
func Save(baseURL, token string) error {
	s, err := load()
	if err != nil {
		s = store{}
	}
	s[NormalizeBaseURL(baseURL)] = Entry{Token: token}
	return save(s)
}

// Delete removes the stored entry for baseURL, if any. Deleting an absent
// entry is not an error.
func Delete(baseURL string) error {
	s, err := load()
	if err != nil {
		return err
	}
	key := NormalizeBaseURL(baseURL)
	if _, ok := s[key]; !ok {
		return nil
	}
	delete(s, key)
	return save(s)
}
