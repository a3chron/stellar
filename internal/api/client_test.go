package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a3chron/stellar/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	// Without env override
	client := NewClient()
	assert.Equal(t, BaseURL, client.baseURL)
	assert.NotNil(t, client.httpClient)
}

func TestNewClientWithURL(t *testing.T) {
	customURL := "http://localhost:3000"
	client := NewClientWithURL(customURL)
	assert.Equal(t, customURL, client.baseURL)
}

func TestClient_FetchThemeConfig(t *testing.T) {
	mockAPI := testutil.CreateDefaultMockAPI()
	server := httptest.NewServer(mockAPI)
	defer server.Close()

	client := NewClientWithURL(server.URL)

	t.Run("success", func(t *testing.T) {
		content, err := client.FetchThemeConfig("testuser", "sample-theme", "1.2")
		require.NoError(t, err)
		assert.Contains(t, content, "[character]")
	})

	t.Run("latest version", func(t *testing.T) {
		content, err := client.FetchThemeConfig("testuser", "sample-theme", "latest")
		require.NoError(t, err)
		assert.Contains(t, content, "[character]")
	})

	t.Run("nonexistent theme", func(t *testing.T) {
		_, err := client.FetchThemeConfig("nobody", "nothing", "1.0")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("nonexistent version", func(t *testing.T) {
		_, err := client.FetchThemeConfig("testuser", "sample-theme", "99.0")
		assert.Error(t, err)
	})
}

func TestClient_GetThemeInfo(t *testing.T) {
	mockAPI := testutil.CreateDefaultMockAPI()
	server := httptest.NewServer(mockAPI)
	defer server.Close()

	client := NewClientWithURL(server.URL)

	t.Run("success", func(t *testing.T) {
		info, err := client.GetThemeInfo("testuser", "sample-theme")
		require.NoError(t, err)
		require.NotNil(t, info)

		assert.Equal(t, "test-id-1", info.ID)
		assert.Equal(t, "Sample Theme", info.Name)
		assert.Equal(t, "sample-theme", info.Slug)
		assert.Equal(t, "testuser", info.Author.Name)
		assert.Equal(t, 42, info.Downloads)
		assert.Len(t, info.Versions, 3)
		assert.Equal(t, "1.2", info.Versions[0].Version)
	})

	t.Run("nonexistent theme", func(t *testing.T) {
		_, err := client.GetThemeInfo("nobody", "nothing")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestClient_IncrementDownloadCount(t *testing.T) {
	mockAPI := testutil.CreateDefaultMockAPI()
	server := httptest.NewServer(mockAPI)
	defer server.Close()

	client := NewClientWithURL(server.URL)

	t.Run("success", func(t *testing.T) {
		initialCount := mockAPI.GetDownloadCount("testuser", "sample-theme")

		err := client.IncrementDownloadCount("testuser", "sample-theme")
		require.NoError(t, err)

		newCount := mockAPI.GetDownloadCount("testuser", "sample-theme")
		assert.Equal(t, initialCount+1, newCount)
	})

	t.Run("multiple increments", func(t *testing.T) {
		initialCount := mockAPI.GetDownloadCount("testuser", "sample-theme")

		for i := 0; i < 3; i++ {
			err := client.IncrementDownloadCount("testuser", "sample-theme")
			require.NoError(t, err)
		}

		newCount := mockAPI.GetDownloadCount("testuser", "sample-theme")
		assert.Equal(t, initialCount+3, newCount)
	})

	t.Run("nonexistent theme", func(t *testing.T) {
		err := client.IncrementDownloadCount("nobody", "nothing")
		assert.Error(t, err)
	})
}

func TestClient_NetworkError(t *testing.T) {
	// Server that immediately closes connection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("force connection close")
	}))
	server.Close() // Close immediately to simulate network error

	client := NewClientWithURL(server.URL)

	_, err := client.FetchThemeConfig("test", "theme", "1.0")
	assert.Error(t, err)

	_, err = client.GetThemeInfo("test", "theme")
	assert.Error(t, err)

	err = client.IncrementDownloadCount("test", "theme")
	assert.Error(t, err)
}

func TestClient_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{invalid json"))
	}))
	defer server.Close()

	client := NewClientWithURL(server.URL)

	_, err := client.GetThemeInfo("test", "theme")
	assert.Error(t, err)
}

func TestClient_StatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{"200 OK", http.StatusOK, false},
		{"201 Created", http.StatusCreated, false},
		{"204 No Content", http.StatusNoContent, false},
		{"400 Bad Request", http.StatusBadRequest, true},
		{"401 Unauthorized", http.StatusUnauthorized, true},
		{"403 Forbidden", http.StatusForbidden, true},
		{"404 Not Found", http.StatusNotFound, true},
		{"500 Internal Server Error", http.StatusInternalServerError, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := NewClientWithURL(server.URL)
			err := client.IncrementDownloadCount("test", "theme")

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestClientWithEnvOverride(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	mockAPI := testutil.CreateDefaultMockAPI()
	env.SetupMockAPI(mockAPI)

	// NewClient should now use the env override
	client := NewClient()
	assert.Equal(t, env.MockServer.URL, client.baseURL)

	// Verify it actually works
	info, err := client.GetThemeInfo("testuser", "sample-theme")
	require.NoError(t, err)
	assert.Equal(t, "Sample Theme", info.Name)
}
