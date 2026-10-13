// drafts.go implements the draft endpoints from the API contract's "Drafts"
// table. Every call here is bearer-only (see NewAuthedClient) and always
// scoped to the authenticated user - there is no "whose draft" parameter.
package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// DraftTheme is the theme a version draft belongs to.
type DraftTheme struct {
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	LatestVersion string `json:"latestVersion"`
	// HighestVersion is the highest version number this theme has ever had:
	// max(LatestVersion, the highest version number ever deleted from it). A
	// new version must beat THIS, not just LatestVersion - a deleted version
	// number can never be reused. Empty from an older hub that doesn't send
	// it yet, in which case callers should fall back to LatestVersion.
	HighestVersion string `json:"highestVersion"`
}

// Draft mirrors the contract's Draft shape exactly. Pointer fields are the
// ones the contract documents as nullable; Config is additionally omitted
// (not just null) outside a single-draft response, per
// "config only in single-draft responses".
type Draft struct {
	Code                string      `json:"code"`
	Kind                string      `json:"kind"`
	Status              string      `json:"status"`
	URL                 string      `json:"url"`
	Theme               *DraftTheme `json:"theme"`
	Name                *string     `json:"name"`
	Slug                *string     `json:"slug"`
	Description         *string     `json:"description"`
	Group               *string     `json:"group"`
	ColorMode           *string     `json:"colorMode"`
	ColorSchemeID       *string     `json:"colorSchemeId"`
	Dependencies        []string    `json:"dependencies"`
	MinStarshipVersion  *string     `json:"minStarshipVersion"`
	Version             *string     `json:"version"`
	VersionNotes        *string     `json:"versionNotes"`
	HasConfig           bool        `json:"hasConfig"`
	Config              *string     `json:"config,omitempty"`
	ConfigSource        *string     `json:"configSource"`
	ConfigUpdatedAt     *string     `json:"configUpdatedAt"`
	UnchangedFromLatest bool        `json:"unchangedFromLatest"`
	ScreenshotURL       *string     `json:"screenshotUrl"`
	Source              *string     `json:"source"`
	Missing             []string    `json:"missing"`
	CreatedAt           string      `json:"createdAt"`
	UpdatedAt           string      `json:"updatedAt"`
	ExpiresAt           string      `json:"expiresAt"`
}

// Draft status values (server-computed - see the contract's Draft.status
// comment).
const (
	DraftStatusWaitingForConfig = "waiting_for_config"
	DraftStatusIncomplete       = "incomplete"
	DraftStatusReady            = "ready"
)

// Draft kinds.
const (
	DraftKindTheme   = "theme"
	DraftKindVersion = "version"
)

// configSource values.
const (
	ConfigSourceCLI    = "cli"
	ConfigSourceWeb    = "web"
	ConfigSourceLatest = "latest"
)

// CreateDraftRequest is the body of POST /api/drafts. Exactly one of Slug
// (kind:"theme") or ThemeSlug (kind:"version") is set, matching the
// contract's two request shapes. Every field is a pointer with omitempty so
// an unset field is never sent at all, as the request structs convention
// requires.
type CreateDraftRequest struct {
	Kind      string  `json:"kind"`
	Slug      *string `json:"slug,omitempty"`
	ThemeSlug *string `json:"themeSlug,omitempty"`

	Name               *string   `json:"name,omitempty"`
	Description        *string   `json:"description,omitempty"`
	Group              *string   `json:"group,omitempty"`
	ColorMode          *string   `json:"colorMode,omitempty"`
	Dependencies       *[]string `json:"dependencies,omitempty"`
	MinStarshipVersion *string   `json:"minStarshipVersion,omitempty"`
	Version            *string   `json:"version,omitempty"`
	VersionNotes       *string   `json:"versionNotes,omitempty"`
	Config             *string   `json:"config,omitempty"`
	Source             *string   `json:"source,omitempty"`
}

// PatchDraftRequest is the body of PATCH /api/drafts/{code}. Every field is
// optional (omitted = unchanged); kind/themeSlug are immutable and so have
// no place here at all.
type PatchDraftRequest struct {
	Slug               *string   `json:"slug,omitempty"`
	Name               *string   `json:"name,omitempty"`
	Description        *string   `json:"description,omitempty"`
	Group              *string   `json:"group,omitempty"`
	ColorMode          *string   `json:"colorMode,omitempty"`
	ColorSchemeID      *string   `json:"colorSchemeId,omitempty"`
	Dependencies       *[]string `json:"dependencies,omitempty"`
	MinStarshipVersion *string   `json:"minStarshipVersion,omitempty"`
	Version            *string   `json:"version,omitempty"`
	VersionNotes       *string   `json:"versionNotes,omitempty"`
	Config             *string   `json:"config,omitempty"`
	Source             *string   `json:"source,omitempty"`
}

// PublishResponse is the 200 response from POST /api/drafts/{code}/publish.
type PublishResponse struct {
	Author  string `json:"author"`
	Slug    string `json:"slug"`
	Version string `json:"version"`
	URL     string `json:"url"`
}

