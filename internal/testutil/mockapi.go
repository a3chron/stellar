package testutil

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// MockTheme represents a theme in the mock API
type MockTheme struct {
	ID          string
	Author      string
	Slug        string
	Name        string
	Description string
	Downloads   int
	ColorScheme *string
	Group       string
	Versions    []MockVersion
	CreatedAt   string
	UpdatedAt   string
}

// MockVersion represents a theme version
type MockVersion struct {
	Version      string
	ConfigContent string // The actual TOML content
	VersionNotes string
	Dependencies []string
	CreatedAt    string
}

// MockAPIHandler implements http.Handler for testing
type MockAPIHandler struct {
	mu             sync.Mutex
	themes         map[string]*MockTheme // key: "author/slug"
	DownloadCounts map[string]int        // Track download increments for verification
}

// NewMockAPIHandler creates a new mock API handler
func NewMockAPIHandler() *MockAPIHandler {
	return &MockAPIHandler{
		themes:         make(map[string]*MockTheme),
		DownloadCounts: make(map[string]int),
	}
}

// AddTheme adds a theme to the mock API
func (h *MockAPIHandler) AddTheme(theme MockTheme) {
	h.mu.Lock()
	defer h.mu.Unlock()

	key := theme.Author + "/" + theme.Slug
	h.themes[key] = &theme
	h.DownloadCounts[key] = theme.Downloads
}

// ServeHTTP implements http.Handler
func (h *MockAPIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/{author}/{slug} or /api/{author}/{slug}/{version}
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	parts := strings.Split(path, "/")

	if len(parts) < 2 {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	author := parts[0]
	slug := parts[1]
	key := author + "/" + slug

	h.mu.Lock()
	theme, exists := h.themes[key]
	h.mu.Unlock()

	if !exists {
		http.Error(w, "Theme not found", http.StatusNotFound)
		return
	}

	// Handle version endpoint: GET /api/{author}/{slug}/{version}
	if len(parts) == 3 {
		version := parts[2]
		h.handleVersionRequest(w, r, theme, version)
		return
	}

	// Handle theme endpoint: GET/POST /api/{author}/{slug}
	switch r.Method {
	case http.MethodGet:
		h.handleGetTheme(w, theme)
	case http.MethodPost:
		h.handleIncrementDownload(w, key)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetTheme returns theme info
func (h *MockAPIHandler) handleGetTheme(w http.ResponseWriter, theme *MockTheme) {
	response := map[string]interface{}{
		"id":          theme.ID,
		"slug":        theme.Slug,
		"name":        theme.Name,
		"description": theme.Description,
		"downloads":   theme.Downloads,
		"colorScheme": theme.ColorScheme,
		"group":       theme.Group,
		"createdAt":   theme.CreatedAt,
		"updatedAt":   theme.UpdatedAt,
		"author": map[string]interface{}{
			"id":    theme.Author,
			"name":  theme.Author,
			"image": nil,
			"bio":   nil,
		},
		"versions": h.formatVersions(theme.Versions),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// formatVersions converts MockVersions to API format
func (h *MockAPIHandler) formatVersions(versions []MockVersion) []map[string]interface{} {
	result := make([]map[string]interface{}, len(versions))
	for i, v := range versions {
		result[i] = map[string]interface{}{
			"version":      v.Version,
			"versionNotes": v.VersionNotes,
			"dependencies": v.Dependencies,
			"createdAt":    v.CreatedAt,
		}
	}
	return result
}

// handleVersionRequest returns the config content for a specific version
func (h *MockAPIHandler) handleVersionRequest(w http.ResponseWriter, r *http.Request, theme *MockTheme, version string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Handle "latest" version
	if version == "latest" && len(theme.Versions) > 0 {
		version = theme.Versions[0].Version
	}

	// Find the version
	for _, v := range theme.Versions {
		if v.Version == version {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(v.ConfigContent))
			return
		}
	}

	http.Error(w, "Version not found", http.StatusNotFound)
}

// handleIncrementDownload increments the download count
func (h *MockAPIHandler) handleIncrementDownload(w http.ResponseWriter, key string) {
	h.mu.Lock()
	h.DownloadCounts[key]++
	if theme, exists := h.themes[key]; exists {
		theme.Downloads = h.DownloadCounts[key]
	}
	h.mu.Unlock()

	// Return 204 No Content (matching the real API)
	w.WriteHeader(http.StatusNoContent)
}

// GetDownloadCount returns the current download count for a theme
func (h *MockAPIHandler) GetDownloadCount(author, slug string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.DownloadCounts[author+"/"+slug]
}

// CreateDefaultMockAPI creates a mock API with sample themes for testing
func CreateDefaultMockAPI() *MockAPIHandler {
	handler := NewMockAPIHandler()

	// Add a sample theme
	handler.AddTheme(MockTheme{
		ID:          "test-id-1",
		Author:      "testuser",
		Slug:        "sample-theme",
		Name:        "Sample Theme",
		Description: "A sample theme for testing",
		Downloads:   42,
		Group:       "minimal",
		CreatedAt:   "2024-01-01T00:00:00Z",
		UpdatedAt:   "2024-01-15T00:00:00Z",
		Versions: []MockVersion{
			{
				Version:       "1.2",
				ConfigContent: SampleTOML(),
				VersionNotes:  "Latest version",
				Dependencies:  []string{},
				CreatedAt:     "2024-01-15T00:00:00Z",
			},
			{
				Version:       "1.1",
				ConfigContent: SampleTOML(),
				VersionNotes:  "Previous version",
				Dependencies:  []string{},
				CreatedAt:     "2024-01-10T00:00:00Z",
			},
			{
				Version:       "1.0",
				ConfigContent: SampleTOML(),
				VersionNotes:  "Initial release",
				Dependencies:  []string{},
				CreatedAt:     "2024-01-01T00:00:00Z",
			},
		},
	})

	// Add a theme with custom commands for security warning tests
	handler.AddTheme(MockTheme{
		ID:          "test-id-2",
		Author:      "testuser",
		Slug:        "custom-theme",
		Name:        "Custom Theme",
		Description: "A theme with custom commands",
		Downloads:   10,
		Group:       "poweruser",
		CreatedAt:   "2024-01-01T00:00:00Z",
		UpdatedAt:   "2024-01-01T00:00:00Z",
		Versions: []MockVersion{
			{
				Version:       "1.0",
				ConfigContent: SampleTOMLWithCustom(),
				VersionNotes:  "Contains custom commands",
				Dependencies:  []string{},
				CreatedAt:     "2024-01-01T00:00:00Z",
			},
		},
	})

	return handler
}
