package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const BaseURL = "https://stellar-hub.vercel.app"

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		baseURL: BaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
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

func (c *Client) FetchThemeConfig(author, name, version string) (string, error) {
	url := fmt.Sprintf("%s/api/%s/%s/%s", c.baseURL, author, name, version)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch theme: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

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
		return nil, fmt.Errorf("theme not found")
	}

	var info ThemeInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	return &info, nil
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

	return nil
}
