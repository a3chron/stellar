package testutil

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
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
	Version       string
	ConfigContent string // The actual TOML content
	VersionNotes  string
	Dependencies  []string
	CreatedAt     string
}

// MockAPIHandler implements http.Handler for testing
type MockAPIHandler struct {
	mu             sync.Mutex
	themes         map[string]*MockTheme // key: "author/slug"
	DownloadCounts map[string]int        // Track download increments for verification
	RequestCounts  map[string]int        // Track requests received, keyed by r.URL.Path
}

// NewMockAPIHandler creates a new mock API handler
func NewMockAPIHandler() *MockAPIHandler {
	return &MockAPIHandler{
		themes:         make(map[string]*MockTheme),
		DownloadCounts: make(map[string]int),
		RequestCounts:  make(map[string]int),
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
	h.mu.Lock()
	h.RequestCounts[r.URL.Path]++
	h.mu.Unlock()

	// Parse path: /api/{author}/{slug} or /api/{author}/{slug}/{version}
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	parts := strings.Split(path, "/")

	if len(parts) == 1 && parts[0] == "themes" {
		h.handleSearchThemes(w, r)
		return
	}

	if len(parts) < 2 {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	author := parts[0]
	slug := parts[1]
	key := author + "/" + slug

	// Snapshot the theme under the mutex: handleIncrementDownload writes
	// theme.Downloads concurrently, so handlers must not read the shared
	// struct after unlocking (would race under go test -race).
	h.mu.Lock()
	stored, exists := h.themes[key]
	var theme MockTheme
	if exists {
		theme = *stored
	}
	h.mu.Unlock()

	if !exists {
		http.Error(w, "Theme not found", http.StatusNotFound)
		return
	}

	// Handle version endpoint: GET /api/{author}/{slug}/{version}
	if len(parts) == 3 {
		version := parts[2]
		h.handleVersionRequest(w, r, &theme, version)
		return
	}

	// Handle theme endpoint: GET/POST /api/{author}/{slug}
	switch r.Method {
	case http.MethodGet:
		h.handleGetTheme(w, &theme)
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
			"id":    authorID(theme.Author),
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

// clampQueryInt reads an integer query parameter, falling back to def for
// anything unparseable and clamping the rest into [min, max] - mirroring the
// hub's own handling of limit/offset.
func clampQueryInt(r *http.Request, key string, def, minVal, maxVal int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return max(minVal, min(parsed, maxVal))
}

// authorID derives an opaque author id from an author name. On the real hub
// these are unrelated - the id is a better-auth row id, while every
// /api/{author}/{slug} route resolves the author by exact name - so the mock
// must not let them be used interchangeably, or code that reads the wrong one
// would pass every test here and 404 in production.
func authorID(author string) string {
	return "user-" + strings.ToLower(author)
}

// handleSearchThemes implements GET /api/themes?authorName=<prefix>, used by
// shell completion (internal/completion) to look up hub authors/themes
// without downloading a full ThemeInfo per candidate. It filters registered
// themes by a case-insensitive prefix match on author name, and orders the
// result by theme name (matching the hub's sort=name, which is what the
// client requests), tie-broken by author so map iteration order can't leak
// into a test.
func (h *MockAPIHandler) handleSearchThemes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	authorPrefix := strings.ToLower(r.URL.Query().Get("authorName"))

	// Copy matching themes under the mutex (see ServeHTTP for why).
	h.mu.Lock()
	var matches []MockTheme
	for _, theme := range h.themes {
		if authorPrefix != "" && !strings.HasPrefix(strings.ToLower(theme.Author), authorPrefix) {
			continue
		}
		matches = append(matches, *theme)
	}
	h.mu.Unlock()

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Name != matches[j].Name {
			return matches[i].Name < matches[j].Name
		}
		return matches[i].Author < matches[j].Author
	})

	themesJSON := make([]map[string]interface{}, len(matches))
	for i, t := range matches {
		// The hub omits latestVersion entirely for a theme with no versions
		// (JSON.stringify drops an undefined value).
		theme := map[string]interface{}{
			"id": t.ID,
			"author": map[string]interface{}{
				"id":    authorID(t.Author),
				"name":  t.Author,
				"image": nil,
			},
			"name":          t.Name,
			"slug":          t.Slug,
			"description":   t.Description,
			"screenshotUrl": "https://example.com/" + t.Slug + ".png",
			"downloads":     t.Downloads,
			"colorScheme":   t.ColorScheme,
			"createdAt":     t.CreatedAt,
			"updatedAt":     t.UpdatedAt,
		}
		if len(t.Versions) > 0 {
			theme["latestVersion"] = t.Versions[0].Version
		}
		themesJSON[i] = theme
	}

	// Honour limit/offset the way the hub does (default 20, capped at 100),
	// so a test can't get a page size the real API could never return.
	limit := clampQueryInt(r, "limit", 20, 1, 100)
	offset := clampQueryInt(r, "offset", 0, 0, len(themesJSON))
	themesJSON = themesJSON[offset:min(offset+limit, len(themesJSON))]

	response := map[string]interface{}{
		"themes": themesJSON,
		"pagination": map[string]interface{}{
			"total":  len(themesJSON),
			"limit":  limit,
			"offset": offset,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
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

// Requests returns how many requests the handler has received for path
// (matched against r.URL.Path exactly, e.g. "/api/testuser/sample-theme").
func (h *MockAPIHandler) Requests(path string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.RequestCounts[path]
}

// TotalRequests returns the total number of requests received across all paths.
func (h *MockAPIHandler) TotalRequests() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	total := 0
	for _, count := range h.RequestCounts {
		total += count
	}
	return total
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

	// Add a theme from a second author, used by shell completion tests to
	// exercise "unknown author prefix -> hub-only suggestions" behavior.
	handler.AddTheme(MockTheme{
		ID:          "test-id-3",
		Author:      "otheruser",
		Slug:        "ocean-theme",
		Name:        "Ocean Theme",
		Description: "A calming ocean theme",
		Downloads:   5,
		Group:       "nature",
		CreatedAt:   "2024-02-01T00:00:00Z",
		UpdatedAt:   "2024-02-05T00:00:00Z",
		Versions: []MockVersion{
			{
				Version:       "2.1",
				ConfigContent: SampleTOML(),
				VersionNotes:  "Latest version",
				Dependencies:  []string{},
				CreatedAt:     "2024-02-05T00:00:00Z",
			},
			{
				Version:       "2.0",
				ConfigContent: SampleTOML(),
				VersionNotes:  "Initial release",
				Dependencies:  []string{},
				CreatedAt:     "2024-02-01T00:00:00Z",
			},
		},
	})

	return handler
}
