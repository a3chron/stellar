//go:build integration

// Package api provides integration tests that run against the real stellar-hub API.
// These tests require the dev server to be running at localhost:3000 (or STELLAR_DEV_URL).
//
// Run with: go test -tags=integration -v ./internal/api/...
//
// NOTE: We intentionally do NOT test IncrementDownloadCount against
// the real API to avoid inflating download counts.
package api

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getDevURL() string {
	if url := os.Getenv("STELLAR_DEV_URL"); url != "" {
		return url
	}
	return "http://localhost:3000"
}

func TestRealAPI_GetThemeInfo(t *testing.T) {
	devURL := getDevURL()
	client := NewClientWithURL(devURL)

	// Try to get info for a known theme
	// If dev server isn't running or theme doesn't exist, skip the test
	info, err := client.GetThemeInfo("a3chron", "ctp-blue")
	if err != nil {
		t.Skipf("Dev server not available or theme not found: %v", err)
	}

	// Validate response structure
	assert.NotEmpty(t, info.ID, "Expected non-empty ID")
	assert.Equal(t, "a3chron", info.Author.Name, "Expected author name a3chron")
	assert.Equal(t, "ctp-blue", info.Slug, "Expected slug ctp-blue")
	assert.NotEmpty(t, info.Name, "Expected non-empty name")
	assert.GreaterOrEqual(t, len(info.Versions), 1, "Expected at least one version")

	// Validate version structure
	if len(info.Versions) > 0 {
		v := info.Versions[0]
		assert.NotEmpty(t, v.Version, "Expected non-empty version")
		assert.NotEmpty(t, v.CreatedAt, "Expected non-empty createdAt")
	}
}

func TestRealAPI_FetchThemeConfig(t *testing.T) {
	devURL := getDevURL()
	client := NewClientWithURL(devURL)

	// First get theme info to know what version exists
	info, err := client.GetThemeInfo("a3chron", "ctp-blue")
	if err != nil {
		t.Skipf("Dev server not available or theme not found: %v", err)
	}

	require.NotEmpty(t, info.Versions, "Expected at least one version")
	version := info.Versions[0].Version

	// Fetch the config content
	content, err := client.FetchThemeConfig("a3chron", "ctp-blue", version)
	require.NoError(t, err)

	// Validate it's valid TOML-like content
	assert.NotEmpty(t, content, "Expected non-empty config content")
	// Starship configs typically have these sections
	assert.True(t,
		containsAny(content, "[character]", "[format]", "[palette]"),
		"Expected config to contain starship sections",
	)
}

func TestRealAPI_FetchLatestVersion(t *testing.T) {
	devURL := getDevURL()
	client := NewClientWithURL(devURL)

	// Fetch using "latest" version
	content, err := client.FetchThemeConfig("a3chron", "ctp-blue", "latest")
	if err != nil {
		t.Skipf("Dev server not available or theme not found: %v", err)
	}

	assert.NotEmpty(t, content, "Expected non-empty config content")
}

func TestRealAPI_NonexistentTheme(t *testing.T) {
	devURL := getDevURL()
	client := NewClientWithURL(devURL)

	// Try a nonexistent theme
	_, err := client.GetThemeInfo("nonexistent-user-12345", "nonexistent-theme-67890")
	if err == nil {
		t.Skip("Expected error for nonexistent theme, but got success (dev server may be mocked)")
	}

	assert.Contains(t, err.Error(), "not found")
}

// Helper to check if content contains any of the given substrings
func containsAny(content string, substrings ...string) bool {
	for _, s := range substrings {
		if len(content) > 0 && len(s) > 0 {
			for i := 0; i <= len(content)-len(s); i++ {
				if content[i:i+len(s)] == s {
					return true
				}
			}
		}
	}
	return false
}
