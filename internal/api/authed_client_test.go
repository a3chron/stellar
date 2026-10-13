package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

// TestClassifyNewEndpointError exercises every branch classifyNewEndpointError
// has to distinguish: a real contract error (JSON with a code), a
// pre-upgrade hub's legacy shape on 401/404/405 (ErrHubTooOld), and a
// genuine un-coded failure from an up-to-date hub on any OTHER status - the
// regression this guards is a real 500 "Internal server error" (valid JSON,
// no code) being misreported as "doesn't support CLI publishing yet" just
// because it also happened to lack a "code" field.
func TestClassifyNewEndpointError(t *testing.T) {
	c := &Client{baseURL: "https://stellar.example"}

	t.Run("contract error with code", func(t *testing.T) {
		err := c.classifyNewEndpointError(newResp(404, `{"error":"draft not found","code":"draft_not_found"}`))
		var apiErr *APIError
		require.True(t, errors.As(err, &apiErr))
		assert.Equal(t, "draft_not_found", apiErr.Code)
		assert.Equal(t, 404, apiErr.Status)
		assert.False(t, errors.Is(err, ErrHubTooOld))
	})

	t.Run("401 with no code is hub too old (legacy Unauthorized)", func(t *testing.T) {
		err := c.classifyNewEndpointError(newResp(401, `{"error":"Unauthorized"}`))
		assert.True(t, errors.Is(err, ErrHubTooOld))
	})

	t.Run("404 with no code is hub too old (legacy Author not found)", func(t *testing.T) {
		err := c.classifyNewEndpointError(newResp(404, `{"error":"Author not found"}`))
		assert.True(t, errors.Is(err, ErrHubTooOld))
	})

	t.Run("405 with no code is hub too old", func(t *testing.T) {
		err := c.classifyNewEndpointError(newResp(405, `{"error":"Method not allowed"}`))
		assert.True(t, errors.Is(err, ErrHubTooOld))
	})

	t.Run("500 with no code is a real APIError, not hub too old", func(t *testing.T) {
		err := c.classifyNewEndpointError(newResp(500, `{"error":"Internal server error"}`))
		assert.False(t, errors.Is(err, ErrHubTooOld))
		var apiErr *APIError
		require.True(t, errors.As(err, &apiErr))
		assert.Equal(t, 500, apiErr.Status)
		assert.Equal(t, "Internal server error", apiErr.Message)
	})

	t.Run("400 with no code is a real APIError, not hub too old", func(t *testing.T) {
		err := c.classifyNewEndpointError(newResp(400, `{"error":"something else"}`))
		assert.False(t, errors.Is(err, ErrHubTooOld))
		var apiErr *APIError
		require.True(t, errors.As(err, &apiErr))
		assert.Equal(t, "something else", apiErr.Message)
	})

	t.Run("404 non-JSON body is hub too old", func(t *testing.T) {
		err := c.classifyNewEndpointError(newResp(404, "Theme not found\n"))
		assert.True(t, errors.Is(err, ErrHubTooOld))
	})

	t.Run("405 non-JSON body is hub too old", func(t *testing.T) {
		err := c.classifyNewEndpointError(newResp(405, "Method Not Allowed\n"))
		assert.True(t, errors.Is(err, ErrHubTooOld))
	})

	t.Run("413 non-JSON body means screenshot too large, not hub too old", func(t *testing.T) {
		err := c.classifyNewEndpointError(newResp(413, "<html>Request Entity Too Large</html>"))
		assert.False(t, errors.Is(err, ErrHubTooOld))
		var apiErr *APIError
		require.True(t, errors.As(err, &apiErr))
		assert.Equal(t, "screenshot too large", apiErr.Message)
	})

	t.Run("other non-JSON status falls back to the generic message", func(t *testing.T) {
		err := c.classifyNewEndpointError(newResp(504, "<html>Gateway Timeout</html>"))
		assert.False(t, errors.Is(err, ErrHubTooOld))
		assert.Contains(t, err.Error(), "504")
	})

	t.Run("hubTooOldError message names the base URL", func(t *testing.T) {
		err := c.classifyNewEndpointError(newResp(404, "Not found"))
		assert.Contains(t, err.Error(), "https://stellar.example")
		assert.Contains(t, err.Error(), "doesn't support CLI publishing yet")
	})
}

func TestAPIError_IsErrUnauthorized(t *testing.T) {
	err := &APIError{Status: http.StatusUnauthorized, Code: "invalid_token"}
	assert.True(t, errors.Is(err, ErrUnauthorized))

	other := &APIError{Status: http.StatusBadRequest, Code: "validation_failed"}
	assert.False(t, errors.Is(other, ErrUnauthorized))
}

func TestIsLoopbackHost(t *testing.T) {
	assert.True(t, isLoopbackHost("localhost"))
	assert.True(t, isLoopbackHost("localhost:3000"))
	assert.True(t, isLoopbackHost("127.0.0.1"))
	assert.True(t, isLoopbackHost("127.0.0.1:3000"))
	assert.True(t, isLoopbackHost("[::1]:3000"))
	assert.False(t, isLoopbackHost("stellar.a3chron.dev"))
	assert.False(t, isLoopbackHost("example.com:3000"))
}
