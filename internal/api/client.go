package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const BaseURL = "https://stellar.vercel.app"

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

type ThemeInfo struct {
	ID          string        `json:"id"`
	Author      string        `json:"author"`
	Name        string        `json:"name"`
	Slug        string        `json:"slug"`
	Description string        `json:"description"`
	Downloads   int           `json:"downloads"`
	Versions    []VersionInfo `json:"versions"`
}

type VersionInfo struct {
	Version      string                   `json:"version"`
	CreatedAt    string                   `json:"created_at"`
	VersionNotes string                   `json:"version_notes"`
	Dependencies []map[string]interface{} `json:"dependencies"`
}

func (c *Client) FetchThemeConfig(author, name, version string) (string, error) {
	url := fmt.Sprintf("%s/api/themes/%s/%s/%s/download", c.baseURL, author, name, version)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch theme: %w", err)
	}
	defer resp.Body.Close()

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
	url := fmt.Sprintf("%s/api/themes/%s/%s", c.baseURL, author, name)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

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
	url := fmt.Sprintf("%s/api/themes/download-count", c.baseURL)

	// Simple POST (rate-limited on server)
	resp, err := c.httpClient.Post(url, "application/json",
		strings.NewReader(fmt.Sprintf(`{"author":"%s","name":"%s"}`, author, name)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
