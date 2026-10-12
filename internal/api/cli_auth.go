// cli_auth.go implements the four device-login endpoints from the API
// contract's "Auth" table: start, token (polled), whoami and revoke. See
// cmd/auth.go for the command built on top of these.
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
)

// StartAuthResponse is the 200 response from POST /api/cli/auth/start.
type StartAuthResponse struct {
	DeviceCode              string `json:"deviceCode"`
	UserCode                string `json:"userCode"`
	VerificationURL         string `json:"verificationUrl"`
	VerificationURLComplete string `json:"verificationUrlComplete"`
	ExpiresIn               int    `json:"expiresIn"`
	Interval                int    `json:"interval"`
}

// StartDeviceAuth begins a device-login flow: POST /api/cli/auth/start,
// identifying this build as "stellar-cli/<version> (<goos>; <goarch>)" (the
// same string sent as User-Agent on every request).
func (c *Client) StartDeviceAuth() (*StartAuthResponse, error) {
	ua := userAgent()
	body, err := json.Marshal(struct {
		Client *string `json:"client,omitempty"`
	}{Client: &ua})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/cli/auth/start", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, wrapTransportError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, c.classifyNewEndpointError(resp)
	}

	var out StartAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil ||
		out.DeviceCode == "" || out.UserCode == "" || out.VerificationURL == "" {
		return nil, &hubTooOldError{baseURL: c.baseURL}
	}
	return &out, nil
}

// TokenUser is the user nested in a successful POST /api/cli/auth/token
// response.
type TokenUser struct {
	Username string `json:"username"`
	Name     string `json:"name"`
}

// TokenResponse is the 200 response from POST /api/cli/auth/token.
type TokenResponse struct {
	Token string    `json:"token"`
	User  TokenUser `json:"user"`
}

// PollDeviceToken polls POST /api/cli/auth/token once for deviceCode. A
// non-200 response is always returned as an error - including the
// authorization_pending/slow_down "keep waiting" cases, which the contract
// shapes as 400 errors with those codes; the caller (cmd/auth.go's polling
// loop) distinguishes them from a real failure via errors.As(&APIError) and
// its Code field.
func (c *Client) PollDeviceToken(deviceCode string) (*TokenResponse, error) {
	body, err := json.Marshal(struct {
		DeviceCode string `json:"deviceCode"`
	}{DeviceCode: deviceCode})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/cli/auth/token", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, wrapTransportError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, c.classifyNewEndpointError(resp)
	}

	var out TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil ||
		out.Token == "" || out.User.Username == "" {
		return nil, &hubTooOldError{baseURL: c.baseURL}
	}
	return &out, nil
}

// WhoamiResponse is the 200 response from GET /api/cli/auth/whoami.
type WhoamiResponse struct {
	Username string  `json:"username"`
	Name     string  `json:"name"`
	Image    *string `json:"image"`
}

// Whoami calls GET /api/cli/auth/whoami (bearer-only). Call this on a Client
// built with NewAuthedClient - an empty token means the request carries no
// Authorization header at all, which the hub answers with 401
// invalid_token (errors.Is(err, ErrUnauthorized)).
func (c *Client) Whoami() (*WhoamiResponse, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/cli/auth/whoami", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, wrapTransportError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, c.classifyNewEndpointError(resp)
	}

	var out WhoamiResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Username == "" {
		return nil, &hubTooOldError{baseURL: c.baseURL}
	}
	return &out, nil
}

// RevokeToken calls DELETE /api/cli/auth/token (bearer-only). Per the
// contract, a 401 here counts as success too (the token is already
// unusable, which is the caller's goal either way) - see cmd/auth.go's
// logout, which also deletes the local credential regardless of this call's
// outcome.
func (c *Client) RevokeToken() error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+"/api/cli/auth/token", nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return wrapTransportError(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusUnauthorized {
		return nil
	}
	return c.classifyNewEndpointError(resp)
}
