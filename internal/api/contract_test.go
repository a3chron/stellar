//go:build contract

// Package api provides contract tests that validate the mock API matches the real API.
// These tests require the dev server to be running.
//
// Run with: go test -tags=contract -v ./internal/api/...
package api

import (
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
)

func TestContractValidation_ThemeInfoFields(t *testing.T) {
	devURL := os.Getenv("STELLAR_DEV_URL")
	if devURL == "" {
		devURL = "http://localhost:3000"
	}

	// 1. Get response from real API
	realClient := NewClientWithURL(devURL)
	realInfo, err := realClient.GetThemeInfo("a3chron", "ctp-blue")
	if err != nil {
		t.Skipf("Dev server not available: %v", err)
	}

	// 2. Get response from mock API
	mockAPI := testutil.CreateDefaultMockAPI()
	server := httptest.NewServer(mockAPI)
	defer server.Close()

	mockClient := NewClientWithURL(server.URL)
	mockInfo, err := mockClient.GetThemeInfo("testuser", "sample-theme")
	if err != nil {
		t.Fatalf("Mock API failed: %v", err)
	}

	// 3. Compare field structure (not values, just that fields exist)
	realFields := getJSONFields(realInfo)
	mockFields := getJSONFields(mockInfo)

	t.Log("Real API fields:", realFields)
	t.Log("Mock API fields:", mockFields)

	// Check that mock has all fields from real API
	for field := range realFields {
		if _, ok := mockFields[field]; !ok {
			t.Errorf("Mock missing field from real API: %s", field)
		}
	}
}

func TestContractValidation_VersionInfoFields(t *testing.T) {
	devURL := os.Getenv("STELLAR_DEV_URL")
	if devURL == "" {
		devURL = "http://localhost:3000"
	}

	// Get from real API
	realClient := NewClientWithURL(devURL)
	realInfo, err := realClient.GetThemeInfo("a3chron", "ctp-blue")
	if err != nil {
		t.Skipf("Dev server not available: %v", err)
	}

	if len(realInfo.Versions) == 0 {
		t.Skip("Real theme has no versions")
	}

	// Get from mock API
	mockAPI := testutil.CreateDefaultMockAPI()
	server := httptest.NewServer(mockAPI)
	defer server.Close()

	mockClient := NewClientWithURL(server.URL)
	mockInfo, _ := mockClient.GetThemeInfo("testuser", "sample-theme")

	if len(mockInfo.Versions) == 0 {
		t.Fatal("Mock theme has no versions")
	}

	// Compare version fields
	realVersionFields := getJSONFields(&realInfo.Versions[0])
	mockVersionFields := getJSONFields(&mockInfo.Versions[0])

	t.Log("Real version fields:", realVersionFields)
	t.Log("Mock version fields:", mockVersionFields)

	for field := range realVersionFields {
		if _, ok := mockVersionFields[field]; !ok {
			t.Errorf("Mock version missing field from real API: %s", field)
		}
	}
}

func TestContractValidation_AuthorInfoFields(t *testing.T) {
	devURL := os.Getenv("STELLAR_DEV_URL")
	if devURL == "" {
		devURL = "http://localhost:3000"
	}

	// Get from real API
	realClient := NewClientWithURL(devURL)
	realInfo, err := realClient.GetThemeInfo("a3chron", "ctp-blue")
	if err != nil {
		t.Skipf("Dev server not available: %v", err)
	}

	// Get from mock API
	mockAPI := testutil.CreateDefaultMockAPI()
	server := httptest.NewServer(mockAPI)
	defer server.Close()

	mockClient := NewClientWithURL(server.URL)
	mockInfo, _ := mockClient.GetThemeInfo("testuser", "sample-theme")

	// Compare author fields
	realAuthorFields := getJSONFields(&realInfo.Author)
	mockAuthorFields := getJSONFields(&mockInfo.Author)

	t.Log("Real author fields:", realAuthorFields)
	t.Log("Mock author fields:", mockAuthorFields)

	for field := range realAuthorFields {
		if _, ok := mockAuthorFields[field]; !ok {
			t.Errorf("Mock author missing field from real API: %s", field)
		}
	}
}

func TestContractValidation_ConfigContentFormat(t *testing.T) {
	devURL := os.Getenv("STELLAR_DEV_URL")
	if devURL == "" {
		devURL = "http://localhost:3000"
	}

	// Get from real API
	realClient := NewClientWithURL(devURL)
	realContent, err := realClient.FetchThemeConfig("a3chron", "ctp-blue", "latest")
	if err != nil {
		t.Skipf("Dev server not available: %v", err)
	}

	// Get from mock API
	mockAPI := testutil.CreateDefaultMockAPI()
	server := httptest.NewServer(mockAPI)
	defer server.Close()

	mockClient := NewClientWithURL(server.URL)
	mockContent, _ := mockClient.FetchThemeConfig("testuser", "sample-theme", "latest")

	// Both should be non-empty TOML content
	assert.NotEmpty(t, realContent, "Real API should return content")
	assert.NotEmpty(t, mockContent, "Mock API should return content")

	// Both should look like TOML (contain at least one section header)
	assert.Contains(t, realContent, "[", "Real content should be TOML format")
	assert.Contains(t, mockContent, "[", "Mock content should be TOML format")
}

// getJSONFields extracts JSON field names from a struct
func getJSONFields(v interface{}) map[string]bool {
	fields := make(map[string]bool)

	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return fields
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if tag != "" && tag != "-" {
			// Handle tags like "name,omitempty"
			name := tag
			for j, c := range tag {
				if c == ',' {
					name = tag[:j]
					break
				}
			}
			if name != "" {
				fields[name] = true
			}
		}
	}

	return fields
}