// decodeDraft decodes resp's body as a single Draft, treating a decode
// failure or a response missing the two fields every draft always has
// (Code, Kind) as ErrHubTooOld - a 2xx with a shape this far off the
// contract means the route doesn't actually exist yet, not that the draft
// is somehow empty.
func (c *Client) decodeDraft(resp *http.Response) (*Draft, error) {
	var out Draft
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Code == "" || out.Kind == "" {
		return nil, &hubTooOldError{baseURL: c.baseURL}
	}
	return &out, nil
}

// doDraftsJSON issues a request to one of the drafts endpoints with an
// optional JSON body, decoding a successful response with decode (nil
// skips body decoding entirely, for 204 responses). It's the one place that
// does the do-then-status-branch dance shared by every method below.
func (c *Client) doDraftsJSON(method, url string, reqBody any, okStatus int, decode func(*http.Response) error) error {
	var body io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return wrapTransportError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != okStatus {
		return c.classifyNewEndpointError(resp)
	}
	if decode == nil {
		return nil
	}
	return decode(resp)
}

// ListDrafts calls GET /api/drafts, ordered by updatedAt desc per the
// contract (the hub's own ordering - nothing is re-sorted here).
func (c *Client) ListDrafts() ([]Draft, error) {
	var out struct {
		Drafts []Draft `json:"drafts"`
	}
	decoded := false
	err := c.doDraftsJSON(http.MethodGet, c.baseURL+"/api/drafts", nil, http.StatusOK, func(resp *http.Response) error {
		if jerr := json.NewDecoder(resp.Body).Decode(&out); jerr != nil {
			return &hubTooOldError{baseURL: c.baseURL}
		}
		decoded = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !decoded {
		return nil, &hubTooOldError{baseURL: c.baseURL}
	}
	return out.Drafts, nil
}

// CreateDraft calls POST /api/drafts.
func (c *Client) CreateDraft(req CreateDraftRequest) (*Draft, error) {
	var out *Draft
	err := c.doDraftsJSON(http.MethodPost, c.baseURL+"/api/drafts", req, http.StatusCreated, func(resp *http.Response) error {
		d, derr := c.decodeDraft(resp)
		if derr != nil {
			return derr
		}
		out = d
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetDraft calls GET /api/drafts/{code}. includeConfig false sends
// ?config=0, matching the wizard's own lightweight poll and letting the CLI
// avoid pulling a (possibly large) config string over the wire when it only
// needs status/metadata.
func (c *Client) GetDraft(code string, includeConfig bool) (*Draft, error) {
	url := c.baseURL + "/api/drafts/" + code
	if !includeConfig {
		url += "?config=0"
	}
	var out *Draft
	err := c.doDraftsJSON(http.MethodGet, url, nil, http.StatusOK, func(resp *http.Response) error {
		d, derr := c.decodeDraft(resp)
		if derr != nil {
			return derr
		}
		out = d
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PatchDraft calls PATCH /api/drafts/{code}.
func (c *Client) PatchDraft(code string, req PatchDraftRequest) (*Draft, error) {
	var out *Draft
	err := c.doDraftsJSON(http.MethodPatch, c.baseURL+"/api/drafts/"+code, req, http.StatusOK, func(resp *http.Response) error {
		d, derr := c.decodeDraft(resp)
		if derr != nil {
			return derr
		}
		out = d
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteScreenshot calls DELETE /api/drafts/{code}/screenshot, which (unlike
// deleting the draft itself) returns the updated Draft, not 204.
func (c *Client) DeleteScreenshot(code string) (*Draft, error) {
	var out *Draft
	err := c.doDraftsJSON(http.MethodDelete, c.baseURL+"/api/drafts/"+code+"/screenshot", nil, http.StatusOK, func(resp *http.Response) error {
		d, derr := c.decodeDraft(resp)
		if derr != nil {
			return derr
		}
		out = d
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteDraft calls DELETE /api/drafts/{code}, which returns 204 with no
// body.
func (c *Client) DeleteDraft(code string) error {
	return c.doDraftsJSON(http.MethodDelete, c.baseURL+"/api/drafts/"+code, nil, http.StatusNoContent, nil)
}

// PublishDraft calls POST /api/drafts/{code}/publish.
func (c *Client) PublishDraft(code string) (*PublishResponse, error) {
	var out PublishResponse
	decoded := false
	err := c.doDraftsJSON(http.MethodPost, c.baseURL+"/api/drafts/"+code+"/publish", nil, http.StatusOK, func(resp *http.Response) error {
		if jerr := json.NewDecoder(resp.Body).Decode(&out); jerr != nil ||
			out.Author == "" || out.Slug == "" || out.Version == "" {
			return &hubTooOldError{baseURL: c.baseURL}
		}
		decoded = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !decoded {
		return nil, &hubTooOldError{baseURL: c.baseURL}
	}
	return &out, nil
}
