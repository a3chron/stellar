package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/a3chron/stellar/internal/paths"
)

const BaseURL = "https://stellar.a3chron.dev"

type Client struct {
	baseURL    string
	httpClient *http.Client
}

// newClient builds a Client pointed at baseURL with the given request
// timeout. It's the single place that assembles the http.Client so
// NewClient, NewClientWithURL and NewCompletionClient can't drift apart on
// anything but the two knobs that actually differ between them.
func newClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func NewClient() *Client {
	return newClient(paths.APIURL(BaseURL), 30*time.Second)
}

// NewClientWithURL creates a client with a specific base URL (for testing)
func NewClientWithURL(baseURL string) *Client {
	return newClient(baseURL, 30*time.Second)
}

// Timeouts for the two shell-completion clients. Shell completion runs
// synchronously in the user's shell on every TAB, so it must never block
// waiting on a slow or unreachable stellar-hub: callers are expected to
// treat any error from these clients as "degrade to local-only completions"
// rather than surfacing it.
const (
	// completionTimeout bounds a lookup the user explicitly opted in to with
	// STELLAR_COMPLETION_ONLINE=1. They asked for hub suggestions on every
	// completion, so it's worth waiting a bit longer to actually get them.
	completionTimeout = 2 * time.Second
	// fallbackCompletionTimeout bounds the on-by-default lookup that fires
	// only when the local cache had nothing to offer. Nobody asked for that
	// round trip, so it has to be invisible when it fails: a warm hub answers
	// in a few hundred milliseconds, and anything past ~1s reads as a hung
	// terminal.
	fallbackCompletionTimeout = 800 * time.Millisecond
)

// NewCompletionClient creates a client for opt-in hub completion
// (STELLAR_COMPLETION_ONLINE=1), which queries the hub on every completion.
func NewCompletionClient() *Client {
	return newClient(paths.APIURL(BaseURL), completionTimeout)
}

// NewFallbackCompletionClient creates a client for the default
// local-then-hub completion path, which queries the hub only when the local
// cache produced no candidates. It gets a tighter budget than
// NewCompletionClient because the user never asked for the round trip.
func NewFallbackCompletionClient() *Client {
	return newClient(paths.APIURL(BaseURL), fallbackCompletionTimeout)
}

// NewTelemetryClient creates a client for the anonymous install report. The
// report rides along an ordinary user command, so it gets the same 2s budget
// as completion: a slow hub may cost the user a moment, never the command.
func NewTelemetryClient() *Client {
	return newClient(paths.APIURL(BaseURL), 2*time.Second)
}

// CLIPing is the anonymous install report sent to POST /api/cli/ping. Every
// field is random (ID), an enum, or a version string; nothing in it can
// identify a person or a machine. The hub upserts by ID, so a retried report
// is idempotent.
type CLIPing struct {
	ID string `json:"id"`
	// Kind is "install" or "existing" (see config.Config.InstallKind).
	Kind string `json:"kind"`
	// Event is "report" (first run or version change) or "uninstall".
	Event string `json:"event"`
	// Version is the running CLI version without a "v" prefix.
	Version string `json:"version"`
	// Previous is the last version the hub accepted, "" on the first report.
	Previous string `json:"previous"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
}

// SendCLIPing posts p to the hub. Any non-2xx status is an error, so callers
// leave their "reported" marker untouched and retry on the next run.
func (c *Client) SendCLIPing(p CLIPing) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/cli/ping", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cli ping rejected (status: %d)", resp.StatusCode)
	}

	return nil
}

// Author info nested in theme response
type AuthorInfo struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Image *string `json:"image"`
	Bio   *string `json:"bio"`
}

// Theme info from API
type ThemeInfo struct {
	ID          string        `json:"id"`
	Author      AuthorInfo    `json:"author"`
	Name        string        `json:"name"`
	Slug        string        `json:"slug"`
	Description string        `json:"description"`
	Downloads   int           `json:"downloads"`
	ColorScheme *string       `json:"colorScheme"`
	Group       string        `json:"group"`
	Versions    []VersionInfo `json:"versions"`
	CreatedAt   string        `json:"createdAt"`
	UpdatedAt   string        `json:"updatedAt"`
}

// Version info
type VersionInfo struct {
	Version      string   `json:"version"`
	VersionNotes string   `json:"versionNotes"`
	Dependencies []string `json:"dependencies"`
	CreatedAt    string   `json:"createdAt"`
}

// ThemeSummary is the lightweight theme shape returned by GET /api/themes,
// used by shell completion (internal/completion) to look up hub authors and
// theme slugs without downloading a full ThemeInfo per candidate.
type ThemeSummary struct {
	Author        AuthorInfo `json:"author"`
	Name          string     `json:"name"`
	Slug          string     `json:"slug"`
	LatestVersion string     `json:"latestVersion"`
}

func (c *Client) FetchThemeConfig(author, name, version string) (string, error) {
	url := fmt.Sprintf("%s/api/%s/%s/%s", c.baseURL, author, name, version)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch theme: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("theme version not found")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func (c *Client) GetThemeInfo(author, name string) (*ThemeInfo, error) {
	url := fmt.Sprintf("%s/api/%s/%s", c.baseURL, author, name)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("theme not found (status: %d)", resp.StatusCode)
	}

	var info ThemeInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	return &info, nil
}

// SearchThemesByAuthorName queries GET /api/themes for themes whose author
// name matches authorName as a prefix (server-side match), used by shell
// completion to suggest hub authors/themes that aren't in the local cache.
func (c *Client) SearchThemesByAuthorName(authorName string) ([]ThemeSummary, error) {
	reqURL := fmt.Sprintf("%s/api/themes?authorName=%s&limit=100&sort=name", c.baseURL, url.QueryEscape(authorName))

	resp, err := c.httpClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("failed to search themes: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var result struct {
		Themes []ThemeSummary `json:"themes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Themes, nil
}

func (c *Client) IncrementDownloadCount(author, name string) error {
	url := fmt.Sprintf("%s/api/%s/%s", c.baseURL, author, name)

	// Simple POST to increment download count
	resp, err := c.httpClient.Post(url, "application/json", nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusCreated &&
		resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to increment download count (status: %d)", resp.StatusCode)
	}

	return nil
}
