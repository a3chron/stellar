package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/a3chron/stellar/internal/paths"
)

const BaseURL = "https://stellar-hub.vercel.app"

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

// NewCompletionClient creates a client tuned for shell completion requests.
// Shell completion runs synchronously on every keystroke in the user's
// shell, so it must never block waiting on a slow or unreachable
// stellar-hub: callers are expected to treat any error from this client as
// "degrade to local-only completions" rather than surfacing it.
func NewCompletionClient() *Client {
	return newClient(paths.APIURL(BaseURL), 2*time.Second)
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
